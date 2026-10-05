package main

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
// that only lives in a FREE page: its row was deleted or replaced by a version
// of lgit that kept no secure_delete: is invisible to every query and still
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

// The store is routinely written by a binary OLDER than the one that migrated
// it: the extension runs a bundled lgit, Claude Code's MCP server runs whatever
// is on PATH. That older binary stores the password again, and a migration that
// runs once would never see it. Every open re-examines the rows written since.
func TestStore_RescrubsWhatAnOlderBinaryWrote(t *testing.T) {
	path := filepath.Join(t.TempDir(), "findings.db")
	const secret = "Hw4nBv7cXk2mQ9e"

	s, err := openStoreAt(path) // migrates (empty) and sets the watermark
	if err != nil {
		t.Fatal(err)
	}
	var v int
	_ = s.db.QueryRow(`PRAGMA user_version`).Scan(&v)
	if v != schemaVersion {
		t.Fatalf("precondition: store should be migrated, user_version = %d", v)
	}
	// An older binary upserts a finding AFTER the migration.
	plantLegacyRow(t, s, "connection_strings", "late.sh:1:creds_in_url",
		"postgres://svc:"+secret+"@db.prod.io/app in late.sh:1.", time.Now().UnixMilli()+5000)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(rawBytes(t, path), []byte(secret)) {
		t.Fatal("precondition: the plaintext written by the older binary should be in the file")
	}

	s, err = openStoreAt(path)
	if err != nil {
		t.Fatal(err)
	}
	var msg string
	if err := s.db.QueryRow(`SELECT message FROM findings WHERE file_path = 'late.sh:1:creds_in_url'`).Scan(&msg); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(msg, secret) {
		t.Errorf("a row written after the migration was not scrubbed on the next open: %q", msg)
	}
	_ = s.Close()
	if bytes.Contains(rawBytes(t, path, path+"-wal", path+"-shm"), []byte(secret)) {
		t.Error("the secret is gone from the row but still readable in the database file")
	}
}

// A checkpoint blocked by another process's read snapshot does not fail: it
// returns a row with busy=1. Ignoring that row reported success for a WAL that
// still held the old pages.
func TestCheckpointTruncate_ReportsWhenABlockedByAReader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "findings.db")
	s, err := openStoreAt(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if !checkpointTruncate(s.db) {
		t.Fatal("an idle store must checkpoint cleanly")
	}

	// A second connection takes a read snapshot and holds it.
	other, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(2000)")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	tx, err := other.Begin()
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := tx.QueryRow(`SELECT count(*) FROM findings`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	// New frames land in the WAL while the reader is open.
	plantLegacyRow(t, s, "g", "f:1:r", "m", 1)
	start := time.Now()
	if checkpointWithRetry(s.db) {
		t.Error("a checkpoint blocked by an open reader must report failure, not success")
	}
	// It must also give up quickly: waiting out the store's 15 s busy timeout
	// five times over would freeze every lgit command that opens the store.
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("a blocked checkpoint took %v to give up; it must not wait out the 15 s busy timeout", d)
	}
	var bt int
	if err := s.db.QueryRow(`PRAGMA busy_timeout`).Scan(&bt); err != nil || bt != 15000 {
		t.Errorf("the store's busy timeout must be restored afterwards: %d (%v)", bt, err)
	}
	_ = tx.Rollback()
	if !checkpointTruncate(s.db) {
		t.Error("once the reader is gone the checkpoint must complete")
	}
}

// The claim "no gate can leak by forgetting" needs a gate that forgot. The gate
// in the test returns a message with a password in it; finalizeFindings: the
// step RunChecks applies to every gate: must mask it, whatever the gate did.
func TestFinalizeFindings_MasksWhateverAGateSays(t *testing.T) {
	fs := []Finding{
		{Message: "connect with postgres://svc:Kq8vLw2nRt4x@db.prod.io/app now"},
		{Message: "see https://api.acme.io/v1?api_key=Zr5cYb7hJs9u&x=1"},
		{Message: "nothing secret here"},
	}
	finalizeFindings(fs, "/p", Gate{ID: "careless_gate", Severity: SeverityWarning, Title: "T", Tags: "x"}, "", false)
	for _, f := range fs {
		for _, leaked := range []string{"Kq8vLw2nRt4x", "Zr5cYb7hJs9u"} {
			if strings.Contains(f.Message, leaked) {
				t.Errorf("a gate's message reached the store with a secret in it: %q", f.Message)
			}
		}
		if f.Project != "/p" || f.GateID != "careless_gate" || f.Severity != SeverityWarning {
			t.Errorf("the normalisation the funnel always did must still happen: %+v", f)
		}
	}
	if fs[2].Message != "nothing secret here" {
		t.Errorf("an innocent message was altered: %q", fs[2].Message)
	}
}
