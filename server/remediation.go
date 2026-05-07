package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Remediation describes how to resolve a single finding. Two layers:
//
//   - Recipe: an exact, copy-pasteable set of shell commands and/or file
//     edits. Populated only when we can produce a fix that's safe to apply
//     verbatim — no judgement calls, no project-specific guesses.
//   - ClaudePrompt: always populated. A self-contained prompt the user can
//     paste into Claude Code (or any LLM agent). Frames the finding,
//     constraints, and verification step. For deterministic gates it just
//     wraps the recipe; for guided gates it's the only actionable channel.
//
// The struct is computed on demand from a Finding (see RemediationFor) and
// never persisted — improving a recipe doesn't require a DB migration.
type Remediation struct {
	// Summary: a single sentence stating the action ("Stop tracking
	// node_modules and add it to .gitignore."). Always populated.
	Summary string `json:"summary"`
	// Confidence is "deterministic" when Recipe is populated and safe to
	// apply as-is; "guided" when the fix needs judgement (rotate a secret,
	// pick a Docker tag) and Recipe is nil.
	Confidence string `json:"confidence"`
	// Recipe is the exact fix. nil for guided remediations.
	Recipe *Recipe `json:"recipe,omitempty"`
	// ClaudePrompt is the framed ask for an LLM agent (Claude Code via the
	// findings_remediate MCP tool, or copy-pasted manually). Always set.
	ClaudePrompt string `json:"claude_prompt"`
}

// Recipe is the deterministic-fix payload: shell commands plus file edits,
// with caveats the user should read first. Both Commands and FileEdits may
// be empty (unusual but legal — some recipes are entirely "open this file
// and resolve manually").
type Recipe struct {
	Commands  []Command  `json:"commands,omitempty"`
	FileEdits []FileEdit `json:"file_edits,omitempty"`
	// Caveats: short bullets the user must read before running. Examples:
	// "rewrites git history", "force-push required afterwards", "destroys
	// uncommitted changes in <file>".
	Caveats []string `json:"caveats,omitempty"`
}

// Command is one shell command in a recipe. Run is the literal command
// (already shell-quoted where needed); Note is an optional one-liner
// explaining intent — useful when the same recipe has multiple steps.
type Command struct {
	Run  string `json:"run"`
	Note string `json:"note,omitempty"`
}

// FileEdit is a structured edit. Op is "append" (concatenate Content to
// the file, creating it if missing) or "insert_before_line" (insert
// Content as a new line above Line, 1-based). Other ops are intentionally
// not supported in the MVP — gates that need them emit Commands instead.
type FileEdit struct {
	Path    string `json:"path"`
	Op      string `json:"op"`
	Content string `json:"content"`
	Line    int    `json:"line,omitempty"`
}

const (
	OpAppend            = "append"
	OpInsertBeforeLine  = "insert_before_line"
	ConfidenceDeter     = "deterministic"
	ConfidenceGuided    = "guided"
)

// RemediationFor dispatches by gate_id and returns the remediation for the
// given finding. Always returns a non-zero Remediation — the ClaudePrompt
// is always usable even when no deterministic recipe exists.
func RemediationFor(f Finding) Remediation {
	switch f.GateID {
	case "vendored_dir_tracked":
		return remediateVendoredDir(f)
	case "ide_artifact_tracked":
		return remediateIdeArtifact(f)
	case "gitignore_coverage":
		return remediateGitignoreCoverage(f)
	case "unexpected_executable_bit":
		return remediateExecBit(f)
	case "env_example_uncommented":
		return remediateEnvExample(f)
	case "merge_conflict_markers":
		return remediateMergeConflict(f)
	case "large_blob_in_history":
		return remediateLargeBlobHistory(f)
	case "secrets_scan_history":
		return remediateSecretsHistory(f)
	}
	// No deterministic recipe — the LLM is the only channel.
	return Remediation{
		Summary:      f.Title,
		Confidence:   ConfidenceGuided,
		ClaudePrompt: buildClaudePrompt(f, "", nil),
	}
}

// =============================================================================
// per-gate recipes
// =============================================================================

