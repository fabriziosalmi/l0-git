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

// css_lint covers a deliberately tiny set of "objective crimes" — patterns
// that have no legitimate use on the modern web and are recognisable
// without a real CSS AST. We tokenize lazily by tracking selector blocks
// (the text between balanced `{` `}` braces) and which selector applies.

type cssLintOptions struct {
	scanOptions
	DisabledRules []string `json:"disabled_rules,omitempty"`
}

func checkCssLint(ctx context.Context, root string, opts json.RawMessage) ([]Finding, error) {
	options := parseCssOptions(opts)

	if skip, stop := requireGitRepo(root, "css_lint",
		"This gate uses git ls-files to scan tracked stylesheets."); stop {
		return skip, nil
	}
	files, err := gitLsFiles(ctx, root)
	if err != nil {
		return []Finding{{
			Severity: SeverityWarning,
			Title:    "css_lint failed",
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
		if !isCSSBasename(filepath.Base(rel)) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		out = append(out, evaluateCssFile(rel, string(data), disabled)...)
	}
	return out, nil
}

// isCSSBasename includes the common preprocessor extensions but excludes
// minified files where line numbers are useless and false-positive risk
// from concatenated rules is high.
func isCSSBasename(name string) bool {
	low := strings.ToLower(name)
	if strings.HasSuffix(low, ".min.css") {
		return false
	}
	for _, ext := range []string{".css", ".scss", ".sass", ".less", ".styl"} {
		if strings.HasSuffix(low, ext) {
			return true
		}
	}
	return false
}

// cssBlock is one selector → declarations region in the source.
type cssBlock struct {
	selector  string
	startLine int
	body      string // raw body text (between `{` and the matching `}`)
}

// extractCssBlocks tokenizes the source into top-level rules, line-tracking
// throughout. Nested rules (SCSS/LESS) are flattened to their outer
// selector — good enough for the rules we run.
func extractCssBlocks(src string) []cssBlock {
	src = stripCssComments(src)
	out := []cssBlock{}
	line := 1
	i := 0
	for i < len(src) {
		// Find the next `{` while counting lines.
		start := i
		for i < len(src) && src[i] != '{' {
			if src[i] == '\n' {
				line++
			}
			i++
		}
		if i >= len(src) {
			break
		}
		selector := strings.TrimSpace(src[start:i])
		blockStartLine := line
		i++ // consume `{`
		// Now find the matching `}` accounting for nesting.
		depth := 1
		bodyStart := i
		for i < len(src) && depth > 0 {
			switch src[i] {
			case '{':
				depth++
			case '}':
				depth--
			case '\n':
				line++
			}
			i++
		}
		bodyEnd := i - 1
		if bodyEnd < bodyStart {
			bodyEnd = bodyStart
		}
		out = append(out, cssBlock{
			selector:  selector,
			startLine: blockStartLine,
			body:      src[bodyStart:bodyEnd],
		})
	}
	return out
}

// stripCssComments replaces /* … */ with whitespace of the same length to
// preserve byte offsets and line counts. CSS doesn't have // comments
// outside SCSS; we tolerate them by also blanking // lines.
func stripCssComments(src string) string {
	b := []byte(src)
	out := make([]byte, len(b))
	copy(out, b)
	// /* … */ (multi-line)
	i := 0
	for i < len(out)-1 {
		if out[i] == '/' && out[i+1] == '*' {
			// Find closing */
			j := i + 2
			for j < len(out)-1 && !(out[j] == '*' && out[j+1] == '/') {
				if out[j] == '\n' {
					// preserve newlines for line counting
					i++
				}
				j++
			}
			end := j + 2
			if end > len(out) {
				end = len(out)
			}
			for k := i; k < end; k++ {
				if out[k] != '\n' {
					out[k] = ' '
				}
			}
			i = end
			continue
		}
		i++
	}
	// // line comments (SCSS)
	i = 0
	for i < len(out)-1 {
		if out[i] == '/' && out[i+1] == '/' {
			for j := i; j < len(out) && out[j] != '\n'; j++ {
				out[j] = ' '
			}
		}
		i++
	}
	return string(out)
}

func evaluateCssFile(rel, src string, disabled map[string]bool) []Finding {
	out := []Finding{}
	overrides := collectCssOverrides(src)
	blocks := extractCssBlocks(src)

	for _, b := range blocks {
		// hidden_scrollbar: ::-webkit-scrollbar { display:none; } or
		// width:0;. Match the selector first to keep this zero-FP.
		if !disabled["hidden_scrollbar"] && selectorContains(b.selector, "::-webkit-scrollbar") {
			if line, hit := findDeclaration(b, "display", "none"); hit {
				out = append(out, cssFinding(rel, line, cssRules["hidden_scrollbar"], "scrollbar hidden via display:none", overrides))
			} else if line, hit := findDeclaration(b, "width", "0"); hit {
				out = append(out, cssFinding(rel, line, cssRules["hidden_scrollbar"], "scrollbar hidden via width:0", overrides))
			}
		}

		// thin_font_weight: only on body-text selectors. font-weight: 100/200.
		if !disabled["thin_font_weight"] && selectorIsBodyText(b.selector) {
			if line, val, hit := findFontWeight(b); hit {
				if val == "100" || val == "200" {
					out = append(out, cssFinding(rel, line, cssRules["thin_font_weight"], fmt.Sprintf("font-weight: %s on a body-text selector", val), overrides))
				}
			}
		}

		// justified_text: text-align: justify on any selector.
		if !disabled["justified_text"] {
			if line, hit := findDeclaration(b, "text-align", "justify"); hit {
				out = append(out, cssFinding(rel, line, cssRules["justified_text"], "text-align: justify", overrides))
			}
		}
	}
	return out
}

var cssRules = map[string]composeRule{
	"hidden_scrollbar": {
		id:       "hidden_scrollbar",
		severity: SeverityWarning,
		title:    "Scrollbar hidden via CSS",
		advice:   "Hiding the scrollbar disorients users on long content. Style it (track/thumb colours) instead of removing it.",
	},
	"thin_font_weight": {
		id:       "thin_font_weight",
		severity: SeverityWarning,
		title:    "Thin font weight on body text",
		advice:   "font-weight: 100/200 disappears on non-Retina displays. Use 300 minimum for body copy.",
	},
	"justified_text": {
		id:       "justified_text",
		severity: SeverityWarning,
		title:    "text-align: justify",
		advice:   "Justified text on the web (without sophisticated hyphenation) creates rivers of whitespace and hurts readability. Use text-align: left.",
	},
}

func cssFinding(rel string, line int, rule composeRule, msg string, overrides map[int]*gateOverride) Finding {
	if ov := lookupOverrideForRule(line, overrides, rule.id); ov != nil {
		return overrideAcceptedFinding("css_lint", rel, rule.id, dockerfileInstr{
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

func selectorContains(selector, needle string) bool {
	return strings.Contains(strings.ToLower(selector), strings.ToLower(needle))
}

// selectorIsBodyText is conservative: only fires when the selector list
// contains a "body-copy" element selector unaccompanied by class/id
// modifiers that might indicate a stylistic exception.
func selectorIsBodyText(selector string) bool {
	for _, sel := range strings.Split(selector, ",") {
		s := strings.TrimSpace(strings.ToLower(sel))
		switch s {
		case "body", "html", "html, body", "p", "article", "main",
			"body p", "main p", "article p":
			return true
		}
	}
	return false
}

// findDeclaration finds the first declaration matching `prop: <value>`
// inside the block body and returns the absolute source line.
func findDeclaration(b cssBlock, prop, expectedValue string) (int, bool) {
	prop = strings.ToLower(prop)
	expected := strings.ToLower(expectedValue)
	line := b.startLine
	for _, raw := range strings.Split(b.body, "\n") {
		l := strings.ToLower(strings.TrimSpace(raw))
		if l == "" {
			line++
			continue
		}
		// Cheap declaration parse: split on `:`, trim, strip ; and !important.
		colon := strings.Index(l, ":")
		if colon > 0 {
			p := strings.TrimSpace(l[:colon])
			v := strings.TrimSpace(l[colon+1:])
			v = strings.TrimSuffix(v, ";")
			v = strings.TrimSpace(strings.TrimSuffix(v, "!important"))
			v = strings.TrimSpace(v)
			if p == prop && v == expected {
				return line, true
			}
		}
		line++
	}
	return 0, false
}

var fontWeightRe = regexp.MustCompile(`(?i)font-weight\s*:\s*([0-9]+)`)

func findFontWeight(b cssBlock) (int, string, bool) {
	line := b.startLine
	for _, raw := range strings.Split(b.body, "\n") {
		if m := fontWeightRe.FindStringSubmatch(raw); m != nil {
			return line, m[1], true
		}
		line++
	}
	return 0, "", false
}

// CSS comments use /* … */; the override directive is parsed from the
// stripped-comment text. We pre-extract overrides from the ORIGINAL
// source (so comments still exist) and key by line.
func collectCssOverrides(src string) map[int]*gateOverride {
	out := map[int]*gateOverride{}
	src = strings.ReplaceAll(src, "\r\n", "\n")
	for i, line := range strings.Split(src, "\n") {
		// /* l0git: ignore … */
		idx := strings.Index(line, "/*")
		if idx < 0 {
			continue
		}
		end := strings.Index(line[idx:], "*/")
		if end < 0 {
			continue
		}
		body := strings.TrimSpace(line[idx+2 : idx+end])
		if ov := parseGateOverride("# " + body); ov != nil {
			ov.Line = i + 1
			out[ov.Line] = ov
		}
	}
	return out
}

func parseCssOptions(opts json.RawMessage) cssLintOptions {
	if len(opts) == 0 {
		return cssLintOptions{}
	}
	var o cssLintOptions
	_ = json.Unmarshal(opts, &o)
	return o
}
