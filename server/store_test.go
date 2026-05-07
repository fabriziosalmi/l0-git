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
	open, err := s.List(ctx, FindingFilter{Project: "/p", Status: StatusOpen, Limit: 100})
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

	all, err := s.List(ctx, FindingFilter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("List all: got %d, want 2", len(all))
	}
	p1, err := s.List(ctx, FindingFilter{Project: "/p1", Status: StatusOpen, Limit: 100})
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

func TestStore_ListFilters_SeverityAndGate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mk := func(gate, sev, file string) {
		if _, err := s.Upsert(ctx, Finding{Project: "/p", GateID: gate, Severity: sev, Title: "T", Message: "m", FilePath: file, Tags: "security,quality"}); err != nil {
			t.Fatal(err)
		}
	}
	mk("g_a", SeverityError, "a")
	mk("g_a", SeverityWarning, "b")
	mk("g_b", SeverityError, "c")

	// severity filter
	es, err := s.List(ctx, FindingFilter{Project: "/p", Severity: SeverityError, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 2 {
		t.Errorf("severity=error: got %d, want 2", len(es))
	}

	// gate filter
	ga, err := s.List(ctx, FindingFilter{Project: "/p", GateID: "g_a", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(ga) != 2 {
		t.Errorf("gate=g_a: got %d, want 2", len(ga))
	}

	// combined
	combo, err := s.List(ctx, FindingFilter{Project: "/p", GateID: "g_a", Severity: SeverityWarning, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(combo) != 1 || combo[0].FilePath != "b" {
		t.Errorf("combined filter: %+v", combo)
	}
}

func TestStore_ListFilters_TagCSVAware(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.Upsert(ctx, Finding{Project: "/p", GateID: "g1", Severity: SeverityWarning, Title: "T", Message: "m", FilePath: "a", Tags: "security,git-hygiene"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upsert(ctx, Finding{Project: "/p", GateID: "g2", Severity: SeverityWarning, Title: "T", Message: "m", FilePath: "b", Tags: "quality"}); err != nil {
		t.Fatal(err)
	}

	got, err := s.List(ctx, FindingFilter{Project: "/p", Tag: "security", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].FilePath != "a" {
		t.Errorf("tag=security: got %+v", got)
	}

	// "git" must NOT match "git-hygiene".
	got, err = s.List(ctx, FindingFilter{Project: "/p", Tag: "git", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("tag=git must not match git-hygiene, got: %+v", got)
	}

	got, err = s.List(ctx, FindingFilter{Project: "/p", Tag: "git-hygiene", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("tag=git-hygiene: got %+v", got)
	}
}

func TestStore_ListFilters_QuerySubstring(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.Upsert(ctx, Finding{Project: "/p", GateID: "secrets_scan", Severity: SeverityError, Title: "AWS access key", Message: "leaky.go:5 found", FilePath: "leaky.go:5"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upsert(ctx, Finding{Project: "/p", GateID: "tests_present", Severity: SeverityWarning, Title: "No tests", Message: "add a test", FilePath: ""}); err != nil {
		t.Fatal(err)
	}
	got, err := s.List(ctx, FindingFilter{Project: "/p", Query: "AWS", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("query=AWS: %+v", got)
	}
	got, err = s.List(ctx, FindingFilter{Project: "/p", Query: "leaky.go", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("query=leaky.go: %+v", got)
	}
}

func TestStore_ListFilters_SortBySeverity(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mk := func(sev string) {
		if _, err := s.Upsert(ctx, Finding{Project: "/p", GateID: "g", Severity: sev, Title: "T", Message: "m", FilePath: sev}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Millisecond)
	}
	mk(SeverityInfo)
	mk(SeverityError)
	mk(SeverityWarning)

	got, err := s.List(ctx, FindingFilter{Project: "/p", Sort: "severity", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d, want 3", len(got))
	}
	if got[0].Severity != SeverityError || got[1].Severity != SeverityWarning || got[2].Severity != SeverityInfo {
		t.Errorf("severity sort: got %v %v %v", got[0].Severity, got[1].Severity, got[2].Severity)
	}
}

func TestStore_ListFilters_OffsetLimit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if _, err := s.Upsert(ctx, Finding{
			Project: "/p", GateID: "g", Severity: SeverityWarning,
			Title: "T", Message: "m", FilePath: "f" + string(rune('0'+i)),
		}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Millisecond)
	}
	page1, err := s.List(ctx, FindingFilter{Project: "/p", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	page2, err := s.List(ctx, FindingFilter{Project: "/p", Limit: 2, Offset: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page1) != 2 || len(page2) != 2 {
		t.Fatalf("expected 2+2 pages, got %d+%d", len(page1), len(page2))
	}
	if page1[0].FilePath == page2[0].FilePath {
		t.Errorf("offset failed: same first row in both pages")
	}
}

func TestStore_Stats_ShapeAndCounts(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mk := func(gate, sev, file, tags, status string) {
		f := Finding{
			Project: "/p", GateID: gate, Severity: sev,
			Title: "T", Message: "m", FilePath: file, Tags: tags,
		}
		saved, err := s.Upsert(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		if status != "" && status != "open" {
			if status == "ignored" {
				if _, err := s.Ignore(ctx, saved.ID); err != nil {
					t.Fatal(err)
				}
			}
		}
		time.Sleep(2 * time.Millisecond)
	}
	mk("g_a", SeverityError, "src/a.go:10:x", "security", "open")
	mk("g_a", SeverityWarning, "src/a.go:20:y", "security", "open")
	mk("g_a", SeverityWarning, "src/b.go:5:z", "security", "open")
	mk("g_b", SeverityInfo, "docs/c.md:3:w", "documentation", "open")
	mk("g_b", SeverityInfo, "", "documentation", "ignored")

	stats, err := s.Stats(ctx, "/p")
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total != 5 {
		t.Errorf("Total = %d, want 5", stats.Total)
	}
	// BySeverity is open-only: 1 error + 2 warnings + 1 info open
	// (the ignored info is excluded by design).
	if stats.BySeverity[SeverityError] != 1 || stats.BySeverity[SeverityWarning] != 2 || stats.BySeverity[SeverityInfo] != 1 {
		t.Errorf("BySeverity (open-only) = %+v", stats.BySeverity)
	}
	if stats.ByStatus[StatusOpen] != 4 || stats.ByStatus[StatusIgnored] != 1 {
		t.Errorf("ByStatus = %+v", stats.ByStatus)
	}
	if len(stats.ByGate) == 0 || stats.ByGate[0].Key != "g_a" || stats.ByGate[0].Count != 3 {
		t.Errorf("ByGate top should be g_a:3, got: %+v", stats.ByGate)
	}
	if len(stats.TopFiles) == 0 {
		t.Fatalf("TopFiles empty")
	}
	if stats.TopFiles[0].Key != "src/a.go" || stats.TopFiles[0].Count != 2 {
		t.Errorf("TopFiles top should be src/a.go:2 (stem-grouped), got: %+v", stats.TopFiles)
	}
	// ByTag: 3 security (open) + 1 documentation (open). Ignored excluded.
	tagMap := map[string]int{}
	for _, kc := range stats.ByTag {
		tagMap[kc.Key] = kc.Count
	}
	if tagMap["security"] != 3 || tagMap["documentation"] != 1 {
		t.Errorf("ByTag = %+v", stats.ByTag)
	}
	// 7-day trend: 7 entries oldest-first, today at index 6.
	if len(stats.Last7Days) != 7 {
		t.Fatalf("Last7Days length = %d, want 7", len(stats.Last7Days))
	}
	totalInTrend := 0
	for _, d := range stats.Last7Days {
		totalInTrend += d.Count
	}
	if totalInTrend < 4 {
		// All open findings created within the last 7 days (just-now).
		t.Errorf("Last7Days total = %d, expected >= 4 fresh findings", totalInTrend)
	}
}

// Tags exploding via SQL substring would mis-match "git" against
// "git-hygiene"; verify the Go-side splitter doesn't fall into the same
// trap.
func TestStore_Stats_TagExplosionExact(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.Upsert(ctx, Finding{Project: "/p", GateID: "g", Severity: SeverityWarning, Title: "T", Message: "m", FilePath: "a", Tags: "security,git-hygiene"}); err != nil {
		t.Fatal(err)
	}
	stats, err := s.Stats(ctx, "/p")
	if err != nil {
		t.Fatal(err)
	}
	tags := map[string]int{}
	for _, kc := range stats.ByTag {
		tags[kc.Key] = kc.Count
	}
	if tags["security"] != 1 || tags["git-hygiene"] != 1 || tags["git"] != 0 {
		t.Errorf("tag explosion wrong: %+v", tags)
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
