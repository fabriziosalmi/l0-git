package main

import (
	"os"
	"path/filepath"
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
	}, ChannelMCP)
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
	}, ChannelMCP)
	if r.Recipe == nil {
		t.Fatal("expected recipe")
	}
	if got := r.Recipe.FileEdits[0].Content; !strings.Contains(got, ".vscode/") {
		t.Errorf("expected .vscode/ ignore, got %q", got)
	}
}

func TestRemediationFor_IdeArtifactSkipsRedundantIgnore(t *testing.T) {
	// .vscode/ is ALREADY in .gitignore -> the recipe must NOT re-append it (that redundant
	// edit would get a downstream agent to drop the whole fix), but the untrack must remain.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("node_modules/\n.vscode/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := RemediationFor(Finding{GateID: "ide_artifact_tracked", Project: dir, FilePath: ".vscode/settings.json"}, ChannelMCP)
	if r.Recipe == nil {
		t.Fatal("expected recipe")
	}
	if len(r.Recipe.FileEdits) != 0 {
		t.Errorf("expected no .gitignore edit (already covered), got %+v", r.Recipe.FileEdits)
	}
	if len(r.Recipe.Commands) == 0 || !strings.Contains(r.Recipe.Commands[0].Run, "git rm --cached") {
		t.Errorf("the untrack command must remain, got %+v", r.Recipe.Commands)
	}
	if !strings.Contains(r.Summary, "already covered") {
		t.Errorf("summary should note already-covered, got %q", r.Summary)
	}
}

func TestRemediationFor_IdeArtifactAddsIgnoreWhenMissing(t *testing.T) {
	// .gitignore exists but does NOT cover .vscode/ -> the append is still produced.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("node_modules/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := RemediationFor(Finding{GateID: "ide_artifact_tracked", Project: dir, FilePath: ".vscode/settings.json"}, ChannelMCP)
	if r.Recipe == nil || len(r.Recipe.FileEdits) != 1 || !strings.Contains(r.Recipe.FileEdits[0].Content, ".vscode/") {
		t.Errorf("expected a .vscode/ .gitignore edit when not covered, got %+v", r.Recipe)
	}
}

func TestRemediationFor_VendoredDirSkipsRedundantIgnore(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("node_modules/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := RemediationFor(Finding{GateID: "vendored_dir_tracked", Project: dir, FilePath: "node_modules"}, ChannelMCP)
	if r.Recipe == nil || len(r.Recipe.FileEdits) != 0 {
		t.Errorf("expected no .gitignore edit (node_modules already covered), got %+v", r.Recipe)
	}
	if len(r.Recipe.Commands) == 0 || !strings.Contains(r.Recipe.Commands[0].Run, "git rm -r --cached") {
		t.Errorf("the untrack command must remain, got %+v", r.Recipe.Commands)
	}
	if !strings.Contains(r.Summary, "already covered") {
		t.Errorf("summary should note already-covered, got %q", r.Summary)
	}
}

func TestRemediationFor_GitignoreCoveragePullsPattern(t *testing.T) {
	r := RemediationFor(Finding{
		GateID:   "gitignore_coverage",
		FilePath: ".gitignore:node_modules",
	}, ChannelMCP)
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
	}, ChannelMCP)
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
	}, ChannelMCP)
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
	}, ChannelMCP)
	if r.Confidence != ConfidenceGuided || r.Recipe != nil {
		t.Errorf("merge conflict should be guided (no recipe), got %+v", r)
	}
}

func TestRemediationFor_SecretsHistoryFlagsRotation(t *testing.T) {
	r := RemediationFor(Finding{
		GateID: "secrets_scan_history",
	}, ChannelMCP)
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
	}, ChannelMCP)
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

