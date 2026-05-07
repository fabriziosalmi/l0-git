package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// Default off → never fires. Honours the "we can only verify
// protection-as-code, never the actual server state" contract.
func TestBranchProtection_DefaultOffSilent(t *testing.T) {
	root := t.TempDir() // no .github/settings.yml
	fs, err := checkBranchProtectionDeclared(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 0 {
		t.Errorf("default off must be silent, got: %+v", fs)
	}
}

// Enabled, file missing → one info finding pointing at the convention.
func TestBranchProtection_EnabledNoFile(t *testing.T) {
	fs, err := checkBranchProtectionDeclared(context.Background(), t.TempDir(), []byte(`{"enabled":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 {
		t.Fatalf("expected 1 finding, got: %+v", fs)
	}
	if fs[0].Severity != SeverityInfo {
		t.Errorf("severity = %q, want info", fs[0].Severity)
	}
	if !strings.Contains(fs[0].Message, "no .github/settings.yml") {
		t.Errorf("message should explain the missing file: %q", fs[0].Message)
	}
}

// File exists but doesn't declare branch protection → still fires.
func TestBranchProtection_EnabledFileWithoutBranches(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".github", "settings.yml"),
		"repository:\n  name: foo\n  description: bar\n")
	fs, err := checkBranchProtectionDeclared(context.Background(), root, []byte(`{"enabled":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 {
		t.Fatalf("expected 1 finding, got: %+v", fs)
	}
	if !strings.Contains(fs[0].Message, "no `branches:`") {
		t.Errorf("message should explain the empty branches: %q", fs[0].Message)
	}
}

// Proper settings.yml with branches[].protection → silent.
func TestBranchProtection_EnabledProperSettings(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".github", "settings.yml"), `
branches:
  - name: main
    protection:
      required_pull_request_reviews:
        required_approving_review_count: 1
      enforce_admins: false
      allow_force_pushes: false
`)
	fs, err := checkBranchProtectionDeclared(context.Background(), root, []byte(`{"enabled":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 0 {
		t.Errorf("proper settings must satisfy the gate, got: %+v", fs)
	}
}

// branches: array but no entry has protection: → fires (declaring the
// branches without their protection is the same as not declaring).
func TestBranchProtection_BranchesWithoutProtection(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".github", "settings.yml"), `
branches:
  - name: main
    label: production
  - name: develop
`)
	fs, err := checkBranchProtectionDeclared(context.Background(), root, []byte(`{"enabled":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 {
		t.Errorf("branches without protection: must fire, got: %+v", fs)
	}
}

// Malformed YAML → also fires (the gate can't verify anything against
// invalid input, and the user should fix their settings.yml anyway).
func TestBranchProtection_MalformedYAML(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".github", "settings.yml"), "branches: [oops\n")
	fs, err := checkBranchProtectionDeclared(context.Background(), root, []byte(`{"enabled":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 {
		t.Errorf("malformed YAML should fire (treated as not-declared): %+v", fs)
	}
}
