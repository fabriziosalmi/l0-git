package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// =============================================================================
// unexpected_executable_bit
// =============================================================================
//
// Git only preserves a coarse subset of unix file modes (100644 vs 100755),
// so "chmod 777 in source" maps cleanly to "100755 on a file whose extension
// is never legitimately executable". We deliberately whitelist a small set
// of "definitely not executable" extensions; this avoids false positives on
// build scripts, language runtimes, etc.
//
// Scope: text/data formats whose .sh/.py/etc. counterparts would be in a
// different file. If you want a hardcoded list, this is it.

var nonExecutableExts = map[string]bool{
	".md":     true,
	".rst":    true,
	".txt":    true,
	".json":   true,
	".jsonc":  true,
	".yml":    true,
	".yaml":   true,
	".toml":   true,
	".ini":    true,
	".cfg":    true,
	".conf":   true,
	".env":    true,
	".lock":   true,
	".sum":    true,
	".mod":    true,
	".css":    true,
	".scss":   true,
	".sass":   true,
	".less":   true,
	".html":   true,
	".htm":    true,
	".xml":    true,
	".svg":    true,
	".gradle": true,
	".sql":    true,
	".csv":    true,
	".tsv":    true,
}

func checkUnexpectedExecutableBit(ctx context.Context, root string, opts json.RawMessage) ([]Finding, error) {
	if skip, stop := requireGitRepo(root, "unexpected_executable_bit",
		"This gate uses git ls-files -s to read tracked file modes."); stop {
		return skip, nil
	}
	entries, err := gitLsFilesWithMode(ctx, root)
	if err != nil {
		return []Finding{{
			Severity: SeverityWarning,
			Title:    "unexpected_executable_bit failed",
			Message:  fmt.Sprintf("Could not enumerate tracked files with mode: %v", err),
			FilePath: ".git",
		}}, nil
	}
	scan := parseScanOptions(opts)

	out := []Finding{}
	for _, e := range entries {
		if scan.shouldSkip(e.Path) {
			continue
		}
		if e.Mode != "100755" {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Path))
		base := strings.ToLower(filepath.Base(e.Path))
		if !nonExecutableExts[ext] && !looksLikeLockfile(base) {
			continue
		}
		out = append(out, Finding{
			Severity: SeverityWarning,
			Title:    "Unexpected executable bit on a non-script file",
			Message: fmt.Sprintf(
				"%s is tracked with mode 100755 (executable), but its extension/name suggests a text/data file. Run `git update-index --chmod=-x %s` and commit the fix.",
				e.Path, e.Path,
			),
			FilePath: e.Path,
		})
	}
	return out, nil
}

// looksLikeLockfile catches package-lock.json, Cargo.lock, poetry.lock,
// yarn.lock, etc. — extensions don't always carry the signal (no extension
// at all on Cargo.lock; .lock is reused by other tools).
func looksLikeLockfile(base string) bool {
	switch base {
	case "package-lock.json", "yarn.lock", "pnpm-lock.yaml",
		"cargo.lock", "poetry.lock", "gemfile.lock", "composer.lock":
		return true
	}
	return false
}

// =============================================================================
// vendored_dir_tracked
// =============================================================================
//
// Vendored dependency directories rebuild from the manifest. Committing them
// is bloat (and a merge-conflict factory). We list the well-known names
// rather than infer from `.gitignore` so the gate is fully deterministic.

var vendoredDirPrefixes = []string{
	"node_modules/",
	"vendor/", // Go modules vendored, PHP vendor/, etc.
	"target/", // Cargo, Maven, Gradle
	"dist/",
	"build/",
	".next/",
	".nuxt/",
	".cache/",
	"__pycache__/",
	".pytest_cache/",
	".mypy_cache/",
	".tox/",
	"bower_components/",
}

