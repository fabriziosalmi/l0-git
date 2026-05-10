package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// dead_placeholders walks every tracked text file (binary-skip + size-cap
// like secrets_scan) and surfaces unfinished-work markers: TODO:/FIXME:/
// XXX:/HACK: tags, "Update this later", and Lorem ipsum filler. Severity
// is info — the markers are deliberate signals, not bugs — but they're
// almost always things that should land before a release rather than
// stay in the doc/source forever.

type deadPlaceholdersOptions struct {
	scanOptions
	DisabledPatterns []string `json:"disabled_patterns,omitempty"`
}

type deadPlaceholderPattern struct {
	id    string
	title string
	re    *regexp.Regexp
}

// Patterns are deliberately strict (word-boundary, mandatory `:` for the
// XXX-style markers, case-insensitive) so we only match the conventional
// "I'll come back to this" form rather than every occurrence of the
// substring "todo".
var deadPlaceholderPatterns = []deadPlaceholderPattern{
	{id: "todo", title: "TODO marker", re: regexp.MustCompile(`(?i)\bTODO\s*:`)},
	{id: "fixme", title: "FIXME marker", re: regexp.MustCompile(`(?i)\bFIXME\s*:`)},
	{id: "xxx", title: "XXX marker", re: regexp.MustCompile(`(?i)\bXXX\s*:`)},
	{id: "hack", title: "HACK marker", re: regexp.MustCompile(`(?i)\bHACK\s*:`)},
	{id: "update_later", title: "\"Update this later\" placeholder", re: regexp.MustCompile(`(?i)update\s+(?:this\s+)?later`)},
	{id: "lorem_ipsum", title: "Lorem ipsum filler", re: regexp.MustCompile(`(?i)\bLorem\s+ipsum\b`)},
}

func checkDeadPlaceholders(ctx context.Context, root string, opts json.RawMessage) ([]Finding, error) {
	options := parseDeadPlaceholdersOptions(opts)

	if skip, stop := requireGitRepo(root, "dead_placeholders",
		"This gate uses git ls-files to scan tracked text files."); stop {
		return skip, nil
	}
	files, err := gitLsFiles(ctx, root)
	if err != nil {
		return []Finding{{
			Severity: SeverityWarning,
			Title:    "dead_placeholders failed",
			Message:  fmt.Sprintf("Could not enumerate tracked files: %v", err),
			FilePath: ".git",
		}}, nil
	}

	disabled := map[string]bool{}
	for _, id := range options.DisabledPatterns {
		disabled[id] = true
	}

	out := []Finding{}
	for _, rel := range files {
		if options.shouldSkip(rel) {
			continue
		}
		// Files whose name IS the tracking register for placeholders — scanning
		// them produces 100% noise (every line would match).
		if isPlaceholderRegistryFile(rel) {
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
		out = append(out, scanForDeadPlaceholders(rel, data, disabled)...)
	}
	return out, nil
}

func scanForDeadPlaceholders(rel string, data []byte, disabled map[string]bool) []Finding {
	out := []Finding{}
	line := 1
	start := 0
	emit := func(content []byte, lineNum int) {
		for _, p := range deadPlaceholderPatterns {
			if disabled[p.id] {
				continue
			}
			if loc := p.re.FindIndex(content); loc != nil {
				match := strings.TrimSpace(string(content[loc[0]:loc[1]]))
				out = append(out, Finding{
					Severity: SeverityInfo,
					Title:    p.title,
					Message: fmt.Sprintf(
						"%s:%d %s — unfinished-work placeholders are easy to miss before release; chase them down or replace with a tracked issue.",
						rel, lineNum, match,
					),
					FilePath: fmt.Sprintf("%s:%d:%s", rel, lineNum, p.id),
				})
			}
		}
	}
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' {
			emit(data[start:i], line)
			line++
			start = i + 1
		}
	}
	if start < len(data) {
		emit(data[start:], line)
	}
	return out
}

// placeholderRegistryBasenames are filenames that ARE the tracking register
// for placeholder items — scanning them is 100% noise.
var placeholderRegistryBasenames = map[string]bool{
	"todo.md":     true,
	"todos.md":    true,
	"fixme.md":    true,
	"fixmes.md":   true,
	"hack.md":     true,
	"hacks.md":    true,
	"notes.md":    true,
	"todo.txt":    true,
	"fixme.txt":   true,
	"todo":        true,
	"fixme":       true,
}

// isPlaceholderRegistryFile returns true when the file's basename (lowercased)
// is a well-known placeholder tracking file.
func isPlaceholderRegistryFile(rel string) bool {
	base := strings.ToLower(filepath.Base(rel))
	return placeholderRegistryBasenames[base]
}

func parseDeadPlaceholdersOptions(opts json.RawMessage) deadPlaceholdersOptions {
	if len(opts) == 0 {
		return deadPlaceholdersOptions{}
	}
	var o deadPlaceholdersOptions
	_ = json.Unmarshal(opts, &o)
	return o
}
