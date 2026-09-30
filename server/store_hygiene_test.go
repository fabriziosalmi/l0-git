package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func openFindings(t *testing.T, s *Store, project, gate string) []Finding {
	t.Helper()
	// No Status filter: every status.
	fs, err := s.List(context.Background(), FindingFilter{Project: project, GateID: gate, Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

// A directory that is not a git repository makes every gate that reads the index
// say so. Sixteen identical notices were one fact stated sixteen times — 238 rows
// in one real store — and they multiplied in every count.
func TestRunChecks_NotAGitRepoIsReportedOnce(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	res, err := RunChecks(context.Background(), s, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	var notices []Finding
	for _, f := range res.Findings {
		if f.FilePath == ".git" {
			notices = append(notices, f)
		}
	}
	if len(notices) != 1 || notices[0].GateID != notGitGateID {
		t.Fatalf("want exactly one .git finding, filed under %s; got %d: %+v", notGitGateID, len(notices), notices)
	}
	n := notices[0]
	if n.Severity != SeverityInfo {
		t.Errorf("severity = %s, want info", n.Severity)
	}
	// It names the gates it stopped, and the count in the title is honest.
	for _, g := range []string{"secrets_scan", "network_scan", "connection_strings", "merge_conflict_markers"} {
		if !strings.Contains(n.Message, g) {
			t.Errorf("the notice should name %s: %s", g, n.Message)
		}
	}
	names := strings.Split(strings.TrimSuffix(strings.SplitN(n.Message, ": ", 2)[1], ". Run `git init`, or run lgit from inside a clone."), ", ")
	if !strings.Contains(n.Title, strconv.Itoa(len(names))+" gates") {
		t.Errorf("title %q does not state the %d gates the message lists", n.Title, len(names))
	}
	if got := openFindings(t, s, dir, notGitGateID); len(got) != 1 {
		t.Errorf("the store should hold one %s row, got %d", notGitGateID, len(got))
	}
}

// When the directory later becomes a repository the statement is no longer true
// and must go away, not linger as an open finding.
func TestRunChecks_NotAGitRepoNoticeIsRetiredByGitInit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	dir := t.TempDir()
	if _, err := RunChecks(ctx, s, dir, ""); err != nil {
		t.Fatal(err)
	}
	gitInit(t, dir)
	mustWrite(t, filepath.Join(dir, "README.md"), "# x\n")
	runGit(t, dir, "add", "-A")
	if _, err := RunChecks(ctx, s, dir, ""); err != nil {
		t.Fatal(err)
	}
	for _, f := range openFindings(t, s, dir, notGitGateID) {
		if f.Status == StatusOpen {
			t.Errorf("the not-a-git-repository notice is still open after git init: %+v", f)
		}
	}
}

// Rows written by earlier versions — one open notice per gate — must be retired
// by the first run of this one, or the store keeps showing both.
func TestRunChecks_OldPerGateNoticesAreRetired(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	dir := t.TempDir()
	abs, _ := filepath.Abs(dir)
	for _, g := range []string{"secrets_scan", "network_scan", "compose_lint"} {
		if _, err := s.Upsert(ctx, Finding{Project: abs, GateID: g, Severity: SeverityInfo,
			Title: g + " skipped (not a git repository)", Message: "old", FilePath: ".git"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := RunChecks(ctx, s, dir, ""); err != nil {
		t.Fatal(err)
	}
	for _, g := range []string{"secrets_scan", "network_scan", "compose_lint"} {
		for _, f := range openFindings(t, s, abs, g) {
			if f.Status == StatusOpen {
				t.Errorf("an old %s notice is still open: %+v", g, f)
			}
		}
	}
}

// Asking for ONE gate is asking that gate, and a silent answer would be worse
// than its own notice.
func TestRunChecks_NarrowedRunKeepsTheGatesOwnNotice(t *testing.T) {
	s := newTestStore(t)
	res, err := RunChecks(context.Background(), s, t.TempDir(), "secrets_scan")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Findings) != 1 || res.Findings[0].GateID != "secrets_scan" || res.Findings[0].FilePath != ".git" {
		t.Fatalf("a narrowed run on a non-repo should return that gate's own notice, got %+v", res.Findings)
	}
}

// A clean project leaves no finding behind to say it was ever looked at, so the
// check is recorded on its own.
func TestStats_SaysHowCurrentTheDataIs(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	dir := fullProject(t)
	before := time.Now().UnixMilli()
	if _, err := RunChecks(ctx, s, dir, ""); err != nil {
		t.Fatal(err)
	}
	abs, _ := filepath.Abs(dir)
	st, err := s.Stats(ctx, abs)
	if err != nil {
		t.Fatal(err)
	}
	if st.Total != 0 {
		t.Fatalf("precondition: a full project should be clean, total = %d", st.Total)
	}
	if st.LastCheckedAt == nil || *st.LastCheckedAt < before || *st.LastCheckedAt > time.Now().UnixMilli() {
		t.Errorf("last_checked_at = %v, want a time during the check (>= %d)", st.LastCheckedAt, before)
	}
	if st.ProjectExists == nil || !*st.ProjectExists {
		t.Error("project_exists should be true for a directory that is there")
	}

	// A project that was deleted says so; one never checked says 0.
	gone := filepath.Join(t.TempDir(), "gone")
	if err := os.Mkdir(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordCheck(ctx, gone); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	st, _ = s.Stats(ctx, gone)
	if st.ProjectExists == nil || *st.ProjectExists {
		t.Error("project_exists should be false once the directory is gone")
	}
	if st, _ = s.Stats(ctx, filepath.Join(t.TempDir(), "never")); st.LastCheckedAt == nil || *st.LastCheckedAt != 0 {
		t.Errorf("a project never checked should report 0 (present), got %v", st.LastCheckedAt)
	}

	all, err := s.Stats(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if all.ProjectsTracked == nil || all.ProjectsMissing == nil || *all.ProjectsTracked != 2 || *all.ProjectsMissing != 1 {
		t.Errorf("global: tracked %v missing %v, want 2 and 1", all.ProjectsTracked, all.ProjectsMissing)
	}
}

func seedProject(t *testing.T, s *Store, project string, rows map[string]string) {
	t.Helper()
	for file, status := range rows {
		f, err := s.Upsert(context.Background(), Finding{Project: project, GateID: "g", Severity: SeverityWarning,
			Title: "T", Message: "m " + file, FilePath: file})
		if err != nil {
			t.Fatal(err)
		}
		switch status {
		case StatusResolved:
			if _, err := s.db.Exec(`UPDATE findings SET status = 'resolved' WHERE id = ?`, f.ID); err != nil {
				t.Fatal(err)
			}
		case StatusIgnored:
			if _, err := s.Ignore(context.Background(), f.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func scanCount(t *testing.T, s *Store, query string, args ...any) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func countAll(t *testing.T, s *Store) int {
	t.Helper()
	return scanCount(t, s, `SELECT COUNT(*) FROM findings`)
}

func countProject(t *testing.T, s *Store, project string) int {
	t.Helper()
	return scanCount(t, s, `SELECT COUNT(*) FROM findings WHERE project = ?`, project)
}

func countProjectStatus(t *testing.T, s *Store, project, status string) int {
	t.Helper()
	return scanCount(t, s, `SELECT COUNT(*) FROM findings WHERE project = ? AND status = ?`, project, status)
}

func countProjectFile(t *testing.T, s *Store, project, file string) int {
	t.Helper()
	return scanCount(t, s, `SELECT COUNT(*) FROM findings WHERE project = ? AND file_path = ?`, project, file)
}

// The store with: a live project (open, ignored and resolved findings), a
// project whose directory was deleted, and one on a volume that is not mounted.
func pruneFixture(t *testing.T) (s *Store, dbPath, live, gone, unmounted string) {
	t.Helper()
	dbPath = filepath.Join(t.TempDir(), "findings.db")
	s, err := openStoreAt(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	base := t.TempDir()
	live = filepath.Join(base, "live")
	gone = filepath.Join(base, "gone")
	unmounted = filepath.Join(t.TempDir(), "no-such-volume", "proj") // the PARENT is missing too
	if err := os.Mkdir(live, 0o755); err != nil {
		t.Fatal(err)
	}
	seedProject(t, s, live, map[string]string{"o1": StatusOpen, "o2": StatusOpen, "i1": StatusIgnored, "r1": StatusResolved, "r2": StatusResolved})
	seedProject(t, s, gone, map[string]string{"o1": StatusOpen, "i1": StatusIgnored, "r1": StatusResolved})
	seedProject(t, s, unmounted, map[string]string{"o1": StatusOpen, "r1": StatusResolved})
	for _, p := range []string{live, gone, unmounted} {
		if err := s.RecordCheck(context.Background(), p); err != nil {
			t.Fatal(err)
		}
	}
	// `gone` existed, and is deleted: its parent (base) stays.
	if err := os.Mkdir(gone, 0o755); err != nil && !os.IsExist(err) {
		t.Fatal(err)
	}
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	return s, dbPath, live, gone, unmounted
}

func TestPrune_DryRunChangesNothing(t *testing.T) {
	s, path, _, gone, _ := pruneFixture(t)
	before := countAll(t, s)
	rep, err := s.Prune(context.Background(), path, PruneOptions{KeepResolvedDays: 0})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.DryRun || countAll(t, s) != before {
		t.Errorf("a dry run must not delete anything: dry=%v rows %d -> %d", rep.DryRun, before, countAll(t, s))
	}
	// Resolved = the live project's two plus the unmounted project's one: with no
	// retention window every resolved finding goes, wherever it is. What must NOT
	// go from an unreachable project is its OPEN findings — see
	// TestPrune_NeverTreatsAnUnreachableProjectAsGone.
	if rep.VanishedFindings != 3 || rep.ResolvedFindings != 3 || rep.IgnoredKept != 1 {
		t.Errorf("report = vanished %d resolved %d ignored-kept %d, want 3, 3, 1", rep.VanishedFindings, rep.ResolvedFindings, rep.IgnoredKept)
	}
	if len(rep.VanishedProjects) != 1 || rep.VanishedProjects[0] != gone {
		t.Errorf("vanished projects = %v, want [%s]", rep.VanishedProjects, gone)
	}
}

func TestPrune_RemovesWhatCannotBeActedOn(t *testing.T) {
	s, path, live, gone, _ := pruneFixture(t)
	if _, err := s.Prune(context.Background(), path, PruneOptions{Apply: true, KeepResolvedDays: 0}); err != nil {
		t.Fatal(err)
	}
	if n := countProject(t, s, gone); n != 0 {
		t.Errorf("a deleted project's findings must all go, %d left", n)
	}
	var checks int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM project_checks WHERE project = ?`, gone).Scan(&checks)
	if checks != 0 {
		t.Error("a deleted project's check record must go too")
	}
	if n := countProjectStatus(t, s, live, "resolved"); n != 0 {
		t.Errorf("resolved findings of a live project must go with -keep-resolved-days=0, %d left", n)
	}
	// What the user must act on, and what the user decided, survives.
	if n := countProjectStatus(t, s, live, "open"); n != 2 {
		t.Errorf("open findings of a live project must never be pruned: %d left, want 2", n)
	}
	if n := countProjectStatus(t, s, live, "ignored"); n != 1 {
		t.Errorf("an ignored finding is the user's decision and cannot be regenerated: %d left, want 1", n)
	}
}

// A project on an unmounted volume has no parent either. Its findings are not
// garbage; deleting them because a drive was unplugged would be data loss.
func TestPrune_NeverTreatsAnUnreachableProjectAsGone(t *testing.T) {
	s, path, _, _, unmounted := pruneFixture(t)
	rep, err := s.Prune(context.Background(), path, PruneOptions{Apply: true, KeepResolvedDays: 30})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range rep.VanishedProjects {
		if p == unmounted {
			t.Fatalf("%s was classed as vanished although its parent is missing too", p)
		}
	}
	if n := countProject(t, s, unmounted); n != 2 {
		t.Errorf("an unreachable project's findings must be kept: %d left, want 2", n)
	}
}

func TestPrune_KeepsRecentResolvedFindings(t *testing.T) {
	s, path, live, _, _ := pruneFixture(t)
	old := time.Now().Add(-40 * 24 * time.Hour).UnixMilli()
	if _, err := s.db.Exec(`UPDATE findings SET updated_at = ? WHERE project = ? AND file_path = 'r1'`, old, live); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Prune(context.Background(), path, PruneOptions{Apply: true, KeepResolvedDays: 30}); err != nil {
		t.Fatal(err)
	}
	if n := countProjectFile(t, s, live, "r1"); n != 0 {
		t.Error("a resolved finding older than the window must go")
	}
	if n := countProjectFile(t, s, live, "r2"); n != 1 {
		t.Error("a resolved finding inside the window must stay")
	}
}

// Removing a row is not removing its bytes: the pruned text must not be
// readable in the file afterwards.
func TestPrune_LeavesNoRemnantsOnDisk(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "findings.db")
	s, err := openStoreAt(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	// A deleted project leaves its neighbours behind; an EMPTY parent is how an
	// unmounted volume looks, and is (rightly) never pruned.
	if err := os.WriteFile(filepath.Join(base, "neighbour.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(base, "gone")
	const marker = "PruneRemnantMarker7Zk2"
	for i := 0; i < 300; i++ {
		if _, err := s.Upsert(context.Background(), Finding{Project: gone, GateID: "g", Severity: SeverityInfo,
			Title: "T", Message: marker + strings.Repeat(" pad", 20), FilePath: "f" + string(rune('a'+i%26)) + strconv.Itoa(i%100) + string(rune('A'+i/100))}); err != nil {
			t.Fatal(err)
		}
	}
	rep, err := s.Prune(context.Background(), dbPath, PruneOptions{Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if bytes.Contains(rawBytes(t, dbPath, dbPath+"-wal", dbPath+"-shm"), []byte(marker)) {
		t.Error("pruned findings are still readable in the database file")
	}
	// secure_delete already overwrites the removed bytes; the VACUUM is what gives
	// the disk space back, which is most of what pruning a 39 MB store is for.
	if !rep.Compacted || rep.BytesAfter >= rep.BytesBefore {
		t.Errorf("pruning 300 rows should shrink the file: compacted=%v, %d -> %d bytes", rep.Compacted, rep.BytesBefore, rep.BytesAfter)
	}
}

func TestParsePruneFlags(t *testing.T) {
	o, err := parsePruneFlags(nil)
	if err != nil || o.Apply || o.KeepResolvedDays != defaultKeepResolvedDays {
		t.Errorf("default = %+v (%v): must be a dry run keeping %d days", o, err, defaultKeepResolvedDays)
	}
	if o, err = parsePruneFlags([]string{"-apply", "-keep-resolved-days=0"}); err != nil || !o.Apply || o.KeepResolvedDays != 0 {
		t.Errorf("-apply -keep-resolved-days=0 = %+v (%v)", o, err)
	}
	if o, err = parsePruneFlags([]string{"--keep-resolved-days", "7"}); err != nil || o.KeepResolvedDays != 7 {
		t.Errorf("space-separated value = %+v (%v)", o, err)
	}
	for _, bad := range [][]string{{"-keep-resolved-days=-1"}, {"-keep-resolved-days=abc"}, {"-keep-resolved-days"}, {"-apply=yes"}, {"-force"}, {"positional"}} {
		if _, err := parsePruneFlags(bad); err == nil {
			t.Errorf("%v must be rejected", bad)
		}
	}
}

// One file is one file, and the verb agrees with the count.
func TestIgnoredTrackedMessage_Wording(t *testing.T) {
	one := ignoredTrackedMessage("Cargo.lock", []string{"Cargo.lock"})
	if !strings.HasPrefix(one, "Cargo.lock is tracked but matches the repository's .gitignore.") {
		t.Errorf("single-file wording: %q", one)
	}
	if strings.Contains(one, "under Cargo.lock") || strings.Contains(one, "1 tracked") {
		t.Errorf("a single file is not a directory, nor '1 tracked files': %q", one)
	}
	many := ignoredTrackedMessage("data/", []string{"data/a", "data/b", "data/c", "data/d"})
	if !strings.Contains(many, "4 tracked files under data/ match the repository's .gitignore") {
		t.Errorf("multi-file wording: %q", many)
	}
}

// classifyProject decides GONE versus OFFLINE, and getting it wrong in the
// first direction deletes findings because a drive was unplugged.
func TestClassifyProject(t *testing.T) {
	root := t.TempDir()
	mk := func(p string) string {
		t.Helper()
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	write := func(dir string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "neighbour"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	live := mk(filepath.Join(root, "docs", "live"))
	if classifyProject(live) != projectPresent {
		t.Error("an existing directory is present")
	}

	// A deleted project, its neighbours still there.
	parent := mk(filepath.Join(root, "git"))
	write(parent)
	if got := classifyProject(filepath.Join(parent, "deleted")); got != projectVanished {
		t.Errorf("a deleted project next to its neighbours = %v, want vanished", got)
	}

	// A whole TREE deleted — `work/` and every clone in it. These were the
	// largest piles of findings nothing could ever resolve, and the first version
	// of the rule ("the parent must exist") left all of them alone.
	if got := classifyProject(filepath.Join(parent, "work", "clones", "audiolibri")); got != projectVanished {
		t.Errorf("a project whose whole ancestor chain was deleted, under a populated directory = %v, want vanished", got)
	}

	// An empty directory above it is how an unmounted volume's mount point looks.
	empty := mk(filepath.Join(root, "mountpoint"))
	if got := classifyProject(filepath.Join(empty, "proj")); got != projectUnreachable {
		t.Errorf("a missing project under an EMPTY directory = %v, want unreachable (it may be an unmounted volume)", got)
	}

	// Below a mount root whose mount point is missing: offline, whatever else is there.
	vols := mk(filepath.Join(root, "Volumes"))
	write(vols)
	old := mountRoots
	mountRoots = []string{vols}
	t.Cleanup(func() { mountRoots = old })
	if got := classifyProject(filepath.Join(vols, "USB", "proj")); got != projectUnreachable {
		t.Errorf("a project on a volume that is not mounted = %v, want unreachable", got)
	}
	// With the volume mounted: a missing directory right under the mount point is
	// still too shallow to call deleted (the drive may simply not be the one
	// that was used), but one deep inside it, with its neighbours present, is gone.
	usb := mk(filepath.Join(vols, "USB"))
	write(usb)
	if got := classifyProject(filepath.Join(usb, "proj")); got != projectUnreachable {
		t.Errorf("a project directly under a mount point = %v, want unreachable", got)
	}
	deep := mk(filepath.Join(usb, "a", "b", "c"))
	write(deep)
	if got := classifyProject(filepath.Join(deep, "deleted")); got != projectVanished {
		t.Errorf("a project deleted from deep inside a mounted volume = %v, want vanished", got)
	}

	// Nothing above it exists at all.
	if got := classifyProject(filepath.Join(string(filepath.Separator), "definitely-not-a-real-root-dir-xyz", "a", "b")); got == projectVanished {
		t.Errorf("a path with no existing ancestor but the filesystem root must not be pruned, got %v", got)
	}
}