func checkVendoredDirTracked(ctx context.Context, root string, opts json.RawMessage) ([]Finding, error) {
	if skip, stop := requireGitRepo(root, "vendored_dir_tracked",
		"This gate uses git ls-files to find committed vendored directories."); stop {
		return skip, nil
	}
	files, err := gitLsFiles(ctx, root)
	if err != nil {
		return []Finding{{
			Severity: SeverityWarning,
			Title:    "vendored_dir_tracked failed",
			Message:  fmt.Sprintf("Could not enumerate tracked files: %v", err),
			FilePath: ".git",
		}}, nil
	}
	scan := parseScanOptions(opts)

	// One finding per offending top-level directory, not per file —
	// otherwise a stray node_modules with 50k files would bury the
	// Problems pane.
	seen := map[string]bool{}
	out := []Finding{}
	for _, rel := range files {
		if scan.shouldSkip(rel) {
			continue
		}
		// Match either at root or any depth — vendoring at any depth is bad.
		for _, prefix := range vendoredDirPrefixes {
			if dirMatchesAtAnyDepth(rel, prefix) {
				key := vendoredKey(rel, prefix)
				if seen[key] {
					break
				}
				seen[key] = true
				out = append(out, Finding{
					Severity: SeverityWarning,
					Title:    "Vendored directory tracked in git",
					Message: fmt.Sprintf(
						"%s is tracked. %s is meant to rebuild from a manifest — committing it bloats the repo and produces merge conflicts. Add %s to .gitignore and remove with `git rm -r --cached %s`.",
						key, strings.TrimSuffix(prefix, "/"), strings.TrimSuffix(prefix, "/"), key,
					),
					FilePath: key,
				})
				break
			}
		}
	}
	return out, nil
}

// dirMatchesAtAnyDepth returns true when rel contains "/<prefix>" or starts
// with prefix. prefix is expected to end with "/".
func dirMatchesAtAnyDepth(rel, prefix string) bool {
	if strings.HasPrefix(rel, prefix) {
		return true
	}
	return strings.Contains(rel, "/"+prefix)
}

// vendoredKey returns the highest-level path segment that matches prefix.
// Used to deduplicate findings: a single "node_modules/" finding regardless
// of how many files live inside.
func vendoredKey(rel, prefix string) string {
	if strings.HasPrefix(rel, prefix) {
		return strings.TrimSuffix(prefix, "/")
	}
	idx := strings.Index(rel, "/"+prefix)
	if idx < 0 {
		return strings.TrimSuffix(prefix, "/")
	}
	return rel[:idx+1+len(prefix)-1]
}

// =============================================================================
// ide_artifact_tracked
// =============================================================================
//
// Editor and OS-generated artefacts never belong in shared history.

var ideArtifactBasenames = map[string]bool{
	".DS_Store": true,
	"Thumbs.db": true,
	"desktop.ini": true,
}

var ideArtifactDirPrefixes = []string{
	".vscode/",
	".idea/",
	".vs/",
	".sublime-project/",
	".sublime-workspace/",
}

// Suffixes that indicate editor scratch/swap files anywhere in the tree.
var ideArtifactSuffixes = []string{
	".swp", ".swo",
	"~",
}

func checkIdeArtifactTracked(ctx context.Context, root string, opts json.RawMessage) ([]Finding, error) {
	if skip, stop := requireGitRepo(root, "ide_artifact_tracked",
		"This gate uses git ls-files to find tracked editor/IDE artefacts."); stop {
		return skip, nil
	}
	files, err := gitLsFiles(ctx, root)
	if err != nil {
		return []Finding{{
			Severity: SeverityWarning,
			Title:    "ide_artifact_tracked failed",
			Message:  fmt.Sprintf("Could not enumerate tracked files: %v", err),
			FilePath: ".git",
		}}, nil
	}
	scan := parseScanOptions(opts)

	out := []Finding{}
	for _, rel := range files {
		if scan.shouldSkip(rel) {
			continue
		}
		base := filepath.Base(rel)
		if ideArtifactBasenames[base] || matchesAnySuffix(base, ideArtifactSuffixes) {
			out = append(out, ideArtifactFinding(rel))
			continue
		}
		for _, prefix := range ideArtifactDirPrefixes {
			if dirMatchesAtAnyDepth(rel, prefix) {
				out = append(out, ideArtifactFinding(rel))
				break
			}
		}
	}
	return out, nil
}

