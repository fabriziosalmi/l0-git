package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// requireGitRepo emits the standard "skipped, not a git repo" finding for
// gates that need git ls-files. Returns (skipFindings, true) when the gate
// should bail out, or (nil, false) when it can proceed.
func requireGitRepo(root, gateID, why string) ([]Finding, bool) {
	if isGitRepo(root) {
		return nil, false
	}
	return []Finding{{
		Severity: SeverityInfo,
		Title:    gateID + " skipped (not a git repository)",
		Message:  fmt.Sprintf("Project root has no .git/. %s", why),
		FilePath: ".git",
	}}, true
}

// checkMergeConflictMarkers scans every tracked text file for unresolved
// merge conflict markers. A single file with markers is one finding (line
// of the first hit) — once you know one is there you'll go look at the file
// anyway. Severity error: this is never legitimate in main.
func checkMergeConflictMarkers(ctx context.Context, root string, opts json.RawMessage) ([]Finding, error) {
	if skip, stop := requireGitRepo(root, "merge_conflict_markers",
		"Initialize git or run gates from inside a clone — this gate uses git ls-files."); stop {
		return skip, nil
	}
	files, err := gitLsFiles(ctx, root)
	if err != nil {
		return []Finding{{
			Severity: SeverityWarning,
			Title:    "merge_conflict_markers failed",
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
		abs := filepath.Join(root, rel)
		info, err := os.Stat(abs)
		if err != nil || info.IsDir() {
			continue
		}
		if info.Size() > secretsMaxFileSize {
			continue
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		if isBinary(data) {
			continue
		}
		if line, ok := findFirstMergeMarker(data); ok {
			out = append(out, Finding{
				Severity: SeverityError,
				Title:    "Merge conflict markers in tracked file",
				Message:  fmt.Sprintf("%s:%d contains an unresolved merge conflict marker (<<<<<<<, =======, or >>>>>>>). Resolve the conflict before committing.", rel, line),
				FilePath: rel,
			})
		}
	}
	return out, nil
}

// findFirstMergeMarker returns (1-based line, true) on the first line that
// starts with seven < or > or | (the markers git produces). The "======="
// separator alone is too noisy (it appears in markdown rules and shell
// here-docs); we anchor on the directional markers instead, which are
// genuinely git-specific.
func findFirstMergeMarker(data []byte) (int, bool) {
	line := 1
	start := 0
	for i := 0; i <= len(data); i++ {
		if i == len(data) || data[i] == '\n' {
			content := data[start:i]
			if isMergeMarkerLine(content) {
				return line, true
			}
			line++
			start = i + 1
		}
	}
	return 0, false
}

func isMergeMarkerLine(line []byte) bool {
	// Strip trailing CR (CRLF line endings).
	if n := len(line); n > 0 && line[n-1] == '\r' {
		line = line[:n-1]
	}
	if len(line) < 7 {
		return false
	}
	switch line[0] {
	case '<', '>', '|':
		// All seven leading chars must match. Reject "<<< three" prose.
		head := line[0]
		for i := 0; i < 7; i++ {
			if line[i] != head {
				return false
			}
		}
		// 8th char (if any) must be space or end of line — git emits
		// "<<<<<<< HEAD\n", never "<<<<<<<X".
		if len(line) == 7 {
			return true
		}
		return line[7] == ' '
	}
	return false
}

// largeFileOptions is the JSON contract for gate_options.large_file_tracked.
type largeFileOptions struct {
	// ThresholdMB sets the size at which a tracked file becomes a finding.
	// Defaults to 5; clamped to >= 1.
	ThresholdMB int `json:"threshold_mb"`
	// ExcludePaths reuses the shared scan-options shape so users can
	// whitelist e.g. test fixtures without losing coverage elsewhere.
	scanOptions
}

// checkLargeFileTracked finds tracked files heavier than the configured
// threshold. Common cause: someone committed a build artefact, a video, or
// a database dump that should live elsewhere (Git LFS, releases, …). Files
// declared as LFS-managed in .gitattributes are skipped — they're already
// stored out-of-band by design.
func checkLargeFileTracked(ctx context.Context, root string, opts json.RawMessage) ([]Finding, error) {
	thresholdMB := 5
	parsed := largeFileOptions{}
	if len(opts) > 0 {
		if err := json.Unmarshal(opts, &parsed); err == nil && parsed.ThresholdMB >= 1 {
			thresholdMB = parsed.ThresholdMB
		}
	}
	thresholdBytes := int64(thresholdMB) * 1024 * 1024

	if skip, stop := requireGitRepo(root, "large_file_tracked",
		"Initialize git or run gates from inside a clone — this gate uses git ls-files."); stop {
		return skip, nil
	}
	files, err := gitLsFiles(ctx, root)
	if err != nil {
		return []Finding{{
			Severity: SeverityWarning,
			Title:    "large_file_tracked failed",
			Message:  fmt.Sprintf("Could not enumerate tracked files: %v", err),
			FilePath: ".git",
		}}, nil
	}
	lfsPatterns := loadLFSPatterns(root)

	out := []Finding{}
	for _, rel := range files {
		if parsed.shouldSkip(rel) {
			continue
		}
		if matchesLFSPatterns(rel, lfsPatterns) {
			continue
		}
		abs := filepath.Join(root, rel)
		info, err := os.Stat(abs)
		if err != nil || info.IsDir() {
			continue
		}
		if info.Size() <= thresholdBytes {
			continue
		}
		out = append(out, Finding{
			Severity: SeverityWarning,
			Title:    "Large file tracked in git",
			Message: fmt.Sprintf(
				"%s is %s tracked in git (threshold: %d MiB). Move it to Git LFS, releases, or external storage to keep clones lean.",
				rel, humanSize(info.Size()), thresholdMB,
			),
			FilePath: rel,
		})
	}
	return out, nil
}

// loadLFSPatterns reads .gitattributes from the project root and returns
// the path-patterns marked as LFS-managed (`filter=lfs`). Patterns
// missing or `.gitattributes` absent → empty slice; both are normal.
func loadLFSPatterns(root string) []string {
	data, err := os.ReadFile(filepath.Join(root, ".gitattributes"))
	if err != nil {
		return nil
	}
	patterns := []string{}
	for _, raw := range splitLines(string(data)) {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.Contains(line, "filter=lfs") {
			continue
		}
		// First whitespace-separated token is the pattern.
		fields := strings.Fields(line)
		if len(fields) > 0 {
			patterns = append(patterns, fields[0])
		}
	}
	return patterns
}

// matchesLFSPatterns honours the small subset of .gitattributes glob
// semantics we actually see in the wild: bare basename like "*.psd" and
// path globs like "assets/**". filepath.Match handles single-segment
// globs; for the doublestar case we fall back to substring matching,
// which is enough for the typical "assets/**/*.bin" pattern.
func matchesLFSPatterns(rel string, patterns []string) bool {
	base := filepath.Base(rel)
	for _, p := range patterns {
		if strings.Contains(p, "**") {
			// Convert "assets/**" or "**/*.bin" into a substring check
			// on the literal segments.
			for _, seg := range strings.Split(p, "**") {
				seg = strings.Trim(seg, "/")
				if seg != "" && !strings.Contains(rel, seg) {
					goto next
				}
			}
			return true
		next:
			continue
		}
		if ok, _ := filepath.Match(p, rel); ok {
			return true
		}
		if ok, _ := filepath.Match(p, base); ok {
			return true
		}
	}
	return false
}

// splitLines splits a string into LF-delimited lines, stripping trailing
// CR for CRLF inputs.
func splitLines(s string) []string {
	out := strings.Split(s, "\n")
	for i, l := range out {
		out[i] = strings.TrimSuffix(l, "\r")
	}
	return out
}

// humanSize is just enough to make the finding message readable; we don't
// pull in a humanize dep for one site.
func humanSize(n int64) string {
	const (
		_KB = 1024
		_MB = _KB * 1024
		_GB = _MB * 1024
	)
	switch {
	case n >= _GB:
		return fmt.Sprintf("%.1f GiB", float64(n)/float64(_GB))
	case n >= _MB:
		return fmt.Sprintf("%.1f MiB", float64(n)/float64(_MB))
	case n >= _KB:
		return fmt.Sprintf("%.1f KiB", float64(n)/float64(_KB))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

