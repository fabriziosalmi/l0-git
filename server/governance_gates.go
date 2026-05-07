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
// code_of_conduct_present
// =============================================================================
//
// GitHub recognises CODE_OF_CONDUCT.md at the project root, in .github/, or
// in docs/. Any of those satisfies the gate.

func checkCodeOfConductPresent(_ context.Context, root string, _ json.RawMessage) ([]Finding, error) {
	for _, candidate := range codeOfConductLocations() {
		full := filepath.Join(root, candidate)
		if exists(full) {
			return nil, nil
		}
	}
	return []Finding{{
		Severity: SeverityInfo,
		Title:    "CODE_OF_CONDUCT missing",
		Message:  "No CODE_OF_CONDUCT.md found at the project root, .github/, or docs/. Adopt the Contributor Covenant and add CODE_OF_CONDUCT.md so contributors know the rules of engagement.",
		FilePath: "CODE_OF_CONDUCT.md",
	}}, nil
}

func codeOfConductLocations() []string {
	out := []string{}
	for _, dir := range []string{"", ".github", "docs"} {
		for _, name := range []string{"CODE_OF_CONDUCT.md", "CODE_OF_CONDUCT", "CODE_OF_CONDUCT.markdown"} {
			if dir == "" {
				out = append(out, name)
			} else {
				out = append(out, filepath.Join(dir, name))
			}
		}
	}
	return out
}

// =============================================================================
// codeowners_present
// =============================================================================
//
// CODEOWNERS may live at the project root, in .github/, or in docs/. The
// gate is silent unless the project has a recognisable code surface
// (Go/TS/JS/Python source files), to avoid noise on docs-only repos.

func checkCodeownersPresent(ctx context.Context, root string, _ json.RawMessage) ([]Finding, error) {
	for _, p := range []string{"CODEOWNERS", filepath.Join(".github", "CODEOWNERS"), filepath.Join("docs", "CODEOWNERS")} {
		if exists(filepath.Join(root, p)) {
			return nil, nil
		}
	}
	if !looksLikeCodebase(ctx, root) {
		// Pure docs / config repos legitimately don't need a CODEOWNERS;
		// staying silent avoids noise.
		return nil, nil
	}
	return []Finding{{
		Severity: SeverityInfo,
		Title:    "CODEOWNERS missing",
		Message:  "No CODEOWNERS file found at the project root, .github/, or docs/. Declare reviewers per path so PRs route to the right people automatically.",
		FilePath: ".github/CODEOWNERS",
	}}, nil
}

// looksLikeCodebase returns true when the project has at least one tracked
// source file in a recognised language. Cheap heuristic — we only need to
// distinguish "real source tree" from "literally just markdown / config".
func looksLikeCodebase(ctx context.Context, root string) bool {
	if !isGitRepo(root) {
		return false
	}
	files, err := gitLsFiles(ctx, root)
	if err != nil {
		return false
	}
	exts := map[string]bool{
		".go": true, ".rs": true, ".py": true, ".rb": true,
		".ts": true, ".tsx": true, ".js": true, ".jsx": true,
		".java": true, ".kt": true, ".swift": true, ".cs": true,
		".cpp": true, ".cc": true, ".c": true, ".h": true,
		".php": true, ".scala": true, ".ex": true, ".exs": true,
	}
	for _, rel := range files {
		if exts[strings.ToLower(filepath.Ext(rel))] {
			return true
		}
	}
	return false
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// =============================================================================
// env_example_uncommented
// =============================================================================
//
// .env.example (and friends) are a contract: they tell new developers
// which secrets to fill in. A bare list of keys with no comments is
// useless — for each key the contract should say what it is, what
// defaults are sensible, where to obtain it.
//
// Deterministic rule: every KEY= line must either (a) have a `#` comment
// somewhere on the same line, OR (b) be immediately preceded by a comment
// line. Otherwise it's an "uncommented key" finding.

var envExampleNames = []string{
	".env.example",
	".env.sample",
	".env.template",
	".env.dist",
}

func checkEnvExampleUncommented(_ context.Context, root string, _ json.RawMessage) ([]Finding, error) {
	out := []Finding{}
	for _, name := range envExampleNames {
		full := filepath.Join(root, name)
		data, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		out = append(out, evaluateEnvExample(name, string(data))...)
	}
	return out, nil
}

// evaluateEnvExample walks a single .env.example file and emits one info
// finding per key= line that lacks a comment. Pulled out so it's directly
// testable.
func evaluateEnvExample(rel, content string) []Finding {
	out := []Finding{}
	content = strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(content, "\n")
	for i, raw := range lines {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// Recognise key=value lines. Keys are upper/lower alphanum + _.
		eq := strings.IndexByte(trimmed, '=')
		if eq <= 0 {
			continue
		}
		keyPart := trimmed[:eq]
		if !looksLikeEnvKey(keyPart) {
			continue
		}
		if hasInlineComment(raw) {
			continue
		}
		// Look at the previous non-empty line: if it's a comment, accept.
		if i > 0 {
			for j := i - 1; j >= 0; j-- {
				prev := strings.TrimSpace(lines[j])
				if prev == "" {
					continue
				}
				if strings.HasPrefix(prev, "#") {
					goto nextLine
				}
				break
			}
		}
		out = append(out, Finding{
			Severity: SeverityInfo,
			Title:    "Uncommented .env.example key",
			Message: fmt.Sprintf(
				"%s:%d %s has no preceding or inline `# …` comment. Without context, contributors don't know what to fill in.",
				rel, i+1, keyPart,
			),
			FilePath: fmt.Sprintf("%s:%d:%s", rel, i+1, keyPart),
		})
	nextLine:
	}
	return out
}

func looksLikeEnvKey(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

// hasInlineComment is a conservative `#` detector: it returns true only
// when there's a `#` outside of any single- or double-quoted region.
// Avoids mis-classifying `KEY="https://example.com#anchor"` as
// commented.
func hasInlineComment(line string) bool {
	inSingle := false
	inDouble := false
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble {
				return true
			}
		}
	}
	return false
}
