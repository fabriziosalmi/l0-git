package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/net/html"
)

// html_lint is implemented over the x/net/html Tokenizer (not the tree
// parser) so each finding pins to the actual source line of the offending
// tag. Tree-shaped rules (mystery_meat, label/input pairing) are handled
// with explicit element stacks rather than a DOM walk.

type htmlLintOptions struct {
	scanOptions
	DisabledRules []string `json:"disabled_rules,omitempty"`
}

func checkHtmlLint(ctx context.Context, root string, opts json.RawMessage) ([]Finding, error) {
	options := parseHtmlOptions(opts)

	if skip, stop := requireGitRepo(root, "html_lint",
		"This gate uses git ls-files to scan tracked HTML files."); stop {
		return skip, nil
	}
	files, err := gitLsFiles(ctx, root)
	if err != nil {
		return []Finding{{
			Severity: SeverityWarning,
			Title:    "html_lint failed",
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
		if pathExcluded(rel, options.ExcludePaths) {
			continue
		}
		if !isHTMLBasename(filepath.Base(rel)) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		out = append(out, evaluateHtmlFile(rel, data, disabled)...)
	}
	return out, nil
}

func isHTMLBasename(name string) bool {
	low := strings.ToLower(name)
	return strings.HasSuffix(low, ".html") || strings.HasSuffix(low, ".htm")
}

// collectHtmlOverrides extracts `<!-- l0git: ignore … -->` directives from
// the source. Keyed by 1-based line number.
func collectHtmlOverrides(data []byte) map[int]*gateOverride {
	out := map[int]*gateOverride{}
	src := strings.ReplaceAll(string(data), "\r\n", "\n")
	for i, line := range strings.Split(src, "\n") {
		idx := strings.Index(line, "<!--")
		if idx < 0 {
			continue
		}
		end := strings.Index(line[idx:], "-->")
		if end < 0 {
			continue
		}
		body := strings.TrimSpace(line[idx+4 : idx+end])
		if ov := parseGateOverride("# " + body); ov != nil {
			ov.Line = i + 1
			out[ov.Line] = ov
		}
	}
	return out
}

// collectLabelForIDs is a quick first-pass scan for the placeholder_as_label
// rule: which input ids are referenced by a `<label for="…">`.
func collectLabelForIDs(data []byte) map[string]bool {
	out := map[string]bool{}
	z := html.NewTokenizer(strings.NewReader(string(data)))
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			return out
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}
		nameBytes, hasAttr := z.TagName()
		if string(nameBytes) != "label" {
			continue
		}
		for hasAttr {
			k, v, more := z.TagAttr()
			if string(k) == "for" {
				out[string(v)] = true
			}
			hasAttr = more
		}
	}
}