func remediateVendoredDir(f Finding) Remediation {
	dir := strings.TrimSuffix(f.FilePath, "/")
	if dir == "" {
		// Defensive: gate normally always sets FilePath.
		return Remediation{
			Summary:      f.Title,
			Confidence:   ConfidenceGuided,
			ClaudePrompt: buildClaudePrompt(f, "", nil),
		}
	}
	recipe := &Recipe{
		Commands: []Command{
			{Run: fmt.Sprintf("git rm -r --cached %s", shellQuote(dir)), Note: "remove from the index, leave on disk"},
			{Run: fmt.Sprintf("git commit -m %s", shellQuote("stop tracking "+dir))},
		},
		FileEdits: []FileEdit{
			{Path: ".gitignore", Op: OpAppend, Content: dir + "/\n"},
		},
		Caveats: []string{"Other contributors will see the directory disappear from git on next pull — they keep their local copies."},
	}
	return Remediation{
		Summary:      fmt.Sprintf("Stop tracking %s in git and add it to .gitignore.", dir),
		Confidence:   ConfidenceDeter,
		Recipe:       recipe,
		ClaudePrompt: buildClaudePrompt(f, "Apply the recipe exactly. Do not touch unrelated files.", recipe),
	}
}

func remediateIdeArtifact(f Finding) Remediation {
	rel := f.FilePath
	if rel == "" {
		return Remediation{Summary: f.Title, Confidence: ConfidenceGuided, ClaudePrompt: buildClaudePrompt(f, "", nil)}
	}
	// Pick the .gitignore line: prefer a directory glob for known
	// editor dirs, exact path otherwise.
	ignoreLine := rel
	for _, prefix := range []string{".vscode/", ".idea/", ".vs/"} {
		if strings.HasPrefix(rel, prefix) || rel == strings.TrimSuffix(prefix, "/") {
			ignoreLine = prefix
			break
		}
	}
	recipe := &Recipe{
		Commands: []Command{
			{Run: fmt.Sprintf("git rm --cached %s", shellQuote(rel)), Note: "remove from the index, leave on disk"},
			{Run: fmt.Sprintf("git commit -m %s", shellQuote("untrack editor artefact "+rel))},
		},
		FileEdits: []FileEdit{
			{Path: ".gitignore", Op: OpAppend, Content: ignoreLine + "\n"},
		},
	}
	return Remediation{
		Summary:      fmt.Sprintf("Untrack %s and ignore it going forward.", rel),
		Confidence:   ConfidenceDeter,
		Recipe:       recipe,
		ClaudePrompt: buildClaudePrompt(f, "Apply the recipe exactly. Do not touch unrelated files.", recipe),
	}
}

func remediateGitignoreCoverage(f Finding) Remediation {
	// FilePath shape: ".gitignore:<pattern>" — see checkGitignoreCoverage.
	parts := strings.SplitN(f.FilePath, ":", 2)
	if len(parts) != 2 || parts[1] == "" {
		return Remediation{Summary: f.Title, Confidence: ConfidenceGuided, ClaudePrompt: buildClaudePrompt(f, "", nil)}
	}
	pattern := parts[1]
	recipe := &Recipe{
		FileEdits: []FileEdit{
			{Path: ".gitignore", Op: OpAppend, Content: pattern + "\n"},
		},
	}
	return Remediation{
		Summary:      fmt.Sprintf("Add `%s` to .gitignore.", pattern),
		Confidence:   ConfidenceDeter,
		Recipe:       recipe,
		ClaudePrompt: buildClaudePrompt(f, fmt.Sprintf("Append the line `%s` to .gitignore. If the entry already exists in some equivalent form, leave the file alone.", pattern), recipe),
	}
}

func remediateExecBit(f Finding) Remediation {
	rel := f.FilePath
	if rel == "" {
		return Remediation{Summary: f.Title, Confidence: ConfidenceGuided, ClaudePrompt: buildClaudePrompt(f, "", nil)}
	}
	recipe := &Recipe{
		Commands: []Command{
			{Run: fmt.Sprintf("git update-index --chmod=-x %s", shellQuote(rel)), Note: "drop the executable bit in the index (works portably across Linux/macOS/Windows)"},
			{Run: fmt.Sprintf("git commit -m %s", shellQuote("clear executable bit on "+rel))},
		},
	}
	return Remediation{
		Summary:      fmt.Sprintf("Clear the executable bit on %s in the git index.", rel),
		Confidence:   ConfidenceDeter,
		Recipe:       recipe,
		ClaudePrompt: buildClaudePrompt(f, "Apply the recipe exactly.", recipe),
	}
}

