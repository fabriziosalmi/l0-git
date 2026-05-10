package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	"gopkg.in/yaml.v3"
)

// markdown_lint covers the deterministic subset of doc-quality checks:
// every rule produces a finding only when the AST shape is unambiguous
// (image with empty alt, fenced block without language, broken local
// link, broken in-doc anchor, JSON/YAML payload that doesn't parse).

type markdownLintOptions struct {
	scanOptions
	DisabledRules []string `json:"disabled_rules,omitempty"`
}

func checkMarkdownLint(ctx context.Context, root string, opts json.RawMessage) ([]Finding, error) {
	options := parseMarkdownOptions(opts)

	if skip, stop := requireGitRepo(root, "markdown_lint",
		"This gate uses git ls-files to scan tracked Markdown files."); stop {
		return skip, nil
	}
	files, err := gitLsFiles(ctx, root)
	if err != nil {
		return []Finding{{
			Severity: SeverityWarning,
			Title:    "markdown_lint failed",
			Message:  fmt.Sprintf("Could not enumerate tracked files: %v", err),
			FilePath: ".git",
		}}, nil
	}

	disabled := map[string]bool{}
	for _, id := range options.DisabledRules {
		disabled[id] = true
	}

	out := []Finding{}
	for _, rel := range files {
		if options.shouldSkip(rel) {
			continue
		}
		if !isMarkdownBasename(filepath.Base(rel)) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		out = append(out, evaluateMarkdownFile(rel, root, data, disabled)...)
	}
	return out, nil
}

func isMarkdownBasename(name string) bool {
	low := strings.ToLower(name)
	return strings.HasSuffix(low, ".md") || strings.HasSuffix(low, ".markdown")
}

// evaluateMarkdownFile parses the document once, collects all heading
// slugs (for anchor validation), then walks the tree and emits findings.
func evaluateMarkdownFile(rel, root string, source []byte, disabled map[string]bool) []Finding {
	overrides := collectHtmlOverrides(source) // <!-- --> works in Markdown
	lineStarts := computeLineStarts(source)

	md := goldmark.New()
	doc := md.Parser().Parse(text.NewReader(source))

	slugs := collectHeadingSlugs(doc, source)

	out := []Finding{}
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch typed := n.(type) {
		case *ast.Image:
			if !disabled["image_no_alt"] {
				if alt := strings.TrimSpace(extractText(typed, source)); alt == "" {
					line := nodeLine(typed, lineStarts)
					out = append(out, mdFindingAt(rel, line,
						mdRules["image_no_alt"],
						fmt.Sprintf("![](%s) has empty alt text", string(typed.Destination)),
						overrides))
				}
			}
		case *ast.Link:
			line := nodeLine(typed, lineStarts)
			dest := string(typed.Destination)
			if isAnchorOnly(dest) {
				if !disabled["link_anchor_broken"] {
					anchor := strings.TrimPrefix(dest, "#")
					if !slugs[strings.ToLower(anchor)] {
						out = append(out, mdFindingAt(rel, line,
							mdRules["link_anchor_broken"],
							fmt.Sprintf("anchor `#%s` does not match any heading in this document", anchor),
							overrides))
					}
				}
			} else if isLocalLink(dest) {
				if !disabled["link_local_broken"] {
					path, _ := splitPathAndAnchor(dest)
					if !localTargetExists(rel, root, path) {
						out = append(out, mdFindingAt(rel, line,
							mdRules["link_local_broken"],
							fmt.Sprintf("link target `%s` does not exist on disk", path),
							overrides))
					}
				}
			}
		case *ast.FencedCodeBlock:
			line := nodeLine(typed, lineStarts)
			lang := strings.TrimSpace(string(typed.Language(source)))
			if lang == "" {
				if !disabled["codeblock_no_language"] {
					out = append(out, mdFindingAt(rel, line,
						mdRules["codeblock_no_language"],
						"fenced code block has no language tag",
						overrides))
				}
			} else if !disabled["codeblock_invalid_payload"] {
				body := fencedBlockBody(typed, source)
				if msg := validatePayload(lang, body); msg != "" {
					out = append(out, mdFindingAt(rel, line,
						mdRules["codeblock_invalid_payload"],
						fmt.Sprintf("```%s block does not parse: %s", lang, msg),
						overrides))
				}
			}
		}
		return ast.WalkContinue, nil
	})
	return out
}