func ideArtifactFinding(rel string) Finding {
	return Finding{
		Severity: SeverityWarning,
		Title:    "Editor/IDE artefact tracked in git",
		Message: fmt.Sprintf(
			"%s is a user-local editor/IDE/OS artefact and shouldn't live in shared history. Add it to .gitignore and remove with `git rm --cached %s`.",
			rel, rel,
		),
		FilePath: rel,
	}
}

func matchesAnySuffix(s string, suffixes []string) bool {
	for _, sfx := range suffixes {
		if strings.HasSuffix(s, sfx) && len(s) > len(sfx) {
			// Require non-empty stem so we don't flag a literal "~"
			// (rare but possible) as if it were an editor backup.
			return true
		}
	}
	return false
}

// =============================================================================
// filename_quality
// =============================================================================
//
// Filenames with whitespace or non-ASCII characters are technically valid
// but break naive shell pipelines, archive tools, and CI scripts that
// don't quote properly. Severity info — sometimes it's intentional (docs,
// localised assets) — but always worth surfacing.

func checkFilenameQuality(ctx context.Context, root string, opts json.RawMessage) ([]Finding, error) {
	if skip, stop := requireGitRepo(root, "filename_quality",
		"This gate uses git ls-files to scan tracked file paths."); stop {
		return skip, nil
	}
	files, err := gitLsFiles(ctx, root)
	if err != nil {
		return []Finding{{
			Severity: SeverityWarning,
			Title:    "filename_quality failed",
			Message:  fmt.Sprintf("Could not enumerate tracked files: %v", err),
			FilePath: ".git",
		}}, nil
	}
	scan := parseScanOptions(opts)

	out := []Finding{}
	for _, rel := range files {
		if scan.shouldSkip(rel) {
			continue
		}
		base := filepath.Base(rel)
		issues := classifyFilename(base)
		if len(issues) == 0 {
			continue
		}
		out = append(out, Finding{
			Severity: SeverityInfo,
			Title:    "File name quality (" + strings.Join(issues, ", ") + ")",
			Message: fmt.Sprintf(
				"%s has %s. Tools and shell pipelines that don't quote argv or use IFS=$'\\n' break on these. Rename if you can.",
				rel, strings.Join(issues, " and "),
			),
			FilePath: rel,
		})
	}
	return out, nil
}

func classifyFilename(base string) []string {
	out := []string{}
	hasSpace := false
	hasControl := false
	hasNonASCII := false
	for _, r := range base {
		switch {
		case r == ' ':
			hasSpace = true
		case r < 0x20 || r == 0x7f:
			hasControl = true
		case r > 0x7f:
			hasNonASCII = true
		}
	}
	if hasSpace {
		out = append(out, "spaces")
	}
	if hasControl {
		out = append(out, "control chars")
	}
	if hasNonASCII {
		out = append(out, "non-ASCII chars")
	}
	return out
}

// =============================================================================
// nvmrc_missing
// =============================================================================
//
// If the project has a package.json, declaring the Node version up front
// prevents the "works on my machine" class of failures. .nvmrc and
// .node-version are interchangeable for our purposes.

func checkNvmrcMissing(_ context.Context, root string, _ json.RawMessage) ([]Finding, error) {
	pkgPath := filepath.Join(root, "package.json")
	if _, err := os.Stat(pkgPath); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	for _, name := range []string{".nvmrc", ".node-version"} {
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			return nil, nil
		}
	}
	return []Finding{{
		Severity: SeverityInfo,
		Title:    "package.json without .nvmrc / .node-version",
		Message:  "package.json exists but no .nvmrc / .node-version pins the runtime. nvm/asdf/Volta users (and CI runners) will silently pick whatever Node is on PATH. Add a one-line .nvmrc with the target version.",
		FilePath: ".nvmrc",
	}}, nil
}
