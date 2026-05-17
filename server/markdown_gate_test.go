package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func runMDRules(t *testing.T, source string) []Finding {
	t.Helper()
	return evaluateMarkdownFile("doc.md", t.TempDir(), []byte(source), nil)
}

func TestMD_ImageNoAlt(t *testing.T) {
	fs := runMDRules(t, "Look: ![](image.png) — no alt.\n")
	if findFindingByRule(fs, "image_no_alt") == nil {
		t.Fatalf("expected image_no_alt: %+v", fs)
	}
	fs = runMDRules(t, "![Diagram of pipeline](image.png)\n")
	if findFindingByRule(fs, "image_no_alt") != nil {
		t.Errorf("non-empty alt must not fire: %+v", fs)
	}
}

func TestMD_CodeblockNoLanguage(t *testing.T) {
	src := "```\nplain text\n```\n"
	fs := runMDRules(t, src)
	if findFindingByRule(fs, "codeblock_no_language") == nil {
		t.Fatalf("expected codeblock_no_language: %+v", fs)
	}
	src = "```sh\necho hi\n```\n"
	fs = runMDRules(t, src)
	if findFindingByRule(fs, "codeblock_no_language") != nil {
		t.Errorf("tagged block must not fire: %+v", fs)
	}
}

// codeblock_no_language is suppressed on changelog-style files: log
// pastes and old releases are not worth retagging, and tagging them
// rewrites history nobody re-reads. Other rules still apply there.
func TestMD_CodeblockNoLanguage_SkippedInChangelogFiles(t *testing.T) {
	src := "```\nplain text\n```\n"
	for _, name := range []string{
		"CHANGELOG.md",
		"changelog.md",
		"HISTORY.md",
		"RELEASES.md",
		"CHANGES.md",
		"NEWS.md",
		"RELEASE_NOTES.md",
	} {
		t.Run(name, func(t *testing.T) {
			fs := evaluateMarkdownFile(name, t.TempDir(), []byte(src), nil)
			if f := findFindingByRule(fs, "codeblock_no_language"); f != nil {
				t.Errorf("%s must skip codeblock_no_language; got: %+v", name, f)
			}
		})
	}
	// Sanity: the same content in a non-changelog file MUST still fire.
	fs := evaluateMarkdownFile("docs/guide.md", t.TempDir(), []byte(src), nil)
	if findFindingByRule(fs, "codeblock_no_language") == nil {
		t.Errorf("non-changelog must still fire codeblock_no_language: %+v", fs)
	}
}

// Structural rules (broken link, invalid payload) must still run on
// CHANGELOG files — only the no-language nag is suppressed.
func TestMD_OtherRulesStillFireInChangelog(t *testing.T) {
	src := "```json\n{ not_quoted: 1 }\n```\n"
	fs := evaluateMarkdownFile("CHANGELOG.md", t.TempDir(), []byte(src), nil)
	if findFindingByRule(fs, "codeblock_invalid_payload") == nil {
		t.Errorf("CHANGELOG.md must still fire codeblock_invalid_payload: %+v", fs)
	}
}

func TestMD_CodeblockInvalidJSON(t *testing.T) {
	src := "```json\n{ not_quoted: 1 }\n```\n"
	fs := runMDRules(t, src)
	if findFindingByRule(fs, "codeblock_invalid_payload") == nil {
		t.Fatalf("invalid JSON must fire: %+v", fs)
	}
	src = "```json\n{\"name\":\"x\",\"n\":1}\n```\n"
	fs = runMDRules(t, src)
	if findFindingByRule(fs, "codeblock_invalid_payload") != nil {
		t.Errorf("valid JSON must not fire: %+v", fs)
	}
}

