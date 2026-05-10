package main

import (
	"strings"
	"testing"
)

func runCSSRules(t *testing.T, src string) []Finding {
	t.Helper()
	return evaluateCssFile("style.css", src, nil)
}

func TestCSS_HiddenScrollbarDisplayNone(t *testing.T) {
	src := `.thing { color: red; }
::-webkit-scrollbar {
  display: none;
}
`
	fs := runCSSRules(t, src)
	if findFindingByRule(fs, "hidden_scrollbar") == nil {
		t.Errorf("expected hidden_scrollbar: %+v", fs)
	}
}

func TestCSS_HiddenScrollbarWidthZero(t *testing.T) {
	src := `::-webkit-scrollbar { width: 0; }`
	fs := runCSSRules(t, src)
	if findFindingByRule(fs, "hidden_scrollbar") == nil {
		t.Errorf("expected hidden_scrollbar via width:0: %+v", fs)
	}
}

// Outside the scrollbar selector, display:none is fine.
func TestCSS_DisplayNoneElsewhereIsOK(t *testing.T) {
	src := `.hidden { display: none; }`
	fs := runCSSRules(t, src)
	if findFindingByRule(fs, "hidden_scrollbar") != nil {
		t.Errorf("display:none on a non-scrollbar selector must not fire: %+v", fs)
	}
}

func TestCSS_ThinFontWeightOnBodyText(t *testing.T) {
	src := `body { font-weight: 100; }`
	fs := runCSSRules(t, src)
	if findFindingByRule(fs, "thin_font_weight") == nil {
		t.Errorf("expected thin_font_weight on body: %+v", fs)
	}
}

// Thin font weight on a class selector is conservative-skipped — we
// can't tell if it's body copy or a tiny decorative element.
func TestCSS_ThinFontWeightOnClassIsSkipped(t *testing.T) {
	src := `.tiny-label { font-weight: 100; }`
	fs := runCSSRules(t, src)
	if findFindingByRule(fs, "thin_font_weight") != nil {
		t.Errorf("class selector must not fire: %+v", fs)
	}
}

func TestCSS_JustifiedText(t *testing.T) {
	src := `article { text-align: justify; }`
	fs := runCSSRules(t, src)
	if findFindingByRule(fs, "justified_text") == nil {
		t.Errorf("expected justified_text: %+v", fs)
	}
}

func TestCSS_LeftAlignNoFinding(t *testing.T) {
	src := `article { text-align: left; }`
	fs := runCSSRules(t, src)
	if findFindingByRule(fs, "justified_text") != nil {
		t.Errorf("text-align:left must not fire: %+v", fs)
	}
}

func TestCSS_LineNumberAccuracy(t *testing.T) {
	src := `body {
  color: red;
}

article {
  text-align: justify;
}
`
	fs := runCSSRules(t, src)
	f := findFindingByRule(fs, "justified_text")
	if f == nil {
		t.Fatalf("expected justified_text, got: %+v", fs)
	}
	// FilePath shape: "<rel>:<line>:<rule>"
	if !strings.Contains(f.FilePath, ":6:") {
		t.Errorf("expected line 6 (text-align declaration), got %q", f.FilePath)
	}
}

func TestCSS_InlineOverride(t *testing.T) {
	src := `/* l0git: ignore justified_text reason: short pull-quote, hyphenation tuned */
article { text-align: justify; }
`
	fs := runCSSRules(t, src)
	if findFindingByRule(fs, "justified_text") != nil {
		t.Errorf("override must silence justified_text: %+v", fs)
	}
	found := false
	for _, f := range fs {
		if strings.HasSuffix(f.FilePath, ":override_justified_text") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected override_justified_text finding: %+v", fs)
	}
}

// Comments inside a rule must not throw off line counting downstream.
func TestCSS_CommentsPreserveLines(t *testing.T) {
	src := `body {
  /* multi
     line */
  color: red;
  text-align: justify;
}
`
	fs := runCSSRules(t, src)
	f := findFindingByRule(fs, "justified_text")
	if f == nil {
		t.Fatalf("expected justified_text, got: %+v", fs)
	}
	if !strings.Contains(f.FilePath, ":5:") {
		t.Errorf("expected line 5 (after multi-line comment), got %q", f.FilePath)
	}
}

// body.dark-theme is still body text — thin weight should fire.
func TestCSS_ThinFontWeight_BodyWithModifierClass(t *testing.T) {
	src := `body.dark-theme { font-weight: 100; }
`
	fs := runCSSRules(t, src)
	if findFindingByRule(fs, "thin_font_weight") == nil {
		t.Errorf("expected thin_font_weight for body.dark-theme, got: %+v", fs)
	}
}

// Comma-separated selector list — one body-text part is enough.
func TestCSS_ThinFontWeight_CommaSelector(t *testing.T) {
	src := `html, body { font-weight: 200; }
`
	fs := runCSSRules(t, src)
	if findFindingByRule(fs, "thin_font_weight") == nil {
		t.Errorf("expected thin_font_weight for html, body selector, got: %+v", fs)
	}
}

// @media print { … justify … } must NOT fire.
func TestCSS_JustifiedText_PrintMediaExempt(t *testing.T) {
	src := `@media print {
  p {
    text-align: justify;
  }
}
`
	fs := runCSSRules(t, src)
	if findFindingByRule(fs, "justified_text") != nil {
		t.Errorf("justified_text must be silent inside @media print, got: %+v", fs)
	}
}

// Outside @media print the rule still fires.
func TestCSS_JustifiedText_NonPrintFires(t *testing.T) {
	src := `@media screen {
  p {
    text-align: justify;
  }
}
`
	fs := runCSSRules(t, src)
	if findFindingByRule(fs, "justified_text") == nil {
		t.Errorf("expected justified_text outside @media print, got: %+v", fs)
	}
}
