package main

import (
	"strings"
	"testing"
)

// =============================================================================
// per-gate dispatch
// =============================================================================

func TestRemediationFor_VendoredDir(t *testing.T) {
	r := RemediationFor(Finding{
		ID:       7,
		Project:  "/p",
		GateID:   "vendored_dir_tracked",
		Severity: SeverityWarning,
		Title:    "Vendored directory tracked in git",
		FilePath: "node_modules",
	})
	if r.Confidence != ConfidenceDeter {
		t.Fatalf("want deterministic, got %q", r.Confidence)
	}
	if r.Recipe == nil {
		t.Fatal("expected a recipe")
	}
	wantCmd := "git rm -r --cached node_modules"
	if r.Recipe.Commands[0].Run != wantCmd {
		t.Errorf("first command:\n  got  %q\n  want %q", r.Recipe.Commands[0].Run, wantCmd)
	}
	if len(r.Recipe.FileEdits) != 1 || r.Recipe.FileEdits[0].Path != ".gitignore" {
		t.Errorf("expected single .gitignore append, got %+v", r.Recipe.FileEdits)
	}
	if !strings.Contains(r.Recipe.FileEdits[0].Content, "node_modules/") {
		t.Errorf(".gitignore content missing the directory: %q", r.Recipe.FileEdits[0].Content)
	}
}

func TestRemediationFor_IdeArtifactPicksDirGlob(t *testing.T) {
	// .vscode/ subpath should ignore the whole directory, not the
	// specific file — that's what users expect.
	r := RemediationFor(Finding{
		GateID:   "ide_artifact_tracked",
		FilePath: ".vscode/settings.json",
	})
	if r.Recipe == nil {
		t.Fatal("expected recipe")
	}
	if got := r.Recipe.FileEdits[0].Content; !strings.Contains(got, ".vscode/") {
		t.Errorf("expected .vscode/ ignore, got %q", got)
	}
}

func TestRemediationFor_GitignoreCoveragePullsPattern(t *testing.T) {
	r := RemediationFor(Finding{
		GateID:   "gitignore_coverage",
		FilePath: ".gitignore:node_modules",
	})
	if r.Recipe == nil {
		t.Fatal("expected recipe")
	}
	if r.Recipe.FileEdits[0].Path != ".gitignore" {
		t.Errorf("wrong file: %q", r.Recipe.FileEdits[0].Path)
	}
	if !strings.HasPrefix(r.Recipe.FileEdits[0].Content, "node_modules") {
		t.Errorf("expected pattern in content, got %q", r.Recipe.FileEdits[0].Content)
	}
}

func TestRemediationFor_ExecBitUsesPortableCommand(t *testing.T) {
	r := RemediationFor(Finding{
		GateID:   "unexpected_executable_bit",
		FilePath: "README.md",
	})
	if r.Recipe == nil || len(r.Recipe.Commands) == 0 {
		t.Fatal("expected commands")
	}
	if !strings.Contains(r.Recipe.Commands[0].Run, "git update-index --chmod=-x") {
		t.Errorf("expected portable chmod command, got %q", r.Recipe.Commands[0].Run)
	}
}

func TestRemediationFor_EnvExampleParsesLineAndKey(t *testing.T) {
	r := RemediationFor(Finding{
		GateID:   "env_example_uncommented",
		FilePath: ".env.example:7:DATABASE_URL",
	})
	if r.Recipe == nil || len(r.Recipe.FileEdits) != 1 {
		t.Fatalf("expected single edit, got %+v", r.Recipe)
	}
	e := r.Recipe.FileEdits[0]
	if e.Op != OpInsertBeforeLine || e.Line != 7 || e.Path != ".env.example" {
		t.Errorf("wrong edit shape: %+v", e)
	}
	if !strings.Contains(e.Content, "DATABASE_URL") {
		t.Errorf("expected key in placeholder content, got %q", e.Content)
	}
	// Caveat is essential — the placeholder is a TODO, not a real
	// description, and the user needs to be told that.
	if len(r.Recipe.Caveats) == 0 {
		t.Error("expected a caveat about the TODO placeholder")
	}
}

func TestRemediationFor_MergeConflictIsGuided(t *testing.T) {
	r := RemediationFor(Finding{
		GateID:   "merge_conflict_markers",
		FilePath: "src/main.go",
	})
	if r.Confidence != ConfidenceGuided || r.Recipe != nil {
		t.Errorf("merge conflict should be guided (no recipe), got %+v", r)
	}
}