func TestMD_CodeblockInvalidYAML(t *testing.T) {
	src := "```yaml\nkey: [unbalanced\n```\n"
	fs := runMDRules(t, src)
	if findFindingByRule(fs, "codeblock_invalid_payload") == nil {
		t.Fatalf("invalid YAML must fire: %+v", fs)
	}
	src = "```yaml\nname: x\nn: 1\n```\n"
	fs = runMDRules(t, src)
	if findFindingByRule(fs, "codeblock_invalid_payload") != nil {
		t.Errorf("valid YAML must not fire: %+v", fs)
	}
}

// Languages we don't validate must NOT trip codeblock_invalid_payload.
func TestMD_CodeblockOtherLanguagesUnchecked(t *testing.T) {
	src := "```ts\nthis is not real ts\n```\n"
	fs := runMDRules(t, src)
	if findFindingByRule(fs, "codeblock_invalid_payload") != nil {
		t.Errorf("ts blocks must not be parse-validated: %+v", fs)
	}
}

// JSON supersets and line-delimited JSON must pass through without a
// codeblock_invalid_payload finding, even when the content would fail
// strict json.Unmarshal (jsonc with comments, json5 with trailing commas).
func TestMD_CodeblockJSONSupersets(t *testing.T) {
	cases := []struct {
		lang string
		body string
	}{
		{"jsonc", "// comment\n{\"a\": 1}\n"},
		{"json5", "{a: 1, /* comment */ b: 2,}\n"},
		{"hjson", "{\n  # comment\n  a: 1\n}\n"},
		{"ndjson", "{\"a\":1}\n{\"b\":2}\n"},
		{"jsonl", "{\"x\":true}\n"},
	}
	for _, c := range cases {
		t.Run(c.lang, func(t *testing.T) {
			src := "```" + c.lang + "\n" + c.body + "```\n"
			fs := runMDRules(t, src)
			if findFindingByRule(fs, "codeblock_invalid_payload") != nil {
				t.Errorf("%s block must not produce codeblock_invalid_payload: %+v", c.lang, fs)
			}
		})
	}
}

// Valid ndjson must pass; invalid ndjson (bad line) must still fire.
func TestMD_CodeblockNDJSONValidation(t *testing.T) {
	valid := "```ndjson\n{\"a\":1}\n{\"b\":2}\n```\n"
	if findFindingByRule(runMDRules(t, valid), "codeblock_invalid_payload") != nil {
		t.Errorf("valid ndjson must not produce finding")
	}
	invalid := "```ndjson\n{\"a\":1}\nnot json\n```\n"
	if findFindingByRule(runMDRules(t, invalid), "codeblock_invalid_payload") == nil {
		t.Errorf("invalid ndjson line must produce codeblock_invalid_payload")
	}
}

func TestMD_LinkLocalBroken(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "README.md"), "[Contribute](CONTRIBUTING.md)\n")
	fs := evaluateMarkdownFile("README.md", root,
		[]byte("[Contribute](CONTRIBUTING.md)\n"), nil)
	if findFindingByRule(fs, "link_local_broken") == nil {
		t.Fatalf("expected link_local_broken (CONTRIBUTING.md missing): %+v", fs)
	}
	mustWrite(t, filepath.Join(root, "CONTRIBUTING.md"), "x")
	fs = evaluateMarkdownFile("README.md", root,
		[]byte("[Contribute](CONTRIBUTING.md)\n"), nil)
	if findFindingByRule(fs, "link_local_broken") != nil {
		t.Errorf("link target now exists, must not fire: %+v", fs)
	}
}