// evaluateHtmlFile is the second-pass scanner: emits findings with real
// line numbers by counting newlines in each token's raw bytes as we walk
// the stream.
func evaluateHtmlFile(rel string, data []byte, disabled map[string]bool) []Finding {
	overrides := collectHtmlOverrides(data)
	labelFor := collectLabelForIDs(data)

	src := string(data)
	z := html.NewTokenizer(strings.NewReader(src))

	out := []Finding{}
	currentLine := 1

	var anchorStack []accEntry // open <a>
	var buttonStack []accEntry // open <button>
	labelDepth := 0            // any <label> currently open (for wrapping detection)

	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		startLine := currentLine
		// Advance currentLine by newlines in the token's raw bytes.
		raw := z.Raw()
		currentLine += strings.Count(string(raw), "\n")

		switch tt {
		case html.StartTagToken, html.SelfClosingTagToken:
			nameBytes, hasAttr := z.TagName()
			tagName := string(nameBytes)
			attrs := []html.Attribute{}
			for hasAttr {
				k, v, more := z.TagAttr()
				attrs = append(attrs, html.Attribute{Key: string(k), Val: string(v)})
				hasAttr = more
			}

			switch tagName {
			case "meta":
				if !disabled["viewport_no_zoom"] && strings.EqualFold(getAttr(attrs, "name"), "viewport") {
					if blocksZoom(getAttr(attrs, "content")) {
						out = append(out, htmlRuleFindingAt(rel, startLine,
							htmlRules["viewport_no_zoom"],
							"viewport meta blocks zooming",
							overrides))
					}
				}
			case "video":
				if !disabled["autoplay_with_sound"] {
					if hasAttrInList(attrs, "autoplay") && !hasAttrInList(attrs, "muted") {
						out = append(out, htmlRuleFindingAt(rel, startLine,
							htmlRules["autoplay_with_sound"],
							"autoplay without muted",
							overrides))
					}
				}
			case "a":
				if !disabled["target_blank_no_rel"] {
					if getAttr(attrs, "target") == "_blank" && !relCoversNoopener(getAttr(attrs, "rel")) {
						out = append(out, htmlRuleFindingAt(rel, startLine,
							htmlRules["target_blank_no_rel"],
							`target="_blank" without rel="noopener" or "noreferrer"`,
							overrides))
					}
				}
				if !disabled["mystery_meat_nav"] && tt == html.StartTagToken {
					anchorStack = append(anchorStack, accEntry{
						tag: "a", line: startLine,
						ariaLabel:      hasAttrInList(attrs, "aria-label") && strings.TrimSpace(getAttr(attrs, "aria-label")) != "",
						title:          hasAttrInList(attrs, "title") && strings.TrimSpace(getAttr(attrs, "title")) != "",
						ariaLabelledby: hasAttrInList(attrs, "aria-labelledby"),
					})
				}
			case "button":
				if !disabled["mystery_meat_nav"] && tt == html.StartTagToken {
					buttonStack = append(buttonStack, accEntry{
						tag: "button", line: startLine,
						ariaLabel:      hasAttrInList(attrs, "aria-label") && strings.TrimSpace(getAttr(attrs, "aria-label")) != "",
						title:          hasAttrInList(attrs, "title") && strings.TrimSpace(getAttr(attrs, "title")) != "",
						ariaLabelledby: hasAttrInList(attrs, "aria-labelledby"),
					})
				}
				if !disabled["reset_button"] && strings.EqualFold(getAttr(attrs, "type"), "reset") {
					out = append(out, htmlRuleFindingAt(rel, startLine,
						htmlRules["reset_button"],
						`<button type="reset"> wipes user input on click`,
						overrides))
				}
			case "input":
				if !disabled["reset_button"] && strings.EqualFold(getAttr(attrs, "type"), "reset") {
					out = append(out, htmlRuleFindingAt(rel, startLine,
						htmlRules["reset_button"],
						`<input type="reset"> wipes user input on click`,
						overrides))
				}
				if !disabled["placeholder_as_label"] && hasAttrInList(attrs, "placeholder") {
					ariaLabelOK := hasAttrInList(attrs, "aria-label") && strings.TrimSpace(getAttr(attrs, "aria-label")) != ""
					ariaLabelledbyOK := hasAttrInList(attrs, "aria-labelledby")
					inWrappingLabel := labelDepth > 0
					id := getAttr(attrs, "id")
					hasMatchingLabel := id != "" && labelFor[id]
					if !ariaLabelOK && !ariaLabelledbyOK && !inWrappingLabel && !hasMatchingLabel {
						out = append(out, htmlRuleFindingAt(rel, startLine,
							htmlRules["placeholder_as_label"],
							"input with placeholder but no associated <label> / aria-label",
							overrides))
					}
				}
			case "label":
				if tt == html.StartTagToken {
					labelDepth++
				}
			case "img":
				if !disabled["mystery_meat_nav"] {
					if alt := strings.TrimSpace(getAttr(attrs, "alt")); alt != "" {
						if n := len(anchorStack); n > 0 {
							anchorStack[n-1].altImg = true
						}
						if n := len(buttonStack); n > 0 {
							buttonStack[n-1].altImg = true
						}
					}
				}
			}

		case html.EndTagToken:
			nameBytes, _ := z.TagName()
			switch string(nameBytes) {
			case "a":
				if !disabled["mystery_meat_nav"] && len(anchorStack) > 0 {
					top := anchorStack[len(anchorStack)-1]
					anchorStack = anchorStack[:len(anchorStack)-1]
					if isMysteryMeatEntry(top) {
						out = append(out, htmlRuleFindingAt(rel, top.line,
							htmlRules["mystery_meat_nav"],
							"<a> with no accessible name (only icon, no text/aria-label/title)",
							overrides))
					}
				}
			case "button":
				if !disabled["mystery_meat_nav"] && len(buttonStack) > 0 {
					top := buttonStack[len(buttonStack)-1]
					buttonStack = buttonStack[:len(buttonStack)-1]
					if isMysteryMeatEntry(top) {
						out = append(out, htmlRuleFindingAt(rel, top.line,
							htmlRules["mystery_meat_nav"],
							"<button> with no accessible name (only icon, no text/aria-label/title)",
							overrides))
					}
				}
			case "label":
				if labelDepth > 0 {
					labelDepth--
				}
			}

		case html.TextToken:
			text := string(z.Text())
			if strings.TrimSpace(text) == "" {
				continue
			}
			if n := len(anchorStack); n > 0 {
				anchorStack[n-1].text += text
			}
			if n := len(buttonStack); n > 0 {
				buttonStack[n-1].text += text
			}
		}
	}

	// Anchors/buttons left open at EOF (malformed HTML) are evaluated too,
	// so we don't silently swallow real findings.
	for _, top := range anchorStack {
		if !disabled["mystery_meat_nav"] && isMysteryMeatEntry(top) {
			out = append(out, htmlRuleFindingAt(rel, top.line,
				htmlRules["mystery_meat_nav"],
				"<a> with no accessible name (unclosed in source)",
				overrides))
		}
	}
	for _, top := range buttonStack {
		if !disabled["mystery_meat_nav"] && isMysteryMeatEntry(top) {
			out = append(out, htmlRuleFindingAt(rel, top.line,
				htmlRules["mystery_meat_nav"],
				"<button> with no accessible name (unclosed in source)",
				overrides))
		}
	}
	return out
}

