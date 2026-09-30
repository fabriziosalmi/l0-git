package main

import (
	"database/sql"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"
)

// The store holds every finding of every project, and finding messages quote
// repository content. It is for its owner only.
const (
	storeDirMode  os.FileMode = 0o700
	storeFileMode os.FileMode = 0o600
)

// tightenMode removes group and other access from path. It only ever REMOVES
// bits — a 0444 file becomes 0400, never 0600 — and it does not follow a
// symlink: a `~/.l0-git` that links to a shared directory must not have the
// shared directory's mode changed. There are no POSIX modes to tighten on
// Windows, where the store inherits the ACL of its directory.
//
// A failed chmod is reported on stderr, because the documentation promises a
// private store and silence would let it stay world-readable unnoticed.
func tightenMode(path string) {
	if runtime.GOOS == "windows" {
		return
	}
	fi, err := os.Lstat(path)
	if err != nil || fi.Mode()&os.ModeSymlink != 0 {
		return
	}
	perm := fi.Mode().Perm()
	want := perm &^ 0o077
	if want == perm {
		return
	}
	if err := os.Chmod(path, want); err != nil {
		fmt.Fprintf(os.Stderr, "lgit: warning: could not restrict permissions on %s: %v\n", path, err)
	}
}

// tightenStoreFiles applies tightenMode to the database and to the WAL /
// shared-memory / journal files SQLite keeps beside it.
func tightenStoreFiles(path string) {
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		tightenMode(path + suffix)
	}
}

// ensureNewFilePrivate creates path as an empty 0600 file when it does not
// exist, so that a database lgit creates is private whatever the umask. SQLite
// gives its WAL and shared-memory files the mode of the main file, so they
// follow. An existing file is never touched here.
func ensureNewFilePrivate(path string) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, storeFileMode)
	if err == nil {
		_ = f.Close()
	}
}

// schemaVersion lives in PRAGMA user_version.
//
//	0  before versioning
//	1  finding messages no longer carry credentials (see redactSecrets)
const schemaVersion = 1

// metaScrubbedThrough is the watermark, in milliseconds: every row whose
// updated_at is BELOW it has been through redactSecrets.
const metaScrubbedThrough = "scrubbed_through"

