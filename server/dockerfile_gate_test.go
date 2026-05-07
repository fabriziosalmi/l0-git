package main

import (
	"context"
	"strings"
	"testing"
)

// findFindingByRule returns the first finding whose FilePath ends with
// `:<ruleID>` — the canonical way the gate marks each rule's output.
func findFindingByRule(fs []Finding, ruleID string) *Finding {
	for i := range fs {
		if strings.HasSuffix(fs[i].FilePath, ":"+ruleID) {
			return &fs[i]
		}
	}
	return nil
}

// directly drive evaluateDockerfileRules so we test the rule logic
// independent of git ls-files / option plumbing.
func runRules(t *testing.T, src string) []Finding {
	t.Helper()
	instrs := parseDockerfile(src)
	return evaluateDockerfileRules("Dockerfile", instrs, nil)
}

func TestDockerfile_FromUntagged(t *testing.T) {
	fs := runRules(t, "FROM node\n")
	if findFindingByRule(fs, "from_untagged") == nil {
		t.Fatalf("expected from_untagged, got: %+v", fs)
	}
}

func TestDockerfile_FromTaggedNoFinding(t *testing.T) {
	fs := runRules(t, "FROM node:20-alpine\nUSER nobody\nCMD [\"x\"]\n")
	if findFindingByRule(fs, "from_untagged") != nil {
		t.Errorf("tagged FROM should not fire from_untagged: %+v", fs)
	}
	if findFindingByRule(fs, "from_latest") != nil {
		t.Errorf("non-latest tag should not fire from_latest: %+v", fs)
	}
}

func TestDockerfile_FromLatest(t *testing.T) {
	fs := runRules(t, "FROM node:latest\n")
	if f := findFindingByRule(fs, "from_latest"); f == nil {
		t.Fatalf("expected from_latest, got: %+v", fs)
	} else if f.Severity != SeverityWarning {
		t.Errorf("severity = %q, want warning", f.Severity)
	}
}

// FROM with a digest is pinned by definition — must not fire either tag rule.
func TestDockerfile_FromDigestPinned(t *testing.T) {
	fs := runRules(t, "FROM node@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\n")
	if findFindingByRule(fs, "from_untagged") != nil {
		t.Errorf("digest pin must not fire from_untagged: %+v", fs)
	}
}

// --platform=linux/arm64 prefix and `AS stage` suffix must be stripped.
func TestDockerfile_FromWithPlatformAndStage(t *testing.T) {
	fs := runRules(t, "FROM --platform=linux/amd64 node:20-alpine AS build\n")
	if findFindingByRule(fs, "from_untagged") != nil {
		t.Errorf("platform+AS should not confuse the parser: %+v", fs)
	}
}

func TestDockerfile_AddInstruction(t *testing.T) {
	fs := runRules(t, "FROM node:20-alpine\nADD https://example.com/x.tgz /opt/\nUSER nobody\nCMD [\"x\"]\n")
	if f := findFindingByRule(fs, "add_instruction"); f == nil {
		t.Fatalf("expected add_instruction, got: %+v", fs)
	} else if f.Severity != SeverityInfo {
		t.Errorf("severity = %q, want info", f.Severity)
	}
}

func TestDockerfile_MissingUser(t *testing.T) {
	fs := runRules(t, "FROM node:20-alpine\nCMD [\"node\", \"server.js\"]\n")
	if findFindingByRule(fs, "missing_user") == nil {
		t.Fatalf("expected missing_user, got: %+v", fs)
	}
}

// USER must precede CMD; if it does, missing_user does NOT fire.
func TestDockerfile_UserPresentNoMissingFinding(t *testing.T) {
	fs := runRules(t, "FROM node:20-alpine\nUSER nobody\nCMD [\"node\", \"server.js\"]\n")
	if findFindingByRule(fs, "missing_user") != nil {
		t.Errorf("USER set: missing_user must not fire: %+v", fs)
	}
	if findFindingByRule(fs, "user_root") != nil {
		t.Errorf("USER nobody: user_root must not fire: %+v", fs)
	}
}

func TestDockerfile_UserRoot(t *testing.T) {
	fs := runRules(t, "FROM scratch\nUSER root\nCMD [\"/bin/x\"]\n")
	if findFindingByRule(fs, "user_root") == nil {
		t.Fatalf("expected user_root for USER root, got: %+v", fs)
	}
	fs = runRules(t, "FROM scratch\nUSER 0\nCMD [\"/bin/x\"]\n")
	if findFindingByRule(fs, "user_root") == nil {
		t.Fatalf("expected user_root for USER 0, got: %+v", fs)
	}
	fs = runRules(t, "FROM scratch\nUSER 0:0\nCMD [\"/bin/x\"]\n")
	if findFindingByRule(fs, "user_root") == nil {
		t.Fatalf("expected user_root for USER 0:0, got: %+v", fs)
	}
}