// accEntry is the per-tag scratch state used by mystery_meat. The struct
// lives at package scope (not inside evaluateHtmlFile) so the helper
// below can take it by value.
type accEntry struct {
	tag            string
	line           int
	ariaLabel      bool
	title          bool
	ariaLabelledby bool
	altImg         bool
	text           string
}

func isMysteryMeatEntry(e accEntry) bool {
	if e.ariaLabel || e.title || e.ariaLabelledby || e.altImg {
		return false
	}
	return strings.TrimSpace(e.text) == ""
}

func blocksZoom(content string) bool {
	c := strings.ToLower(content)
	if strings.Contains(c, "user-scalable=no") || strings.Contains(c, "user-scalable=0") {
		return true
	}
	if strings.Contains(c, "maximum-scale=1") {
		return true
	}
	return false
}

func relCoversNoopener(rel string) bool {
	for _, t := range strings.Fields(strings.ToLower(rel)) {
		if t == "noopener" || t == "noreferrer" {
			return true
		}
	}
	return false
}

func getAttr(attrs []html.Attribute, key string) string {
	for _, a := range attrs {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

func hasAttrInList(attrs []html.Attribute, key string) bool {
	for _, a := range attrs {
		if strings.EqualFold(a.Key, key) {
			return true
		}
	}
	return false
}

var htmlRules = map[string]htmlRule{
	"viewport_no_zoom": {
		id:       "viewport_no_zoom",
		severity: SeverityWarning,
		title:    "Viewport meta blocks user zoom",
		advice:   "user-scalable=no / maximum-scale=1 violates WCAG 1.4.4 (Resize Text). Drop the restriction.",
	},
	"autoplay_with_sound": {
		id:       "autoplay_with_sound",
		severity: SeverityWarning,
		title:    "<video autoplay> without muted",
		advice:   "Modern browsers refuse to autoplay video with sound. Add `muted`, or trigger playback from user interaction.",
	},
	"target_blank_no_rel": {
		id:       "target_blank_no_rel",
		severity: SeverityWarning,
		title:    `target="_blank" without rel="noopener"`,
		advice:   `Without rel="noopener", the new tab can read window.opener and run reverse-tabnabbing attacks. Add rel="noopener noreferrer".`,
	},
	"mystery_meat_nav": {
		id:       "mystery_meat_nav",
		severity: SeverityWarning,
		title:    "Icon-only control with no accessible name",
		advice:   "Screen readers announce nothing. Add visible text, or aria-label / title for the icon.",
	},
	"placeholder_as_label": {
		id:       "placeholder_as_label",
		severity: SeverityWarning,
		title:    "Input uses placeholder as a label",
		advice:   "Placeholders disappear on focus and have poor contrast — they're not labels. Add a <label for=…> or aria-label.",
	},
	"reset_button": {
		id:       "reset_button",
		severity: SeverityWarning,
		title:    "Form has a reset button",
		advice:   `<input/button type="reset"> wipes user input on accidental click. The 2026 web has zero legitimate use for this.`,
	},
}

type htmlRule struct {
	id       string
	severity string
	title    string
	advice   string
}

// htmlRuleFindingAt builds the finding pinned to an explicit line. The
// override grammar is shared with dockerfile_lint / compose_lint via
// lookupOverrideForRule.
func htmlRuleFindingAt(rel string, line int, rule htmlRule, msg string, overrides map[int]*gateOverride) Finding {
	if ov := lookupOverrideForRule(line, overrides, rule.id); ov != nil {
		return overrideAcceptedFinding("html_lint", rel, rule.id, dockerfileInstr{
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

func parseHtmlOptions(opts json.RawMessage) htmlLintOptions {
	if len(opts) == 0 {
		return htmlLintOptions{}
	}
	var o htmlLintOptions
	_ = json.Unmarshal(opts, &o)
	return o
}