func TestRemediationFor_SecretsHistoryFlagsRotation(t *testing.T) {
	r := RemediationFor(Finding{
		GateID: "secrets_scan_history",
	})
	if r.Recipe == nil || len(r.Recipe.Caveats) == 0 {
		t.Fatal("expected recipe with caveats")
	}
	// "Rotate first" must be the loudest signal — not just buried in
	// the prompt. Look for it in caveats.
	joined := strings.ToUpper(strings.Join(r.Recipe.Caveats, " "))
	if !strings.Contains(joined, "ROTATE") {
		t.Errorf("expected ROTATE caveat to be prominent, got: %v", r.Recipe.Caveats)
	}
}

func TestRemediationFor_UnknownGateFallsBackToGuided(t *testing.T) {
	r := RemediationFor(Finding{
		GateID: "secrets_scan", // no deterministic recipe — needs rotation
		Title:  "API key in source",
	})
	if r.Confidence != ConfidenceGuided {
		t.Errorf("expected guided, got %q", r.Confidence)
	}
	if r.ClaudePrompt == "" {
		t.Error("ClaudePrompt must always be populated, even for guided")
	}
	if r.Recipe != nil {
		t.Error("guided should not produce a recipe")
	}
}

// =============================================================================
// shellQuote
// =============================================================================

func TestShellQuote_NoEscapeNeeded(t *testing.T) {
	cases := map[string]string{
		"":                "''",
		"node_modules":    "node_modules",
		"src/main.go":     "src/main.go",
		"path with space": "'path with space'",
		"it's":            `'it'\''s'`,
		"$(rm -rf /)":     `'$(rm -rf /)'`,
	}
	for in, want := range cases {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

// =============================================================================
// ClaudePrompt content
// =============================================================================

func TestClaudePrompt_ReferencesMCPTools(t *testing.T) {
	r := RemediationFor(Finding{
		ID:      99,
		GateID:  "vendored_dir_tracked",
		Project: "/srv/proj",
		FilePath: "vendor",
		Title:    "x",
	})
	if !strings.Contains(r.ClaudePrompt, "findings_remediate") {
		t.Error("prompt should mention findings_remediate so the agent can re-fetch context")
	}
	if !strings.Contains(r.ClaudePrompt, "lgit check") {
		t.Error("prompt should include the verification step")
	}
	if !strings.Contains(r.ClaudePrompt, "/srv/proj") {
		t.Error("prompt should include the project path")
	}
}

// =============================================================================
// RenderRemediationText
// =============================================================================

func TestRenderRemediationText_DeterministicHasAllSections(t *testing.T) {
	f := Finding{
		ID: 1, Project: "/p", GateID: "vendored_dir_tracked",
		Severity: SeverityWarning, Title: "Vendored directory tracked",
		FilePath: "node_modules", Message: "node_modules is tracked.",
	}
	var sb strings.Builder
	RenderRemediationText(&sb, f, RemediationFor(f))
	out := sb.String()
	for _, want := range []string{
		"l0-git finding #1",
		"vendored_dir_tracked",
		"Detected", "Fix", "Run", "Edit", "Verify", "Hand off to Claude Code",
		"--- prompt ---", "--- end ---",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered text missing %q\n--- output ---\n%s", want, out)
		}
	}
}

func TestRenderRemediationText_GuidedSkipsRunSection(t *testing.T) {
	f := Finding{
		ID: 2, Project: "/p", GateID: "merge_conflict_markers",
		Severity: SeverityError, Title: "Merge conflict markers",
		FilePath: "src/x.go", Message: "src/x.go:14 has markers.",
	}
	var sb strings.Builder
	RenderRemediationText(&sb, f, RemediationFor(f))
	out := sb.String()
	// "Run" header should not appear when there are no commands.
	if strings.Contains(out, "\nRun\n") {
		t.Errorf("guided remediation should not have a Run section, got:\n%s", out)
	}
	// Verify section also skipped (nothing to verify after a no-op recipe).
	if strings.Contains(out, "\nVerify\n") {
		t.Errorf("guided remediation should not have a Verify section, got:\n%s", out)
	}
	// Prompt block always present.
	if !strings.Contains(out, "--- prompt ---") {
		t.Error("prompt block must always be present")
	}
}