func migrateStore(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS meta (k TEXT PRIMARY KEY, v TEXT NOT NULL)`); err != nil {
		return err
	}
	var v int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		return err
	}
	// A newer lgit wrote this store: leave it alone rather than "migrate" it
	// backwards.
	if v > schemaVersion {
		return nil
	}
	if v < 1 {
		return scrubWholeStore(db)
	}
	return rescrubRecent(db)
}

// scrubWholeStore is the one-off migration: every stored message goes through
// redactSecrets, the file is rebuilt, and only then is the version bumped.
//
// Until this version connection_strings stored the matched URL verbatim, so a
// store that has been used for a while holds real passwords — including in
// rows already marked resolved, long after the credential left the repository.
func scrubWholeStore(db *sql.DB) error {
	if _, err := rewriteMessages(db, 0); err != nil {
		return fmt.Errorf("scrub stored credentials: %w", err)
	}
	// Rewriting the rows is not enough. The previous text of each one is still
	// in the file — in pages freed when rows were deleted or replaced, and in
	// the earlier versions an upsert leaves behind — and secure_delete only
	// protects what is freed from now on. A measured run on a real 39 MB store
	// left 9 of 47 distinctive passwords readable after the UPDATEs alone.
	// VACUUM rebuilds the whole file, so nothing old survives it.
	//
	// A failure is returned, not swallowed: the version is not bumped, so the
	// next open tries again instead of reporting a scrub that left the secrets
	// on disk.
	if _, err := db.Exec(`VACUUM`); err != nil {
		return fmt.Errorf("compact store after scrub: %w", err)
	}
	if !checkpointWithRetry(db) {
		// The rows are clean and the file is rebuilt, but another process still
		// holds a read snapshot, so the old pages cannot be dropped from the
		// write-ahead log yet. Say so, and leave the version alone: the next
		// open repeats the (cheap) work until it completes.
		fmt.Fprintln(os.Stderr, "lgit: the findings store was scrubbed, but another process is still reading it, "+
			"so the previous text may remain in its write-ahead log; this will be retried the next time it is opened")
		return nil
	}
	if err := setWatermark(db, maxUpdatedAt(db)); err != nil {
		return err
	}
	// PRAGMA takes no bound parameters; schemaVersion is a constant.
	_, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion))
	return err
}

// rescrubRecent runs on every open of an already-migrated store, over the rows
// written since the last time. The extension runs a bundled lgit and Claude
// Code's MCP server runs whatever is on PATH, so a store is routinely written by
// a binary OLDER than the one that migrated it, and that binary stores the
// password again. The work is bounded by an index on updated_at: a store nobody
// has written to since costs one indexed lookup.
func rescrubRecent(db *sql.DB) error {
	// Read the high-water mark BEFORE the scan: anything written after it has a
	// later updated_at and is picked up next time, so no write can slip between
	// the scan and the watermark.
	high := maxUpdatedAt(db)
	changed, err := rewriteMessages(db, getWatermark(db))
	if err != nil {
		return fmt.Errorf("rescrub stored credentials: %w", err)
	}
	if changed > 0 {
		// An older binary put plaintext back. Rebuild the file again (best
		// effort: this is the rare path) so the replaced text does not linger.
		_, _ = db.Exec(`VACUUM`)
		checkpointWithRetry(db)
	}
	return setWatermark(db, high)
}

// rewriteMessages passes every message of a row updated at or after `since`
// through redactSecrets, and returns how many it changed. updated_at is left
// alone: this changes what a row SAYS, not when it was last observed. Each
// UPDATE names the message it read, so a concurrent writer's newer text is never
// overwritten with an older redaction.
func rewriteMessages(db *sql.DB, since int64) (int, error) {
	rows, err := db.Query(`SELECT id, message FROM findings
		WHERE updated_at >= ? AND (message LIKE '%://%' OR message LIKE '%=%' OR message LIKE '%jdbc:%')`, since)
	if err != nil {
		return 0, err
	}
	type change struct {
		id       int64
		old, new string
	}
	var changes []change
	for rows.Next() {
		var id int64
		var msg string
		if err := rows.Scan(&id, &msg); err != nil {
			_ = rows.Close()
			return 0, err
		}
		if clean := redactSecrets(msg); clean != msg {
			changes = append(changes, change{id, msg, clean})
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	_ = rows.Close()
	if len(changes) == 0 {
		return 0, nil
	}
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	for _, c := range changes {
		if _, err := tx.Exec(`UPDATE findings SET message = ? WHERE id = ? AND message = ?`, c.new, c.id, c.old); err != nil {
			_ = tx.Rollback()
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(changes), nil
}

func maxUpdatedAt(db *sql.DB) int64 {
	var m sql.NullInt64
	if err := db.QueryRow(`SELECT MAX(updated_at) FROM findings`).Scan(&m); err != nil || !m.Valid {
		return 0
	}
	return m.Int64
}

func getWatermark(db *sql.DB) int64 {
	var v string
	if err := db.QueryRow(`SELECT v FROM meta WHERE k = ?`, metaScrubbedThrough).Scan(&v); err != nil {
		return 0
	}
	var n int64
	if _, err := fmt.Sscanf(strings.TrimSpace(v), "%d", &n); err != nil {
		return 0
	}
	return n
}

func setWatermark(db *sql.DB, ms int64) error {
	_, err := db.Exec(`INSERT INTO meta (k, v) VALUES (?, ?)
		ON CONFLICT(k) DO UPDATE SET v = excluded.v`, metaScrubbedThrough, fmt.Sprintf("%d", ms))
	return err
}

// checkpointTruncate folds the write-ahead log into the main file and truncates
// it, and reports whether that COMPLETED. A checkpoint blocked by another
// process's read snapshot is not a Go error: it comes back as a row with
// busy=1, so ignoring the result reports success for a log that still holds the
// old pages.
func checkpointTruncate(db *sql.DB) bool {
	var busy, logFrames, checkpointed int
	if err := db.QueryRow(`PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &logFrames, &checkpointed); err != nil {
		return false
	}
	return busy == 0
}

// checkpointWithRetry tries a few times, briefly. The store's normal busy
// timeout is 15 s — right for a write that has to wait its turn, wrong here:
// a checkpoint blocked by a long-lived reader waits the whole timeout before
// giving up, and five of those would freeze the open for over a minute. The
// pool holds one connection, so the pragma applies to the calls below and is
// put back afterwards.
func checkpointWithRetry(db *sql.DB) bool {
	_, _ = db.Exec(`PRAGMA busy_timeout = 250`)
	defer func() { _, _ = db.Exec(`PRAGMA busy_timeout = 15000`) }()
	for i := 0; i < 5; i++ {
		if checkpointTruncate(db) {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}
