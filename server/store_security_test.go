package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// plantLegacyRow inserts a row the way versions before redaction wrote it: the
// whole URL, password included, in the message.
func plantLegacyRow(t *testing.T, s *Store, gate, file, msg string, updatedAt int64) {
	t.Helper()
	_, err := s.db.Exec(`INSERT INTO findings
		(project, gate_id, severity, title, message, file_path, tags, status, created_at, updated_at)
		VALUES ('/p', ?, 'error', 'T', ?, ?, '', 'open', 1000, ?)`, gate, msg, file, updatedAt)
	if err != nil {
		t.Fatal(err)
	}
}

func rawBytes(t *testing.T, paths ...string) []byte {
	t.Helper()
	var all []byte
	for _, p := range paths {
		if b, err := os.ReadFile(p); err == nil {
			all = append(all, b...)
		}
	}
	return all
}

// The migration must remove the secret from the FILE, not just from query
// results: a row that was rewritten but whose old bytes sit in a free page or
// in the WAL has not been scrubbed, only hidden.
func TestStore_MigrationScrubsStoredCredentialsFromDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "findings.db")
	const secret = "Zq8nRt4vLw2xKd9"

	s, err := openStoreAt(path)
	if err != nil {
		t.Fatal(err)
	}
	plantLegacyRow(t, s, "connection_strings", "a.sh:3:creds_in_url",
		"postgres://admin:"+secret+"@db.prod.io:5432/app in a.sh:3. Remove it.", 111)
	plantLegacyRow(t, s, "connection_strings", "b.sh:1:db_uri",
		"mongodb://h/db?authSource=x&password="+secret+" in b.sh:1.", 222)
	plantLegacyRow(t, s, "markdown_lint", "README.md:5:link_local_broken",
		"link target `docs/guide.md` does not exist on disk.", 333)
	if _, err := s.db.Exec(`PRAGMA user_version = 0`); err != nil { // "a store from before"
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(rawBytes(t, path), []byte(secret)) {
		t.Fatal("precondition: the planted secret should be in the file before the migration")
	}

	s, err = openStoreAt(path) // runs the migration
	if err != nil {
		t.Fatal(err)
	}
	var v int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil || v != schemaVersion {
		t.Fatalf("user_version = %d (err %v), want %d", v, err, schemaVersion)
	}

	rows, err := s.db.Query(`SELECT file_path, message, updated_at FROM findings ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][2]any{}
	for rows.Next() {
		var fp, msg string
		var upd int64
		if err := rows.Scan(&fp, &msg, &upd); err != nil {
			t.Fatal(err)
		}
		got[fp] = [2]any{msg, upd}
	}
	_ = rows.Close()
	for _, fp := range []string{"a.sh:3:creds_in_url", "b.sh:1:db_uri"} {
		msg := got[fp][0].(string)
		if bytes.Contains([]byte(msg), []byte(secret)) || !bytes.Contains([]byte(msg), []byte(redactedMark)) {
			t.Errorf("%s was not scrubbed: %q", fp, msg)
		}
	}
	// A row with nothing to hide is byte-for-byte what it was, and no row's
	// timestamp moved: the scrub changes what a message says, not when the
	// finding was last seen.
	if got["README.md:5:link_local_broken"][0] != "link target `docs/guide.md` does not exist on disk." {
		t.Errorf("an innocent row was altered: %v", got["README.md:5:link_local_broken"])
	}
	for fp, want := range map[string]int64{"a.sh:3:creds_in_url": 111, "b.sh:1:db_uri": 222, "README.md:5:link_local_broken": 333} {
		if got[fp][1].(int64) != want {
			t.Errorf("%s updated_at = %v, want %d", fp, got[fp][1], want)
		}
	}

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(rawBytes(t, path, path+"-wal", path+"-shm"), []byte(secret)) {
		t.Fatal("the secret is gone from the rows but still readable in the database file or its WAL")
	}

	// And it is a one-off: opening again neither errors nor rewrites anything.
	s, err = openStoreAt(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
}

// A newer lgit wrote this store. It must not be "migrated" backwards or have
// its rows touched by an older binary.
func TestStore_MigrationLeavesANewerStoreAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "findings.db")
	s, err := openStoreAt(path)
	if err != nil {
		t.Fatal(err)
	}
	plantLegacyRow(t, s, "connection_strings", "a.sh:3:creds_in_url", "https://u:pw9pw9@h.io in a.sh:3.", 1)
	if _, err := s.db.Exec(`PRAGMA user_version = 99`); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	s, err = openStoreAt(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var msg string
	var v int
	_ = s.db.QueryRow(`SELECT message FROM findings`).Scan(&msg)
	_ = s.db.QueryRow(`PRAGMA user_version`).Scan(&v)
	if v != 99 || msg != "https://u:pw9pw9@h.io in a.sh:3." {
		t.Errorf("a newer store was modified: user_version=%d message=%q", v, msg)
	}
}

func modeOf(t *testing.T, p string) os.FileMode {
	t.Helper()
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode().Perm()
}

func TestStore_FilesAreOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	base := t.TempDir()
	path := filepath.Join(base, "created", "by", "lgit", "findings.db")
	t.Setenv("LGIT_DB", path)
	s, err := OpenStore()
	if err != nil {
		t.Fatal(err)
	}
	plantLegacyRow(t, s, "g", "f:1:r", "m", 1) // forces the WAL and shm files to exist
	defer s.Close()
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if _, err := os.Stat(path + suffix); err != nil {
			continue
		}
		if m := modeOf(t, path+suffix); m&0o077 != 0 {
			t.Errorf("%s has mode %v, want no group/other access", filepath.Base(path+suffix), m)
		}
	}
	if m := modeOf(t, filepath.Dir(path)); m&0o077 != 0 {
		t.Errorf("a directory lgit created has mode %v, want 0700", m)
	}
}

func TestStore_TightensAnExistingWorldReadableDB(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	path := filepath.Join(t.TempDir(), "findings.db")
	s, err := openStoreAt(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if err := os.Chmod(path, 0o644); err != nil { // what earlier versions left behind
		t.Fatal(err)
	}
	s, err = openStoreAt(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if m := modeOf(t, path); m&0o077 != 0 {
		t.Errorf("an existing 0644 store was not tightened: %v", m)
	}
}

// The default directory is lgit's own and is brought to 0700. A directory the
// user pointed LGIT_DB into is NOT: `LGIT_DB=/tmp/x.db` must never chmod /tmp.
func TestStoreDir_OnlyTheDefaultDirectoryIsTightened(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("LGIT_DB", "")
	def := filepath.Join(home, ".l0-git")
	if err := os.Mkdir(def, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := defaultDBPath(); err != nil {
		t.Fatal(err)
	}
	if m := modeOf(t, def); m != 0o700 {
		t.Errorf("default dir mode = %v, want 0700", m)
	}

	shared := filepath.Join(t.TempDir(), "shared")
	if err := os.Mkdir(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LGIT_DB", filepath.Join(shared, "findings.db"))
	if _, err := defaultDBPath(); err != nil {
		t.Fatal(err)
	}
	if m := modeOf(t, shared); m != 0o755 {
		t.Errorf("an existing directory chosen via LGIT_DB was chmod'ed to %v; it must be left alone", m)
	}
}

func TestStore_SecureDeleteIsOn(t *testing.T) {
	s := newTestStore(t)
	var on int
	if err := s.db.QueryRowContext(context.Background(), `PRAGMA secure_delete`).Scan(&on); err != nil {
		t.Fatal(err)
	}
	if on != 1 {
		t.Errorf("secure_delete = %d, want 1", on)
	}
}

// Rewriting rows is not enough, and this is the case that proves it: a secret
// that only lives in a FREE page — its row was deleted or replaced by a version
// of lgit that kept no secure_delete — is invisible to every query and still
// readable in the file. The migration has to rebuild the file itself.
func TestStore_MigrationRemovesRemnantsFromFreePages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "findings.db")
	const gone = "Rm3vKq8xTn5wPz2" // only ever in rows that are deleted before the migration
	const kept = "Ld6cYb4hJs9uXe7" // in a row that survives, so it must be scrubbed in place

	s, err := openStoreAt(path)
	if err != nil {
		t.Fatal(err)
	}
	// Behave like an old binary: freed bytes are NOT overwritten.
	if _, err := s.db.Exec(`PRAGMA secure_delete = OFF`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 400; i++ {
		plantLegacyRow(t, s, "connection_strings", fmt.Sprintf("f%d.sh:1:creds_in_url", i),
			"postgres://svc:"+gone+"@db.prod.io/app in f.sh:1. "+strings.Repeat("padding ", 12), int64(i))
	}
	plantLegacyRow(t, s, "connection_strings", "keep.sh:1:creds_in_url", "postgres://svc:"+kept+"@db.prod.io/app in keep.sh:1.", 9999)
	if _, err := s.db.Exec(`DELETE FROM findings WHERE file_path LIKE 'f%.sh:1:creds_in_url'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`PRAGMA user_version = 0`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if !bytes.Contains(rawBytes(t, path), []byte(gone)) {
		t.Fatal("precondition: deleting rows without secure_delete should leave their bytes in the file")
	}

	s, err = openStoreAt(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	disk := rawBytes(t, path, path+"-wal", path+"-shm")
	if bytes.Contains(disk, []byte(gone)) {
		t.Error("a deleted row's password is still readable in the database file after the migration")
	}
	if bytes.Contains(disk, []byte(kept)) {
		t.Error("a surviving row's password is still readable in the database file after the migration")
	}
}