// =============================================================================
// rule registry
// =============================================================================

type mdRule struct {
	id       string
	severity string
	title    string
	advice   string
}

var mdRules = map[string]mdRule{
	"image_no_alt": {
		id:       "image_no_alt",
		severity: SeverityWarning,
		title:    "Image without alt text",
		advice:   "Empty alt makes the image invisible to screen readers and unusable when images fail to load. Describe what the image shows.",
	},
	"link_local_broken": {
		id:       "link_local_broken",
		severity: SeverityWarning,
		title:    "Broken local link",
		advice:   "The link target is missing from the repository. Either fix the path or remove the link.",
	},
	"link_anchor_broken": {
		id:       "link_anchor_broken",
		severity: SeverityWarning,
		title:    "Broken in-document anchor",
		advice:   "The `#anchor` does not match any heading in this file. GitHub generates slugs from heading text (lowercased, spaces → `-`); fix the anchor or rename the heading.",
	},
	"codeblock_no_language": {
		id:       "codeblock_no_language",
		severity: SeverityInfo,
		title:    "Fenced code block without language tag",
		advice:   "Tag the block (```sh, ```json, ```ts, …) so syntax highlighting works and screen readers announce the language.",
	},
	"codeblock_invalid_payload": {
		id:       "codeblock_invalid_payload",
		severity: SeverityWarning,
		title:    "Code block payload does not parse",
		advice:   "The doc claims this is JSON/YAML but the contents fail to parse. Either fix the snippet or relabel the block.",
	},
}

func mdFindingAt(rel string, line int, rule mdRule, msg string, overrides map[int]*gateOverride) Finding {
	if ov := lookupOverrideForRule(line, overrides, rule.id); ov != nil {
		return overrideAcceptedFinding("markdown_lint", rel, rule.id, dockerfileInstr{
			Line:     line,
			Override: ov,
		})
	}
	return Finding{
		Severity: rule.severity,
		Title:    rule.title,
		Message:  fmt.Sprintf("%s:%d %s. %s", rel, line, msg, rule.advice),
		FilePath: fmt.Sprintf("%s:%d:%s", rel, line, rule.id),
	}
}

// =============================================================================
// helpers: link classification
// =============================================================================

func isAnchorOnly(dest string) bool {
	return strings.HasPrefix(dest, "#")
}

func isLocalLink(dest string) bool {
	if dest == "" {
		return false
	}
	// Skip absolute URLs and unusual schemes. Markdown source rarely
	// declares custom schemes; if it does we play it safe and ignore.
	if u, err := url.Parse(dest); err == nil && u.Scheme != "" {
		return false
	}
	if strings.HasPrefix(dest, "//") {
		return false
	}
	if strings.HasPrefix(dest, "#") {
		return false
	}
	return true
}

func splitPathAndAnchor(dest string) (string, string) {
	if i := strings.Index(dest, "#"); i >= 0 {
		return dest[:i], dest[i+1:]
	}
	return dest, ""
}

// localTargetExists resolves dest relative to the directory of the
// markdown file (rel) and stats the resulting path. URL-decodes the
// path because authors sometimes write `My%20Notes.md` in links.
func localTargetExists(rel, root, dest string) bool {
	if dest == "" {
		// Pure anchor or empty link — handled elsewhere.
		return true
	}
	decoded, err := url.PathUnescape(dest)
	if err != nil {
		decoded = dest
	}
	target := filepath.Join(root, filepath.Dir(rel), decoded)
	if _, err := os.Stat(target); err == nil {
		return true
	}
	return false
}

// =============================================================================
// helpers: heading slugs
// =============================================================================

// collectHeadingSlugs walks the document, computes the GitHub-flavored
// slug for each heading text, and stores them in a lowercase set so
// link_anchor_broken can match in O(1).
func collectHeadingSlugs(doc ast.Node, source []byte) map[string]bool {
	out := map[string]bool{}
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if h, ok := n.(*ast.Heading); ok {
			out[githubSlug(extractText(h, source))] = true
		}
		return ast.WalkContinue, nil
	})
	return out
}

