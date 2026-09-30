//go:build unix

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// An independent review of `lgit prune` — the one command that deletes — found
// these. Each reproduced on the first version of the branch.

func mkdirs(t *testing.T, p string) string {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func touch(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "x"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// DATA LOSS: `~/proj -> /Volumes/USB/proj` with the drive unplugged. os.Stat
// follows the link, says "not found", and the link itself makes the parent
// non-empty — so the project looked deleted and prune removed its findings,
// the ignored ones included.
func TestReview_DanglingSymlinkIsOfflineNotGone(t *testing.T) {
	root := t.TempDir()
	home := mkdirs(t, filepath.Join(root, "home"))
	touch(t, home)
	target := filepath.Join(root, "unplugged", "proj") // never created: the drive is away

	link := filepath.Join(home, "proj")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if got := classifyProject(link); got != projectUnreachable {
		t.Errorf("a project that is a dangling symlink = %v, want unreachable", got)
	}

	// The same when an ANCESTOR is the dangling link: ~/code -> /Volumes/T7/code.
	codeLink := filepath.Join(home, "code")
	if err := os.Symlink(filepath.Join(root, "unplugged", "code"), codeLink); err != nil {
		t.Fatal(err)
	}
	if got := classifyProject(filepath.Join(codeLink, "app")); got != projectUnreachable {
		t.Errorf("a project below a dangling symlink = %v, want unreachable", got)
	}

	// End to end: nothing is deleted.
	s, path, _, _, _ := pruneFixture(t)
	seedProject(t, s, link, map[string]string{"o": StatusOpen, "i": StatusIgnored})
	rep, err := s.Prune(context.Background(), path, PruneOptions{Apply: true, KeepResolvedDays: 0})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range rep.VanishedProjects {
		if p == link {
			t.Fatalf("%s was pruned although it is a link to a drive that is not there", link)
		}
	}
	if n := countProject(t, s, link); n != 2 {
		t.Errorf("an offline project's open and ignored findings must survive: %d left, want 2", n)
	}
}

// DATA LOSS: udisks mounts removable drives at /media/USER/LABEL. With USB1
// mounted and USB2 unplugged, `/media/alice/USB2/proj` is missing, its nearest
// ancestor `/media/alice` exists and is non-empty (USB1 is in it), and the
// first-component mount-point rule looked at `/media/alice` — which exists.
func TestReview_TwoUSBDrivesUnderMediaUser(t *testing.T) {
	root := t.TempDir()
	media := mkdirs(t, filepath.Join(root, "media"))
	usb1 := mkdirs(t, filepath.Join(media, "alice", "USB1"))
	touch(t, usb1)
	old := mountRoots
	mountRoots = []string{media}
	t.Cleanup(func() { mountRoots = old })

	if got := classifyProject(filepath.Join(media, "alice", "USB2", "proj")); got != projectUnreachable {
		t.Errorf("a project on the unplugged second drive = %v, want unreachable", got)
	}
	// Even a project under the MOUNTED drive, one level down: a drive that can be
	// unplugged is not evidence of deletion, and a missing directory directly
	// under a mount point is too shallow to tell.
	if got := classifyProject(filepath.Join(usb1, "proj")); got != projectUnreachable {
		t.Errorf("a project directly under a mount point = %v, want unreachable", got)
	}
	// Deep inside a mounted drive, with its neighbours present, it IS gone.
	mkdirs(t, filepath.Join(usb1, "work", "code"))
	touch(t, filepath.Join(usb1, "work", "code"))
	if got := classifyProject(filepath.Join(usb1, "work", "code", "deleted")); got != projectVanished {
		t.Errorf("a project deleted from deep inside a mounted drive = %v, want vanished", got)
	}
}

// DATA LOSS (resolved rows): time.Duration overflows int64 at 106,752 days, so a
// natural way of writing "forever" put the cutoff in the FUTURE and deleted every
// resolved finding — and the dry run inverted the same way, so it did not warn.
func TestReview_KeepResolvedDaysHasAnUpperBound(t *testing.T) {
	for _, bad := range []string{"106752", "200000", "999999", "9999999", "1099511627776"} {
		if _, err := parsePruneFlags([]string{"-keep-resolved-days=" + bad}); err == nil {
			t.Errorf("-keep-resolved-days=%s must be rejected", bad)
		}
	}
	if o, err := parsePruneFlags([]string{"-keep-resolved-days=36500"}); err != nil || o.KeepResolvedDays != 36500 {
		t.Errorf("36500 days (a century) is the largest window: %+v %v", o, err)
	}
	s, path, live, _, _ := pruneFixture(t)
	if _, err := s.Prune(context.Background(), path, PruneOptions{Apply: true, KeepResolvedDays: 999999}); err == nil {
		t.Error("Prune itself must refuse an absurd window, not only the flag parser")
	}
	if n := countProjectStatus(t, s, live, StatusResolved); n != 2 {
		t.Errorf("a refused prune must delete nothing: %d resolved left, want 2", n)
	}
	// And the largest allowed window keeps a fresh resolved row.
	if _, err := s.Prune(context.Background(), path, PruneOptions{Apply: true, KeepResolvedDays: 36500}); err != nil {
		t.Fatal(err)
	}
	if n := countProjectStatus(t, s, live, StatusResolved); n != 2 {
		t.Errorf("a 100-year window must keep a fresh resolved finding: %d left", n)
	}
}

// Old IGNORED findings are the user's decisions too: the default 30-day path must
// not touch them (the first tests only exercised a window of 0).
func TestReview_OldIgnoredFindingsSurviveTheDefaultWindow(t *testing.T) {
	s, path, live, _, _ := pruneFixture(t)
	old := time.Now().Add(-90 * 24 * time.Hour).UnixMilli()
	if _, err := s.db.Exec(`UPDATE findings SET updated_at = ? WHERE project = ?`, old, live); err != nil {
		t.Fatal(err)
	}
	rep, err := s.Prune(context.Background(), path, PruneOptions{Apply: true, KeepResolvedDays: defaultKeepResolvedDays})
	if err != nil {
		t.Fatal(err)
	}
	if n := countProjectStatus(t, s, live, StatusIgnored); n != 1 {
		t.Errorf("an old ignored finding was pruned: %d left, want 1", n)
	}
	if n := countProjectStatus(t, s, live, StatusOpen); n != 2 {
		t.Errorf("an old open finding was pruned: %d left, want 2", n)
	}
	if n := countProjectStatus(t, s, live, StatusResolved); n != 0 {
		t.Errorf("old resolved findings should go: %d left", n)
	}
	// Only the live project's two resolved rows are old; the unreachable project's
	// one was written just now.
	if rep.ResolvedFindings != 2 || rep.IgnoredKept != 1 {
		t.Errorf("report = resolved %d ignored-kept %d, want 2 and 1", rep.ResolvedFindings, rep.IgnoredKept)
	}
}

// The dry run must report exactly what -apply then removes, with a window.
func TestReview_DryRunCountsEqualWhatApplyRemoves(t *testing.T) {
	s, path, live, _, _ := pruneFixture(t)
	old := time.Now().Add(-90 * 24 * time.Hour).UnixMilli()
	if _, err := s.db.Exec(`UPDATE findings SET updated_at = ? WHERE project = ? AND file_path = 'r1'`, old, live); err != nil {
		t.Fatal(err)
	}
	dry, err := s.Prune(context.Background(), path, PruneOptions{KeepResolvedDays: 30})
	if err != nil {
		t.Fatal(err)
	}
	before := countAll(t, s)
	if before != countAll(t, s) {
		t.Fatal("a dry run must not delete")
	}
	if _, err := s.Prune(context.Background(), path, PruneOptions{Apply: true, KeepResolvedDays: 30}); err != nil {
		t.Fatal(err)
	}
	removed := before - countAll(t, s)
	if want := dry.VanishedFindings + dry.ResolvedFindings; removed != want {
		t.Errorf("the dry run promised %d rows and -apply removed %d", want, removed)
	}
	if dry.ResolvedFindings != 1 {
		t.Errorf("only r1 is older than the window: dry-run resolved = %d, want 1", dry.ResolvedFindings)
	}
}

// A stat error that is not "not found" (permission denied) is no evidence of
// deletion.
func TestReview_PermissionDeniedIsNotDeletion(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read everything")
	}
	root := t.TempDir()
	locked := mkdirs(t, filepath.Join(root, "locked"))
	touch(t, root)
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	if got := classifyProject(filepath.Join(locked, "proj")); got != projectUnreachable {
		t.Errorf("a project behind a directory we may not read = %v, want unreachable", got)
	}
}

// `lgit clear <project>` is how a kept, unreachable project is removed by hand;
// it left its check record behind, so the project stayed in every count and in
// the prune report for ever.
func TestReview_ClearRemovesTheCheckRecord(t *testing.T) {
	s, _, _, _, unmounted := pruneFixture(t)
	if _, err := s.ClearProject(context.Background(), unmounted); err != nil {
		t.Fatal(err)
	}
	ps, err := s.knownProjects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ps {
		if p == unmounted {
			t.Errorf("%s is still a known project after `lgit clear`", p)
		}
	}
}

// The freshness bookkeeping must not lie. A run narrowed to ONE gate is not a
// check of the project; and a narrowed run after `git init` still knows the
// directory is a repository now.
func TestReview_NarrowedRunsAndFreshness(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	dir := fullProject(t)
	abs, _ := filepath.Abs(dir)

	if _, err := RunChecks(ctx, s, dir, "license_present"); err != nil {
		t.Fatal(err)
	}
	st, _ := s.Stats(ctx, abs)
	if st.LastCheckedAt != nil && *st.LastCheckedAt != 0 {
		t.Errorf("checking ONE gate is not checking the project: last_checked_at = %d", *st.LastCheckedAt)
	}
	if _, err := RunChecks(ctx, s, dir, ""); err != nil {
		t.Fatal(err)
	}
	if st, _ = s.Stats(ctx, abs); st.LastCheckedAt == nil || *st.LastCheckedAt == 0 {
		t.Error("a full run must record the check")
	}

	// A narrowed run must not retire the not-a-git-repository notice of a
	// directory that still is not one…
	plain := t.TempDir()
	if _, err := RunChecks(ctx, s, plain, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := RunChecks(ctx, s, plain, "secrets_scan"); err != nil {
		t.Fatal(err)
	}
	if n := openCount(t, s, plain, notGitGateID); n != 1 {
		t.Errorf("a narrowed run on a non-repo retired the notice: %d open, want 1", n)
	}
	// …but it must retire it once the directory HAS become a repository.
	gitInit(t, plain)
	mustWrite(t, filepath.Join(plain, "README.md"), "# x\n")
	runGit(t, plain, "add", "-A")
	if _, err := RunChecks(ctx, s, plain, "secrets_scan"); err != nil {
		t.Fatal(err)
	}
	if n := openCount(t, s, plain, notGitGateID); n != 0 {
		t.Errorf("after git init a narrowed run left the notice open: %d", n)
	}
}

func openCount(t *testing.T, s *Store, project, gate string) int {
	t.Helper()
	abs, _ := filepath.Abs(project)
	return scanCount(t, s, `SELECT COUNT(*) FROM findings WHERE project = ? AND gate_id = ? AND status = 'open'`, abs, gate)
}

// The fold keys on BOTH the `.git` path and the title suffix: a real gate
// finding that happens to live at `.git` (the "could not enumerate" kind) must
// not be swallowed.
func TestReview_TheFoldDoesNotSwallowARealFindingAtDotGit(t *testing.T) {
	in := []Finding{
		{Severity: SeverityWarning, Title: "compose_lint failed", FilePath: ".git", Message: "Could not enumerate tracked files: boom"},
		{Severity: SeverityInfo, Title: "secrets_scan skipped (not a git repository)", FilePath: ".git"},
		{Severity: SeverityWarning, Title: "whatever", FilePath: "a.go:3:rule"},
	}
	out, found := withoutNotGitNotice(in)
	if !found || len(out) != 2 {
		t.Fatalf("want the one real notice removed and two findings kept, got found=%v %+v", found, out)
	}
	for _, f := range out {
		if strings.HasSuffix(f.Title, "skipped (not a git repository)") {
			t.Errorf("the notice survived: %+v", f)
		}
	}
}

// The notice used to be silenced by ignoring each gate; ignoring its id must work.
func TestReview_ConfigCanIgnoreTheNotGitNotice(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".l0git.json"), `{"ignore": ["git_repository"]}`)
	res, err := RunChecks(context.Background(), s, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range res.Findings {
		if f.GateID == notGitGateID {
			t.Errorf("an ignored git_repository notice was reported: %+v", f)
		}
	}
}

// Stats says what it means: a check that is not on record is 0 and PRESENT, and
// the two scopes carry different fields.
func TestReview_StatsJSONIsExplicit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	never := filepath.Join(t.TempDir(), "never")
	proj, _ := s.Stats(ctx, never)
	b, _ := json.Marshal(proj)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if v, ok := m["last_checked_at"]; !ok || v.(float64) != 0 {
		t.Errorf("project scope: last_checked_at must be present and 0 when nothing is on record: %s", b)
	}
	if _, ok := m["project_exists"]; !ok {
		t.Errorf("project scope: project_exists missing: %s", b)
	}
	if _, ok := m["projects_tracked"]; ok {
		t.Errorf("project scope must not carry the global fields: %s", b)
	}
	all, _ := s.Stats(ctx, "")
	b, _ = json.Marshal(all)
	m = map[string]any{}
	_ = json.Unmarshal(b, &m)
	for _, k := range []string{"projects_tracked", "projects_missing"} {
		if _, ok := m[k]; !ok {
			t.Errorf("global scope: %s must be present even when 0: %s", k, b)
		}
	}
	if _, ok := m["last_checked_at"]; ok {
		t.Errorf("global scope must not carry last_checked_at: %s", b)
	}
}
