package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// newTestStore returns a Store backed by a fresh DB inside t.TempDir(). The
// store is closed automatically when the test ends.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := openStoreAt(filepath.Join(dir, "findings.db"))
	if err != nil {
		t.Fatalf("openStoreAt: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestStore_UpsertInsertsAndUpdates(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	first, err := s.Upsert(ctx, Finding{
		Project: "/p", GateID: "g1", Severity: SeverityWarning,
		Title: "T", Message: "m1", FilePath: "a",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != StatusOpen {
		t.Fatalf("status = %q, want open", first.Status)
	}
	if first.CreatedAt == 0 || first.UpdatedAt == 0 {
		t.Fatalf("timestamps must be populated")
	}

	// Same key, new message — should update in place, not insert.
	second, err := s.Upsert(ctx, Finding{
		Project: "/p", GateID: "g1", Severity: SeverityError,
		Title: "T", Message: "m2", FilePath: "a",
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID {
		t.Fatalf("ID changed on conflict: %d -> %d", first.ID, second.ID)
	}
	if second.Message != "m2" {
		t.Fatalf("message not updated: %q", second.Message)
	}
	if second.Severity != SeverityError {
		t.Fatalf("severity not updated: %q", second.Severity)
	}
}

func TestStore_UpsertPreservesIgnored(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	f, err := s.Upsert(ctx, Finding{Project: "/p", GateID: "g", Severity: SeverityWarning, Title: "T", Message: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Ignore(ctx, f.ID); err != nil {
		t.Fatal(err)
	}

	// Re-detection should NOT resurface an ignored finding.
	again, err := s.Upsert(ctx, Finding{Project: "/p", GateID: "g", Severity: SeverityWarning, Title: "T", Message: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if again.Status != StatusIgnored {
		t.Fatalf("ignored finding was resurfaced: status=%q", again.Status)
	}
}

func TestStore_MarkResolved(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	mk := func(file string) {
		if _, err := s.Upsert(ctx, Finding{Project: "/p", GateID: "g", Severity: SeverityWarning, Title: "T", Message: "m", FilePath: file}); err != nil {
			t.Fatal(err)
		}
	}
	mk("a")
	mk("b")
	mk("c")

	// Keep only "b" — a and c should be resolved.
	n, err := s.MarkResolved(ctx, "/p", "g", []string{"b"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("MarkResolved closed %d, want 2", n)
	}
	open, err := s.List(ctx, "/p", StatusOpen, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 || open[0].FilePath != "b" {
		t.Fatalf("unexpected open set: %+v", open)
	}

	// Empty keep => close all remaining open ones.
	n2, err := s.MarkResolved(ctx, "/p", "g", nil)
	if err != nil {
		t.Fatal(err)
	}
	if n2 != 1 {
		t.Fatalf("MarkResolved empty-keep closed %d, want 1", n2)
	}
}

func TestStore_ListFilters(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.Upsert(ctx, Finding{Project: "/p1", GateID: "g", Severity: SeverityWarning, Title: "T", Message: "m"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upsert(ctx, Finding{Project: "/p2", GateID: "g", Severity: SeverityWarning, Title: "T", Message: "m"}); err != nil {
		t.Fatal(err)
	}

	all, err := s.List(ctx, "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("List all: got %d, want 2", len(all))
	}
	p1, err := s.List(ctx, "/p1", StatusOpen, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(p1) != 1 || p1[0].Project != "/p1" {
		t.Fatalf("List p1: got %+v", p1)
	}
}

func TestStore_DeleteAndClear(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	f, _ := s.Upsert(ctx, Finding{Project: "/p", GateID: "g", Severity: SeverityWarning, Title: "T", Message: "m"})
	ok, err := s.Delete(ctx, f.ID)
	if err != nil || !ok {
		t.Fatalf("Delete: ok=%v err=%v", ok, err)
	}
	if _, err := s.Upsert(ctx, Finding{Project: "/p", GateID: "g2", Severity: SeverityWarning, Title: "T", Message: "m"}); err != nil {
		t.Fatal(err)
	}
	n, err := s.ClearProject(ctx, "/p")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("ClearProject: got %d, want 1", n)
	}
}

func TestStore_TimestampMonotonic(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	a, _ := s.Upsert(ctx, Finding{Project: "/p", GateID: "g", Severity: SeverityWarning, Title: "T", Message: "m"})
	time.Sleep(2 * time.Millisecond)
	b, _ := s.Upsert(ctx, Finding{Project: "/p", GateID: "g", Severity: SeverityWarning, Title: "T", Message: "m2"})
	if b.UpdatedAt < a.UpdatedAt {
		t.Fatalf("updated_at went backwards: %d -> %d", a.UpdatedAt, b.UpdatedAt)
	}
}