// Multi-stage: the missing-USER state resets per FROM.
func TestDockerfile_MultiStage(t *testing.T) {
	src := `FROM node:20 AS build
RUN npm ci
USER node
CMD ["node", "build"]

FROM node:20-alpine
CMD ["node", "server.js"]
`
	fs := runRules(t, src)
	// Stage 1 has USER → no missing_user. Stage 2 has none → exactly one
	// missing_user finding pinned to the stage-2 CMD.
	missing := []Finding{}
	for _, f := range fs {
		if strings.HasSuffix(f.FilePath, ":missing_user") {
			missing = append(missing, f)
		}
	}
	if len(missing) != 1 {
		t.Fatalf("expected 1 missing_user finding (stage 2 only), got %d: %+v", len(missing), fs)
	}
}

// Override comment must silence the rule and emit one info-level
// override_accepted finding instead.
func TestDockerfile_OverrideAccepted(t *testing.T) {
	src := "# l0git: ignore from_latest reason: dev base\nFROM node:latest\nUSER nobody\nCMD [\"x\"]\n"
	fs := runRules(t, src)
	if findFindingByRule(fs, "from_latest") != nil {
		t.Errorf("override should silence from_latest: %+v", fs)
	}
	found := false
	for _, f := range fs {
		if strings.HasSuffix(f.FilePath, ":override_from_latest") {
			found = true
			if f.Severity != SeverityInfo {
				t.Errorf("override_accepted severity = %q, want info", f.Severity)
			}
			if !strings.Contains(f.Message, "dev base") {
				t.Errorf("audit message must include the reason; got %q", f.Message)
			}
			break
		}
	}
	if !found {
		t.Errorf("expected override_from_latest audit finding, got: %+v", fs)
	}
}

// Override without `reason: …` is still accepted but bumped to warning so
// silent overrides stand out in the Problems pane.
func TestDockerfile_OverrideWithoutReasonIsWarning(t *testing.T) {
	src := "# l0git: ignore from_latest\nFROM node:latest\nUSER nobody\nCMD [\"x\"]\n"
	fs := runRules(t, src)
	for _, f := range fs {
		if strings.HasSuffix(f.FilePath, ":override_from_latest") {
			if f.Severity != SeverityWarning {
				t.Errorf("override-without-reason severity = %q, want warning", f.Severity)
			}
			return
		}
	}
	t.Errorf("expected override_from_latest finding, got: %+v", fs)
}

// Wildcard override silences every rule.
func TestDockerfile_WildcardOverride(t *testing.T) {
	src := "# l0git: ignore * reason: minimal scratch base\nFROM scratch\n"
	fs := runRules(t, src)
	for _, f := range fs {
		if !strings.Contains(f.FilePath, ":override_") {
			t.Errorf("wildcard must silence non-override findings, but got: %+v", f)
		}
	}
}

// suggest_when_missing produces one info finding when no Dockerfile is
// tracked, and nothing otherwise.
func TestDockerfile_SuggestWhenMissing(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)
	// Initial commit so git ls-files doesn't error on a fresh repo.
	mustWrite(t, "/dev/null", "") // no-op, just to exercise the helper
	runGit(t, root, "config", "user.email", "t@t")
	runGit(t, root, "config", "user.name", "t")
	runGit(t, root, "commit", "--allow-empty", "-q", "-m", "init")

	// Default behaviour: silent.
	fs, err := checkDockerfileLint(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 0 {
		t.Errorf("default should be silent on missing Dockerfile, got: %+v", fs)
	}

	// Opt-in: one info finding with the missed-opportunity message.
	opts := []byte(`{"suggest_when_missing": true}`)
	fs2, err := checkDockerfileLint(context.Background(), root, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs2) != 1 || fs2[0].Severity != SeverityInfo {
		t.Errorf("expected 1 info finding, got: %+v", fs2)
	}
}

// disabled_rules silences rules entirely (no override_accepted noise).
func TestDockerfile_DisabledRules(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		"Dockerfile": "FROM node:latest\nUSER nobody\nCMD [\"x\"]\n",
	})
	opts := []byte(`{"disabled_rules": ["from_latest"]}`)
	fs, err := checkDockerfileLint(context.Background(), root, opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fs {
		if strings.Contains(f.FilePath, "from_latest") {
			t.Errorf("disabled rule must not appear (even as override_accepted): %+v", f)
		}
	}
}