func remediateEnvExample(f Finding) Remediation {
	// FilePath shape: "<file>:<line>:<KEY>" — see evaluateEnvExample.
	parts := strings.SplitN(f.FilePath, ":", 3)
	if len(parts) != 3 {
		return Remediation{Summary: f.Title, Confidence: ConfidenceGuided, ClaudePrompt: buildClaudePrompt(f, "", nil)}
	}
	file := parts[0]
	line, err := strconv.Atoi(parts[1])
	if err != nil || line <= 0 {
		return Remediation{Summary: f.Title, Confidence: ConfidenceGuided, ClaudePrompt: buildClaudePrompt(f, "", nil)}
	}
	key := parts[2]
	// We don't know what the key means — only the user / Claude Code does.
	// The recipe is a placeholder comment; the prompt asks Claude to fill
	// in the real explanation.
	placeholder := fmt.Sprintf("# TODO: explain what %s is used for", key)
	recipe := &Recipe{
		FileEdits: []FileEdit{
			{Path: file, Op: OpInsertBeforeLine, Line: line, Content: placeholder + "\n"},
		},
		Caveats: []string{"The inserted comment is a placeholder — replace `TODO: explain what " + key + " is used for` with a one-line explanation of the variable's purpose."},
	}
	return Remediation{
		Summary:      fmt.Sprintf("Add an explanatory comment above %s in %s:%d.", key, file, line),
		Confidence:   ConfidenceDeter,
		Recipe:       recipe,
		ClaudePrompt: buildClaudePrompt(f,
			fmt.Sprintf("Insert a `# …` comment immediately above line %d in %s explaining what %s is. Look at the codebase to understand its purpose; don't leave a TODO placeholder.", line, file, key),
			recipe),
	}
}

func remediateMergeConflict(f Finding) Remediation {
	// No deterministic recipe — only the human/LLM can resolve the
	// semantic conflict. We still print the file:line so the user can
	// jump straight there.
	return Remediation{
		Summary:    fmt.Sprintf("Resolve the merge conflict in %s, then `git add` and `git commit`.", f.FilePath),
		Confidence: ConfidenceGuided,
		ClaudePrompt: buildClaudePrompt(f,
			"Open the file at the line indicated, decide which side of the conflict to keep (or merge them), remove the `<<<<<<<`/`=======`/`>>>>>>>` markers, then run `git add` and `git commit`. Ask the user before discarding either side.",
			nil),
	}
}

func remediateLargeBlobHistory(f Finding) Remediation {
	// We can show the canonical filter-repo recipe, but the threshold
	// comes from the project config — re-read it so the recipe matches
	// what the gate ran with.
	thresholdMB := largeBlobThresholdMB(f.Project)
	cmd := fmt.Sprintf("git filter-repo --strip-blobs-bigger-than %dM", thresholdMB)
	recipe := &Recipe{
		Commands: []Command{
			{Run: cmd, Note: "rewrites every reachable commit; coordinate with collaborators first"},
			{Run: "git push --force-with-lease --all", Note: "publish the rewritten history (after `git filter-repo` has cleaned the local clone)"},
		},
		Caveats: []string{
			"Rewrites git history. Every collaborator must re-clone or `git fetch && git reset --hard origin/<branch>`.",
			"Requires the `git-filter-repo` tool: `brew install git-filter-repo` or `pip install git-filter-repo`.",
			"Run on a fresh clone of the repo — `git filter-repo` refuses to operate on a non-fresh clone by default.",
		},
	}
	return Remediation{
		Summary:    fmt.Sprintf("Purge blobs > %d MiB from .git/objects with `git filter-repo`.", thresholdMB),
		Confidence: ConfidenceDeter,
		Recipe:     recipe,
		ClaudePrompt: buildClaudePrompt(f,
			"This rewrites git history — confirm with the user before running. Use `git filter-repo --strip-blobs-bigger-than "+strconv.Itoa(thresholdMB)+"M` on a fresh clone, then force-push with `--force-with-lease`. Make sure every collaborator is told to re-clone afterwards.",
			recipe),
	}
}

