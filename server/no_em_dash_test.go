package main

import (
	"os/exec"
	"strings"
	"testing"
)

// The project's prose, comments and messages do not use the em dash. This keeps
// it out: a colon, a comma or parentheses say the same thing. The character is
// built from its code point here so that this file passes its own check.
func TestRepositoryHasNoEmDash(t *testing.T) {
	out, err := exec.Command("git", "-C", "..", "grep", "-I", "-n", "-F", string(rune(0x2014))).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return // no match
		}
		t.Skipf("git grep unavailable: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) > 8 {
		lines = append(lines[:8], "...")
	}
	t.Errorf("em dash found (use a colon, comma or parentheses):\n%s", strings.Join(lines, "\n"))
}