// Static-site generators (VitePress, MkDocs, Docusaurus, …) resolve
// extensionless links. The gate must accept `./configuration` when
// `configuration.md` or `configuration/index.md` exists.
func TestMD_LinkLocalExtensionlessFallback(t *testing.T) {
	t.Run("sibling_md", func(t *testing.T) {
		root := t.TempDir()
		mustWrite(t, filepath.Join(root, "configuration.md"), "x")
		fs := evaluateMarkdownFile("getting-started.md", root,
			[]byte("[Config](./configuration)\n"), nil)
		if findFindingByRule(fs, "link_local_broken") != nil {
			t.Errorf("extensionless link with sibling .md must not fire: %+v", fs)
		}
	})
	t.Run("subdir_index_md", func(t *testing.T) {
		root := t.TempDir()
		mustWrite(t, filepath.Join(root, "guide", "index.md"), "x")
		fs := evaluateMarkdownFile("README.md", root,
			[]byte("[Guide](./guide)\n"), nil)
		if findFindingByRule(fs, "link_local_broken") != nil {
			t.Errorf("extensionless link to dir with index.md must not fire: %+v", fs)
		}
	})
	t.Run("typo_extension_still_fires", func(t *testing.T) {
		root := t.TempDir()
		mustWrite(t, filepath.Join(root, "configuration.md"), "x")
		fs := evaluateMarkdownFile("README.md", root,
			[]byte("[Config](./configuration.mdd)\n"), nil)
		if findFindingByRule(fs, "link_local_broken") == nil {
			t.Errorf("typo with extension must still fire (no fallback): %+v", fs)
		}
	})
}

func TestMD_LinkLocalIgnoresAbsoluteAndAnchors(t *testing.T) {
	src := "[ext](https://example.com) | [a](#intro)\n# Intro\n"
	fs := runMDRules(t, src)
	if findFindingByRule(fs, "link_local_broken") != nil {
		t.Errorf("absolute URL must not fire: %+v", fs)
	}
}

func TestMD_LinkAnchorBroken(t *testing.T) {
	src := "# Intro\n\n[Jump](#intro) and [oops](#missing)\n"
	fs := runMDRules(t, src)
	if findFindingByRule(fs, "link_anchor_broken") == nil {
		t.Fatalf("expected link_anchor_broken for #missing: %+v", fs)
	}
	// And #intro must NOT fire.
	for _, f := range fs {
		if strings.Contains(f.FilePath, "link_anchor_broken") &&
			strings.Contains(f.Message, "#intro") {
			t.Errorf("valid anchor #intro should not fire: %+v", f)
		}
	}
}

// GitHub-style slug normalisation: "## Hello, World!" → #hello-world
func TestMD_LinkAnchorSlugNormalisation(t *testing.T) {
	src := "## Hello, World!\n\n[link](#hello-world)\n"
	fs := runMDRules(t, src)
	if findFindingByRule(fs, "link_anchor_broken") != nil {
		t.Errorf("slug for 'Hello, World!' should match #hello-world: %+v", fs)
	}
}

// Inline override silences the rule and emits override_accepted.
func TestMD_InlineOverride(t *testing.T) {
	src := "<!-- l0git: ignore image_no_alt reason: decorative spacer -->\n![](spacer.png)\n"
	fs := runMDRules(t, src)
	if findFindingByRule(fs, "image_no_alt") != nil {
		t.Errorf("override must silence image_no_alt: %+v", fs)
	}
	found := false
	for _, f := range fs {
		if strings.HasSuffix(f.FilePath, ":override_image_no_alt") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected override_image_no_alt audit finding: %+v", fs)
	}
}

// goldmark's FencedCodeBlock segments cover the body, so we pin to the
// first body line (line 6 for "```\nno lang"). Pinning to the opening
// fence is a UX polish queued for later — the body line is unambiguous
// enough to navigate to.
func TestMD_LinePinning(t *testing.T) {
	src := "# Intro\n\nSome text.\n\n```\nno lang\n```\n"
	fs := runMDRules(t, src)
	f := findFindingByRule(fs, "codeblock_no_language")
	if f == nil {
		t.Fatalf("expected codeblock_no_language, got: %+v", fs)
	}
	if !strings.Contains(f.FilePath, ":6:") {
		t.Errorf("expected line 6 (body of unlabelled fence), got %q", f.FilePath)
	}
}