// The verification step must name only the surface guaranteed present for
// the delivery channel: gates_check (MCP) vs `lgit check` (CLI). Promising
// the other one is exactly the mismatch this split fixes — an agent session
// often has one but not the other.
func TestClaudePrompt_VerificationIsChannelAware(t *testing.T) {
	find := Finding{
		ID:       99,
		GateID:   "vendored_dir_tracked",
		Project:  "/srv/proj",
		FilePath: "vendor",
		Title:    "x",
	}

	mcp := RemediationFor(find, ChannelMCP).ClaudePrompt
	if !strings.Contains(mcp, "gates_check") {
		t.Error("MCP prompt should verify via the gates_check MCP tool")
	}
	if !strings.Contains(mcp, "findings_remediate") {
		t.Error("MCP prompt should mention findings_remediate so the agent can re-fetch context")
	}
	if strings.Contains(mcp, "lgit check") {
		t.Error("MCP prompt must NOT tell the agent to run `lgit check` — lgit is not guaranteed on PATH in an MCP session")
	}
	if !strings.Contains(mcp, "/srv/proj") {
		t.Error("MCP prompt should include the project path")
	}

	cli := RemediationFor(find, ChannelCLI).ClaudePrompt
	if !strings.Contains(cli, "lgit check") {
		t.Error("CLI prompt should verify via `lgit check` — lgit is on PATH in a `lgit fix` session")
	}
	if strings.Contains(cli, "gates_check") {
		t.Error("CLI prompt must NOT reference gates_check — the MCP server may not be registered for a CLI user")
	}
	if !strings.Contains(cli, "/srv/proj") {
		t.Error("CLI prompt should include the project path")
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
	RenderRemediationText(&sb, f, RemediationFor(f, ChannelCLI))
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
	RenderRemediationText(&sb, f, RemediationFor(f, ChannelCLI))
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

// =============================================================================
// text rendering
// =============================================================================

// A gate with no recipe sets Summary to the finding's own title. The renderer
// used to print that under a "Fix" heading, so the same sentence appeared three
// times — header, Detected, Fix — and the one labelled "Fix" told the reader
// nothing they could act on.
func TestRenderRemediationText_FixDoesNotEchoTitle(t *testing.T) {
	f := Finding{
		ID:       42,
		Project:  "/p",
		GateID:   "dockerfile_lint",
		Severity: SeverityWarning,
		Title:    "Dockerfile has no USER directive",
		Message:  "Dockerfile:4 ENTRYPOINT/CMD with no preceding USER directive in this stage.",
		FilePath: "Dockerfile:4:missing_user",
	}
	var sb strings.Builder
	RenderRemediationText(&sb, f, RemediationFor(f, ChannelCLI))
	out := sb.String()

	fix := sectionBody(t, out, "Fix")
	if strings.Contains(fix, f.Title) {
		t.Errorf("Fix section repeats the title verbatim:\n%s", fix)
	}
	if !strings.Contains(fix, "judgement") {
		t.Errorf("Fix section should say the fix needs judgement, got:\n%s", fix)
	}
	// The substitute text already says there is no recipe; saying it twice was
	// the other half of the noise.
	if strings.Count(out, "No deterministic recipe") > 0 {
		t.Errorf("redundant no-recipe line kept alongside the substituted Fix:\n%s", out)
	}
}

// A gate that supplies a real summary must keep it — the de-duplication is
// about echoed titles, not about suppressing guidance.
func TestRenderRemediationText_KeepsRealSummary(t *testing.T) {
	f := Finding{
		ID:       43,
		Project:  "/p",
		GateID:   "merge_conflict_markers",
		Severity: SeverityError,
		Title:    "Merge conflict markers in tracked file",
		Message:  "src/main.go:2 contains an unresolved merge conflict marker.",
		FilePath: "src/main.go",
	}
	var sb strings.Builder
	RenderRemediationText(&sb, f, RemediationFor(f, ChannelCLI))
	out := sb.String()

	fix := sectionBody(t, out, "Fix")
	if !strings.Contains(fix, "Resolve the merge conflict in src/main.go") {
		t.Errorf("real summary was dropped, got:\n%s", fix)
	}
	if !strings.Contains(out, "No deterministic recipe") {
		t.Error("guided finding with its own summary should still say there is no recipe")
	}
}

// sectionBody returns the indented lines that follow a heading, up to the next
// blank line.
func sectionBody(t *testing.T, out, heading string) string {
	t.Helper()
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		if l != heading {
			continue
		}
		var body []string
		for _, b := range lines[i+1:] {
			if strings.TrimSpace(b) == "" {
				break
			}
			body = append(body, b)
		}
		return strings.Join(body, "\n")
	}
	t.Fatalf("heading %q not found in:\n%s", heading, out)
	return ""
}

// The MCP and --json channels read Summary directly, with no renderer to save
// them. A Summary equal to the Title told an agent nothing it did not already
// have in the same payload.
func TestRemediationFor_GuidedSummaryIsNotTheTitle(t *testing.T) {
	guided := []struct {
		gate  string
		title string
	}{
		{"dockerfile_lint", "Dockerfile has no USER directive"},
		{"compose_lint", "Compose service is privileged"},
		{"secrets_scan", "Tracked .env file"},
		{"html_lint", "Viewport meta blocks user zoom"},
		{"readme_present", "README missing"},
	}
	for _, c := range guided {
		t.Run(c.gate, func(t *testing.T) {
			r := RemediationFor(Finding{GateID: c.gate, Title: c.title}, ChannelMCP)
			if r.Confidence != ConfidenceGuided {
				t.Fatalf("expected a guided remediation, got %q", r.Confidence)
			}
			if r.Summary == c.title {
				t.Errorf("Summary echoes Title: %q", r.Summary)
			}
			if r.Summary == "" {
				t.Error("Summary is empty; it should say the fix needs judgement")
			}
		})
	}
}

// Deterministic gates must keep their real, specific summary — the change is
// about removing an echo, not about flattening every gate to one sentence.
func TestRemediationFor_DeterministicSummarySurvives(t *testing.T) {
	r := RemediationFor(Finding{
		GateID:   "ide_artifact_tracked",
		Title:    "Editor/IDE artefact tracked in git",
		FilePath: ".idea/workspace.xml",
	}, ChannelMCP)
	if r.Confidence != ConfidenceDeter {
		t.Fatalf("expected deterministic, got %q", r.Confidence)
	}
	if r.Summary == GuidedNoRecipeSummary || !strings.Contains(r.Summary, ".idea/workspace.xml") {
		t.Errorf("deterministic summary lost its specifics: %q", r.Summary)
	}
}