// githubSlug approximates GitHub's heading-to-anchor algorithm: lower,
// drop punctuation except `-` `_`, collapse whitespace into `-`. Good
// enough for the dominant case; perfect parity with GitHub's rules
// (which involve a Ruby gem) is out of scope.
func githubSlug(text string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(text) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case r == ' ' || r == '\t' || r == '-':
			if !prevDash && b.Len() > 0 {
				b.WriteRune('-')
				prevDash = true
			}
		case r == '_':
			b.WriteRune(r)
			prevDash = false
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

// extractText concatenates every Text/RawText/CodeSpan child of a node.
// Used for heading text and image alt text.
func extractText(n ast.Node, source []byte) string {
	var b strings.Builder
	_ = ast.Walk(n, func(child ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch c := child.(type) {
		case *ast.Text:
			b.Write(c.Segment.Value(source))
		case *ast.String:
			b.Write(c.Value)
		case *ast.CodeSpan:
			// Inline code inside a heading like `## `code`` — treat as
			// part of the slug text.
			for cc := c.FirstChild(); cc != nil; cc = cc.NextSibling() {
				if t, ok := cc.(*ast.Text); ok {
					b.Write(t.Segment.Value(source))
				}
			}
		}
		return ast.WalkContinue, nil
	})
	return b.String()
}

// =============================================================================
// helpers: line numbers
// =============================================================================

// computeLineStarts builds a sorted slice of byte offsets where each
// line begins. lineStarts[k] = byte offset of the start of line k+1.
func computeLineStarts(source []byte) []int {
	out := []int{0}
	for i, b := range source {
		if b == '\n' {
			out = append(out, i+1)
		}
	}
	return out
}

// nodeLine looks up the 1-based line number of a node by walking up to
// the nearest block ancestor that has Lines(), then mapping the first
// segment's start offset to a line via the precomputed lineStarts.
func nodeLine(n ast.Node, lineStarts []int) int {
	for cur := n; cur != nil; cur = cur.Parent() {
		if cur.Type() != ast.TypeBlock {
			continue
		}
		if liner, ok := cur.(interface{ Lines() *text.Segments }); ok {
			segs := liner.Lines()
			if segs != nil && segs.Len() > 0 {
				return offsetToLine(segs.At(0).Start, lineStarts)
			}
		}
	}
	return 1
}

func offsetToLine(offset int, lineStarts []int) int {
	idx := sort.SearchInts(lineStarts, offset+1) - 1
	if idx < 0 {
		idx = 0
	}
	return idx + 1
}

// =============================================================================
// helpers: payload validation
// =============================================================================

// fencedBlockBody concatenates all body lines of a FencedCodeBlock.
func fencedBlockBody(b *ast.FencedCodeBlock, source []byte) string {
	var sb strings.Builder
	lines := b.Lines()
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		sb.Write(seg.Value(source))
	}
	return sb.String()
}

// validatePayload returns "" when a code block parses cleanly under its
// declared language, or a short error string when it doesn't. Only strict
// JSON and YAML are validated — JSON supersets (jsonc, json5, hjson) and
// line-delimited variants (ndjson, jsonl) are passed through unchanged
// because the Go stdlib parser rejects their legal syntax.
func validatePayload(lang, body string) string {
	switch strings.ToLower(lang) {
	case "json":
		var v any
		if err := json.Unmarshal([]byte(body), &v); err != nil {
			return err.Error()
		}
	// JSON supersets: pass through — stdlib json.Unmarshal rejects
	// comments, trailing commas, unquoted keys, etc.
	case "jsonc", "json5", "hjson", "json with comments":
		return ""
	// Line-delimited JSON: each non-empty line is a JSON value.
	// We validate line-by-line but treat partial/streaming payloads
	// (last line may be incomplete) as pass-through.
	case "ndjson", "jsonl", "ldjson":
		for _, line := range strings.Split(strings.TrimSpace(body), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var v any
			if err := json.Unmarshal([]byte(line), &v); err != nil {
				return err.Error()
			}
		}
	case "yaml", "yml":
		var v any
		if err := yaml.Unmarshal([]byte(body), &v); err != nil {
			return err.Error()
		}
	}
	return ""
}

func parseMarkdownOptions(opts json.RawMessage) markdownLintOptions {
	if len(opts) == 0 {
		return markdownLintOptions{}
	}
	var o markdownLintOptions
	_ = json.Unmarshal(opts, &o)
	return o
}
