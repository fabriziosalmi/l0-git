package main

import (
	"strings"
	"testing"
)

func runHTMLRules(t *testing.T, html string) []Finding {
	t.Helper()
	return evaluateHtmlFile("page.html", []byte(html), nil)
}

func TestHTML_ViewportBlocksZoom(t *testing.T) {
	for _, content := range []string{
		`<!DOCTYPE html><html><head><meta name="viewport" content="width=device-width, user-scalable=no"></head></html>`,
		`<!DOCTYPE html><html><head><meta name="viewport" content="width=device-width, maximum-scale=1.0"></head></html>`,
	} {
		t.Run(content, func(t *testing.T) {
			fs := runHTMLRules(t, content)
			if findFindingByRule(fs, "viewport_no_zoom") == nil {
				t.Errorf("expected viewport_no_zoom; got: %+v", fs)
			}
		})
	}
}

func TestHTML_ViewportSafeContent(t *testing.T) {
	fs := runHTMLRules(t, `<meta name="viewport" content="width=device-width, initial-scale=1">`)
	if findFindingByRule(fs, "viewport_no_zoom") != nil {
		t.Errorf("safe viewport should not fire: %+v", fs)
	}
}

func TestHTML_AutoplayWithoutMuted(t *testing.T) {
	fs := runHTMLRules(t, `<video autoplay src="x.mp4"></video>`)
	if findFindingByRule(fs, "autoplay_with_sound") == nil {
		t.Errorf("expected autoplay_with_sound: %+v", fs)
	}
	fs = runHTMLRules(t, `<video autoplay muted src="x.mp4"></video>`)
	if findFindingByRule(fs, "autoplay_with_sound") != nil {
		t.Errorf("muted autoplay must be silent: %+v", fs)
	}
}

func TestHTML_TargetBlankWithoutRel(t *testing.T) {
	fs := runHTMLRules(t, `<a href="https://x" target="_blank">x</a>`)
	if findFindingByRule(fs, "target_blank_no_rel") == nil {
		t.Errorf("expected target_blank_no_rel: %+v", fs)
	}
	fs = runHTMLRules(t, `<a href="https://x" target="_blank" rel="noopener noreferrer">x</a>`)
	if findFindingByRule(fs, "target_blank_no_rel") != nil {
		t.Errorf("rel=noopener must satisfy: %+v", fs)
	}
	fs = runHTMLRules(t, `<a href="https://x" target="_blank" rel="noreferrer">x</a>`)
	if findFindingByRule(fs, "target_blank_no_rel") != nil {
		t.Errorf("rel=noreferrer alone must satisfy: %+v", fs)
	}
}

func TestHTML_MysteryMeatNav(t *testing.T) {
	fs := runHTMLRules(t, `<button><svg><path/></svg></button>`)
	if findFindingByRule(fs, "mystery_meat_nav") == nil {
		t.Errorf("icon-only button must fire: %+v", fs)
	}
	fs = runHTMLRules(t, `<button aria-label="Close"><svg/></button>`)
	if findFindingByRule(fs, "mystery_meat_nav") != nil {
		t.Errorf("aria-label must satisfy: %+v", fs)
	}
	fs = runHTMLRules(t, `<a href="x"><svg/></a>`)
	if findFindingByRule(fs, "mystery_meat_nav") == nil {
		t.Errorf("icon-only anchor must fire: %+v", fs)
	}
	fs = runHTMLRules(t, `<button>Submit</button>`)
	if findFindingByRule(fs, "mystery_meat_nav") != nil {
		t.Errorf("button with text must not fire: %+v", fs)
	}
	fs = runHTMLRules(t, `<button><img alt="Close" src="x"/></button>`)
	if findFindingByRule(fs, "mystery_meat_nav") != nil {
		t.Errorf("img with alt counts as accessible name: %+v", fs)
	}
}

func TestHTML_PlaceholderAsLabel(t *testing.T) {
	fs := runHTMLRules(t, `<form><input id="email" placeholder="Email"></form>`)
	if findFindingByRule(fs, "placeholder_as_label") == nil {
		t.Errorf("input with placeholder, no label, no aria-label must fire: %+v", fs)
	}
	fs = runHTMLRules(t, `<form><label for="email">Email</label><input id="email" placeholder="user@example"></form>`)
	if findFindingByRule(fs, "placeholder_as_label") != nil {
		t.Errorf("matching label[for] must satisfy: %+v", fs)
	}
	fs = runHTMLRules(t, `<form><label>Email <input placeholder="user@example"></label></form>`)
	if findFindingByRule(fs, "placeholder_as_label") != nil {
		t.Errorf("wrapping label must satisfy: %+v", fs)
	}
	fs = runHTMLRules(t, `<form><input aria-label="Email" placeholder="Email"></form>`)
	if findFindingByRule(fs, "placeholder_as_label") != nil {
		t.Errorf("aria-label must satisfy: %+v", fs)
	}
}

func TestHTML_ResetButton(t *testing.T) {
	fs := runHTMLRules(t, `<button type="reset">Reset</button>`)
	if findFindingByRule(fs, "reset_button") == nil {
		t.Errorf("reset button must fire: %+v", fs)
	}
	fs = runHTMLRules(t, `<input type="reset">`)
	if findFindingByRule(fs, "reset_button") == nil {
		t.Errorf("input type=reset must fire: %+v", fs)
	}
}

// Phase B-bis: findings now pin to the actual line of the offending tag.
func TestHTML_LineNumberAccuracy(t *testing.T) {
	src := `<!DOCTYPE html>
<html>
<head>
  <meta name="viewport" content="user-scalable=no">
</head>
<body>
  <video autoplay src="x.mp4"></video>
  <a href="https://x" target="_blank">x</a>
  <button type="reset">Reset</button>
</body>
</html>
`
	fs := runHTMLRules(t, src)
	want := map[string]int{
		"viewport_no_zoom":    4,
		"autoplay_with_sound": 7,
		"target_blank_no_rel": 8,
		"reset_button":        9,
	}
	for ruleID, wantLine := range want {
		f := findFindingByRule(fs, ruleID)
		if f == nil {
			t.Errorf("%s missing; got: %+v", ruleID, fs)
			continue
		}
		// FilePath shape: page.html:<line>:<rule_id>
		expected := "page.html:" + itoa(wantLine) + ":" + ruleID
		if f.FilePath != expected {
			t.Errorf("%s pinned to %q, want %q", ruleID, f.FilePath, expected)
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

// Inline override silences the rule and emits override_accepted.
func TestHTML_InlineOverride(t *testing.T) {
	src := `<!-- l0git: ignore reset_button reason: legacy form intentionally has reset -->
<form><button type="reset">Reset</button></form>`
	fs := runHTMLRules(t, src)
	if findFindingByRule(fs, "reset_button") != nil {
		t.Errorf("override must silence reset_button: %+v", fs)
	}
	found := false
	for _, f := range fs {
		if strings.HasSuffix(f.FilePath, ":override_reset_button") {
			found = true
			if !strings.Contains(f.Message, "legacy form intentionally") {
				t.Errorf("audit message must include reason: %q", f.Message)
			}
		}
	}
	if !found {
		t.Errorf("expected override_reset_button audit finding: %+v", fs)
	}
}