func remediateSecretsHistory(f Finding) Remediation {
	// Deterministic enough to be useful: the recipe is `git filter-repo
	// --replace-text patterns.txt` where patterns.txt holds the literal
	// secret. We can't auto-rotate, so the prompt leads with that.
	recipe := &Recipe{
		Commands: []Command{
			{Run: "echo '<the-leaked-secret>==>REDACTED' > /tmp/lgit-replace.txt", Note: "write a replace-text file with the literal value(s) to scrub"},
			{Run: "git filter-repo --replace-text /tmp/lgit-replace.txt", Note: "rewrites every reachable commit"},
			{Run: "git push --force-with-lease --all", Note: "publish the rewritten history"},
			{Run: "rm /tmp/lgit-replace.txt", Note: "the file contained the literal secret — don't leave it on disk"},
		},
		Caveats: []string{
			"ROTATE THE CREDENTIAL FIRST. Purging history doesn't help if the leaked value is still valid — assume any secret committed to git is compromised.",
			"Rewrites git history. Every collaborator must re-clone or hard-reset.",
			"Requires `git-filter-repo`: `brew install git-filter-repo` or `pip install git-filter-repo`.",
		},
	}
	return Remediation{
		Summary:    "Rotate the credential, then purge the literal value from git history.",
		Confidence: ConfidenceDeter,
		Recipe:     recipe,
		ClaudePrompt: buildClaudePrompt(f,
			"This is a leaked credential. Step 1 (cannot be skipped): the user must rotate the credential at the issuing service — do not proceed until they confirm. Step 2: replace `<the-leaked-secret>` in the recipe with the literal value. Step 3: run `git filter-repo --replace-text` and force-push. Coordinate the force-push with collaborators.",
			recipe),
	}
}

// =============================================================================
// helpers
// =============================================================================

// largeBlobThresholdMB reads the project's .l0git.json and returns the
// threshold the gate would have used. Falls back to the gate's default
// (5 MiB) when the file is missing or doesn't override the threshold.
func largeBlobThresholdMB(project string) int {
	cfg, err := loadProjectConfig(project)
	if err != nil || cfg == nil {
		return 5
	}
	opts := cfg.optionsFor("large_blob_in_history")
	if len(opts) == 0 {
		return 5
	}
	var parsed largeBlobHistoryOptions
	if err := json.Unmarshal(opts, &parsed); err != nil || parsed.ThresholdMB <= 0 {
		return 5
	}
	return parsed.ThresholdMB
}

// shellQuote returns s wrapped in single quotes, with any embedded single
// quotes escaped. Safe for paste into bash/zsh — handles paths with
// spaces, parentheses, etc.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n\"'\\$`!*?[]{}()<>|;&#~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// buildClaudePrompt returns a self-contained prompt the user can paste
// into Claude Code (or any LLM agent). Always references the lgit MCP
// tools so the agent can re-fetch context if needed.
func buildClaudePrompt(f Finding, extra string, recipe *Recipe) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Fix l0-git finding #%d (%s, %s): %s.\n\n",
		f.ID, f.GateID, f.Severity, f.Title)
	fmt.Fprintf(&b, "Project: %s\n", f.Project)
	if f.FilePath != "" {
		fmt.Fprintf(&b, "Location: %s\n", f.FilePath)
	}
	fmt.Fprintf(&b, "Detected: %s\n\n", f.Message)
	b.WriteString("Constraints:\n")
	b.WriteString("- Do not touch files unrelated to this finding.\n")
	b.WriteString("- After applying the fix, run `lgit check " + shellQuote(f.Project) + " " + f.GateID + "` to confirm the finding is resolved.\n")
	b.WriteString("- For changes that touch git history (filter-repo, force-push), confirm with the user before running.\n")
	if extra != "" {
		b.WriteString("- " + extra + "\n")
	}
	if recipe != nil {
		b.WriteString("\nRecommended recipe (deterministic, safe to apply as-is):\n")
		for _, c := range recipe.Commands {
			fmt.Fprintf(&b, "  $ %s", c.Run)
			if c.Note != "" {
				fmt.Fprintf(&b, "    # %s", c.Note)
			}
			b.WriteString("\n")
		}
		for _, e := range recipe.FileEdits {
			fmt.Fprintf(&b, "  edit %s (%s)", e.Path, e.Op)
			if e.Line > 0 {
				fmt.Fprintf(&b, " line %d", e.Line)
			}
			fmt.Fprintf(&b, ":\n%s", indent(e.Content, "    "))
			if !strings.HasSuffix(e.Content, "\n") {
				b.WriteString("\n")
			}
		}
	}
	b.WriteString("\nUse the l0-git MCP tools (`findings_remediate`, `gates_check`) for full context if needed.\n")
	return b.String()
}

