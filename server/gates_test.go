package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
)

// emptyProject creates a fresh empty directory and returns its path.
func emptyProject(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// fullProject creates a directory tree containing every file the hygiene
// gates expect, so that none of them fire. Returns the project root.
func fullProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "README.md"), "# x")
	mustWrite(t, filepath.Join(root, "LICENSE"), "MIT")
	mustWrite(t, filepath.Join(root, ".gitignore"), "# noop")
	mustWrite(t, filepath.Join(root, "CONTRIBUTING.md"), "# c")
	mustWrite(t, filepath.Join(root, "SECURITY.md"), "# s")
	mustWrite(t, filepath.Join(root, "CHANGELOG.md"), "# c")
	if err := os.MkdirAll(filepath.Join(root, ".github", "workflows"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(root, ".github", "workflows", "ci.yml"), "name: ci")
	mustWrite(t, filepath.Join(root, ".github", "PULL_REQUEST_TEMPLATE.md"), "# pr")
	if err := os.MkdirAll(filepath.Join(root, ".github", "ISSUE_TEMPLATE"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(root, ".github", "ISSUE_TEMPLATE", "bug.md"), "# bug")
	mustWrite(t, filepath.Join(root, "CODE_OF_CONDUCT.md"), "# CoC")
	// A test file so tests_present passes.
	mustWrite(t, filepath.Join(root, "thing_test.go"), "package x")
	// secrets_scan needs a git repo to enumerate tracked files; with no
	// commits, ls-files is empty and the gate cleanly returns nothing.
	gitInit(t, root)
	return root
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gateIDs(findings []Finding) []string {
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, f.GateID)
	}
	sort.Strings(out)
	return out
}

// TestRunChecks_EmptyDir confirms the full battery fires on an empty
// directory and produces a finding for every "missing X" gate.
func TestRunChecks_EmptyDir(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	res, err := RunChecks(ctx, store, emptyProject(t), "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"changelog_present",
		"ci_workflow_present",
		"code_of_conduct_present",
		"compose_lint",              // skipped (not git)
		"connection_strings",        // skipped (not git)
		"contributing_present",
		"css_lint",                  // skipped (not git)
		"dead_placeholders",         // skipped (not git)
		"dockerfile_lint",           // skipped (not git)
		"filename_quality",          // skipped (not git)
		"gitignore_present",
		"html_lint",                 // skipped (not git)
		"ide_artifact_tracked",      // skipped (not git)
		"issue_template_present",
		"large_file_tracked",        // skipped (not git)
		"license_present",
		"markdown_lint",             // skipped (not git)
		"merge_conflict_markers",    // skipped (not git)
		"network_scan",              // skipped (not git)
		"pr_template_present",
		"readme_present",
		"secrets_scan",              // skipped (not git)
		"security_present",
		"tests_present",
		"unexpected_executable_bit", // skipped (not git)
		"vendored_dir_tracked",      // skipped (not git)
		// gitignore_coverage / codeowners_present / env_example_uncommented
		// stay silent on stack-less empty dirs.
	}
	got := gateIDs(res.Findings)
	if !equalStringSets(got, want) {
		t.Fatalf("findings: got %v, want %v", got, want)
	}
}

// TestRunChecks_AllPresent confirms zero findings when every expected file
// exists. tests_present sees thing_test.go.
func TestRunChecks_AllPresent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	res, err := RunChecks(ctx, store, fullProject(t), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Findings) != 0 {
		t.Fatalf("expected zero findings, got: %+v", res.Findings)
	}
}

// TestRunChecks_SingleGate honours the gate_id filter and runs nothing else.
func TestRunChecks_SingleGate(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	res, err := RunChecks(ctx, store, emptyProject(t), "readme_present")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.GatesRun) != 1 || res.GatesRun[0] != "readme_present" {
		t.Fatalf("gates_run: %v", res.GatesRun)
	}
	if len(res.Findings) != 1 || res.Findings[0].GateID != "readme_present" {
		t.Fatalf("findings: %+v", res.Findings)
	}
}

// TestRunChecks_ConfigIgnore: gate IDs in .l0git.json's "ignore" list don't
// run AND any pre-existing open finding for them is closed.
func TestRunChecks_ConfigIgnore(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	root := emptyProject(t)

	// First run with no config — readme_present should fire.
	if _, err := RunChecks(ctx, store, root, ""); err != nil {
		t.Fatal(err)
	}
	open, _ := store.List(ctx, FindingFilter{Project: root, Status: StatusOpen, Limit: 100})
	hasReadme := false
	for _, f := range open {
		if f.GateID == "readme_present" {
			hasReadme = true
			break
		}
	}
	if !hasReadme {
		t.Fatalf("expected readme_present finding before ignore")
	}

	// Now drop a config and re-run.
	mustWrite(t, filepath.Join(root, ".l0git.json"), `{"ignore": ["readme_present"]}`)
	res, err := RunChecks(ctx, store, root, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range res.GatesRun {
		if id == "readme_present" {
			t.Fatalf("readme_present should be ignored, but it ran")
		}
	}
	if !contains(res.GatesIgnored, "readme_present") {
		t.Fatalf("gates_ignored: %v", res.GatesIgnored)
	}
	open, _ = store.List(ctx, FindingFilter{Project: root, Status: StatusOpen, Limit: 100})
	for _, f := range open {
		if f.GateID == "readme_present" {
			t.Fatalf("ignored gate left an open finding behind: %+v", f)
		}
	}
}

// TestRunChecks_SeverityOverride: the config can dial a gate's severity up
// or down. The persisted finding must reflect the override.
func TestRunChecks_SeverityOverride(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	root := emptyProject(t)

	mustWrite(t, filepath.Join(root, ".l0git.json"), `{"severity": {"readme_present": "error"}}`)
	res, err := RunChecks(ctx, store, root, "readme_present")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Findings) != 1 {
		t.Fatalf("findings: %+v", res.Findings)
	}
	if res.Findings[0].Severity != SeverityError {
		t.Fatalf("severity = %q, want error", res.Findings[0].Severity)
	}
}

// TestRunChecks_ConfigError: a malformed .l0git.json surfaces ConfigError
// but does not abort the run.
func TestRunChecks_ConfigError(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	root := emptyProject(t)
	mustWrite(t, filepath.Join(root, ".l0git.json"), `{ "severity": { "x": "lol" } }`)

	res, err := RunChecks(ctx, store, root, "readme_present")
	if err != nil {
		t.Fatal(err)
	}
	if res.ConfigError == "" {
		t.Fatalf("expected ConfigError to be set")
	}
	if len(res.Findings) == 0 {
		t.Fatalf("run aborted on bad config; findings should still be produced")
	}
}

func equalStringSets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[string]int{}
	for _, x := range a {
		m[x]++
	}
	for _, x := range b {
		m[x]--
	}
	for _, v := range m {
		if v != 0 {
			return false
		}
	}
	return true
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
