package main

import (
	"reflect"
	"testing"
)

// TestParseDockerfile_BasicShape covers the determinism contract: the
// parser must emit one instruction per logical directive, with the
// correct kind, args, and source-line bookkeeping.
func TestParseDockerfile_BasicShape(t *testing.T) {
	src := `# syntax=docker/dockerfile:1
FROM node:20-alpine
RUN apk add --no-cache git
USER nobody
CMD ["node", "server.js"]
`
	got := parseDockerfile(src)
	if len(got) != 4 {
		t.Fatalf("expected 4 instructions, got %d: %+v", len(got), got)
	}
	want := []struct {
		kind, args string
		line       int
	}{
		{"FROM", "node:20-alpine", 2},
		{"RUN", "apk add --no-cache git", 3},
		{"USER", "nobody", 4},
		{"CMD", `["node", "server.js"]`, 5},
	}
	for i, w := range want {
		if got[i].Kind != w.kind || got[i].Args != w.args || got[i].Line != w.line {
			t.Errorf("instr[%d] = {%s %s %d}, want {%s %s %d}",
				i, got[i].Kind, got[i].Args, got[i].Line,
				w.kind, w.args, w.line)
		}
	}
}

func TestParseDockerfile_LineContinuation(t *testing.T) {
	src := "FROM debian \\\n  AS base\nRUN echo a \\\n  && echo b \\\n  && echo c\n"
	got := parseDockerfile(src)
	if len(got) != 2 {
		t.Fatalf("expected 2 instructions, got %d: %+v", len(got), got)
	}
	if got[0].Kind != "FROM" || got[0].Line != 1 || got[0].EndLine != 2 {
		t.Errorf("FROM instr unexpected: %+v", got[0])
	}
	if got[1].Kind != "RUN" || got[1].Line != 3 || got[1].EndLine != 5 {
		t.Errorf("RUN instr unexpected: %+v", got[1])
	}
}

// CRLF input must produce identical line numbers to LF input — editors
// and CI runners differ on this, the gate must not.
func TestParseDockerfile_CRLF(t *testing.T) {
	src := "FROM scratch\r\nUSER 0\r\n"
	got := parseDockerfile(src)
	if len(got) != 2 {
		t.Fatalf("expected 2, got %d: %+v", len(got), got)
	}
	if got[1].Line != 2 {
		t.Errorf("USER line under CRLF = %d, want 2", got[1].Line)
	}
}

// TestParseGateOverride covers every shape the directive grammar accepts
// or rejects — this is the security boundary between "rule fired" and
// "rule deliberately silenced", so the parse table must be exhaustive.
func TestParseGateOverride(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantIDs []string
		wantR   string
		wantNil bool
	}{
		{name: "single", input: "# l0git: ignore from_latest", wantIDs: []string{"from_latest"}},
		{name: "single_with_reason", input: "# l0git: ignore from_latest reason: dev image", wantIDs: []string{"from_latest"}, wantR: "dev image"},
		{name: "multi", input: "# l0git: ignore from_latest, missing_user", wantIDs: []string{"from_latest", "missing_user"}},
		{name: "multi_with_reason", input: "# l0git: ignore from_latest, missing_user reason: legacy", wantIDs: []string{"from_latest", "missing_user"}, wantR: "legacy"},
		{name: "wildcard", input: "# l0git: ignore *", wantIDs: []string{"*"}},
		{name: "spaces", input: "#  l0git:   ignore   from_latest   reason:   x  ", wantIDs: []string{"from_latest"}, wantR: "x"},
		{name: "no_hash", input: "l0git: ignore from_latest", wantNil: true},
		{name: "wrong_prefix", input: "# l0other: ignore foo", wantNil: true},
		{name: "missing_ignore", input: "# l0git: foo bar", wantNil: true},
		{name: "no_rules", input: "# l0git: ignore", wantNil: true},
		{name: "empty_line", input: "", wantNil: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseGateOverride(tc.input)
			if tc.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected override, got nil")
			}
			if !reflect.DeepEqual(got.RuleIDs, tc.wantIDs) {
				t.Errorf("RuleIDs = %v, want %v", got.RuleIDs, tc.wantIDs)
			}
			if got.Reason != tc.wantR {
				t.Errorf("Reason = %q, want %q", got.Reason, tc.wantR)
			}
		})
	}
}

// Override applies to the next non-comment, non-blank instruction. A
// blank line in between cancels the pending override.
func TestParseDockerfile_OverridePending(t *testing.T) {
	src := `# l0git: ignore from_latest reason: dev base
FROM node:latest
USER nobody`
	got := parseDockerfile(src)
	if len(got) != 2 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].Override == nil || got[0].Override.RuleIDs[0] != "from_latest" {
		t.Errorf("expected override on FROM, got: %+v", got[0])
	}
	if got[1].Override != nil {
		t.Errorf("USER should not have an override: %+v", got[1])
	}
}

func TestParseDockerfile_BlankLineCancelsOverride(t *testing.T) {
	src := "# l0git: ignore from_latest reason: x\n\nFROM node:latest\n"
	got := parseDockerfile(src)
	if len(got) != 1 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].Override != nil {
		t.Errorf("blank line must clear pending override; got: %+v", got[0])
	}
}

// gateOverride.matches uses literal equality plus a single wildcard.
func TestGateOverride_Matches(t *testing.T) {
	o := &gateOverride{RuleIDs: []string{"from_latest", "user_root"}}
	if !o.matches("from_latest") {
		t.Errorf("should match from_latest")
	}
	if !o.matches("user_root") {
		t.Errorf("should match user_root")
	}
	if o.matches("missing_user") {
		t.Errorf("must not match missing_user")
	}
	star := &gateOverride{RuleIDs: []string{"*"}}
	if !star.matches("anything") {
		t.Errorf("wildcard should match")
	}
	var nilO *gateOverride
	if nilO.matches("anything") {
		t.Errorf("nil override must never match")
	}
}