func indent(s, prefix string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, ln := range lines {
		lines[i] = prefix + ln
	}
	return strings.Join(lines, "\n")
}

// =============================================================================
// human-readable rendering for `lgit fix <id>`
// =============================================================================

// RenderRemediationText writes a human-readable view of (finding, remediation)
// to w. Format is intentionally plain — no ANSI colour, no boxes — so it
// pipes well to `less`, `pbcopy`, or a markdown viewer.
func RenderRemediationText(w *strings.Builder, f Finding, r Remediation) {
	fmt.Fprintf(w, "l0-git finding #%d — %s (%s)\n", f.ID, f.GateID, f.Severity)
	fmt.Fprintf(w, "%s\n", f.Title)
	if f.FilePath != "" {
		fmt.Fprintf(w, "Location: %s\n", f.FilePath)
	}
	fmt.Fprintf(w, "Project:  %s\n\n", f.Project)

	w.WriteString("Detected\n")
	fmt.Fprintf(w, "  %s\n\n", wrap(f.Message, 76, "  "))

	w.WriteString("Fix\n")
	fmt.Fprintf(w, "  %s\n\n", wrap(r.Summary, 76, "  "))

	if r.Recipe != nil {
		if len(r.Recipe.Commands) > 0 {
			w.WriteString("Run\n")
			for _, c := range r.Recipe.Commands {
				fmt.Fprintf(w, "  $ %s\n", c.Run)
				if c.Note != "" {
					fmt.Fprintf(w, "      %s\n", wrap(c.Note, 72, "      "))
				}
			}
			w.WriteString("\n")
		}
		if len(r.Recipe.FileEdits) > 0 {
			w.WriteString("Edit\n")
			for _, e := range r.Recipe.FileEdits {
				switch e.Op {
				case OpAppend:
					fmt.Fprintf(w, "  append to %s:\n%s\n", e.Path, indent(strings.TrimRight(e.Content, "\n"), "    "))
				case OpInsertBeforeLine:
					fmt.Fprintf(w, "  insert before %s:%d:\n%s\n", e.Path, e.Line, indent(strings.TrimRight(e.Content, "\n"), "    "))
				default:
					fmt.Fprintf(w, "  %s %s:\n%s\n", e.Op, e.Path, indent(strings.TrimRight(e.Content, "\n"), "    "))
				}
			}
			w.WriteString("\n")
		}
		if len(r.Recipe.Caveats) > 0 {
			w.WriteString("Caveats\n")
			for _, c := range r.Recipe.Caveats {
				fmt.Fprintf(w, "  - %s\n", wrap(c, 74, "    "))
			}
			w.WriteString("\n")
		}
	} else {
		w.WriteString("No deterministic recipe — this gate needs human or LLM judgement.\n\n")
	}

	w.WriteString("Confidence\n")
	fmt.Fprintf(w, "  %s\n\n", r.Confidence)

	if r.Recipe != nil && (len(r.Recipe.Commands) > 0 || len(r.Recipe.FileEdits) > 0) {
		w.WriteString("Verify\n")
		fmt.Fprintf(w, "  $ lgit check %s %s\n\n", shellQuote(f.Project), f.GateID)
	}

	w.WriteString("Hand off to Claude Code\n")
	w.WriteString("  Either paste the prompt below into Claude Code, or have it call the\n")
	w.WriteString("  l0-git MCP tool `findings_remediate` with id=" + strconv.FormatInt(f.ID, 10) + ".\n\n")
	w.WriteString("--- prompt ---\n")
	w.WriteString(r.ClaudePrompt)
	if !strings.HasSuffix(r.ClaudePrompt, "\n") {
		w.WriteString("\n")
	}
	w.WriteString("--- end ---\n")
}

// wrap is a minimal soft-wrap helper for paragraph-style remediation
// fields. Splits on whitespace; keeps already-short lines intact; honours
// the indent on continuation lines.
func wrap(s string, width int, contIndent string) string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return s
	}
	var out strings.Builder
	col := 0
	for i, w := range words {
		if i == 0 {
			out.WriteString(w)
			col = len(w)
			continue
		}
		if col+1+len(w) > width {
			out.WriteString("\n")
			out.WriteString(contIndent)
			out.WriteString(w)
			col = len(contIndent) + len(w)
			continue
		}
		out.WriteString(" ")
		out.WriteString(w)
		col += 1 + len(w)
	}
	return out.String()
}

