package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// dockerfileLintOptions is the shape of gate_options.dockerfile_lint.
type dockerfileLintOptions struct {
	scanOptions
	// DisabledRules silences specific rule IDs entirely (no findings,
	// not even info). Use this for repo-wide policy choices; per-line
	// silence belongs in `# l0git: ignore` comments.
	DisabledRules []string `json:"disabled_rules,omitempty"`
	// SuggestWhenMissing emits one info finding when no Dockerfile is
	// tracked. Default false — repos that don't ship containers
	// shouldn't see noise.
	SuggestWhenMissing bool `json:"suggest_when_missing,omitempty"`
}

// dockerfileViolation pairs a parsed instruction index with a
// rule-specific message. Severity and rule_id come from the rule itself.
type dockerfileViolation struct {
	instrIdx int
	msg      string
}

type dockerfileRule struct {
	id       string
	severity string
	title    string
	advice   string
	check    func(instrs []dockerfileInstr) []dockerfileViolation
}

var dockerfileRules = []dockerfileRule{
	{
		id:       "from_untagged",
		severity: SeverityWarning,
		title:    "Dockerfile FROM has no tag",
		advice:   "Pin the base image (e.g. `node:20-alpine`) — untagged FROM resolves to whatever moves on the registry, breaking reproducibility.",
		check:    checkFromUntagged,
	},
	{
		id:       "from_latest",
		severity: SeverityWarning,
		title:    "Dockerfile FROM uses :latest",
		advice:   "`:latest` is a moving target. Pin to a real version (or a digest with @sha256:…) so the image you ship today still rebuilds tomorrow.",
		check:    checkFromLatest,
	},
	{
		id:       "add_instruction",
		severity: SeverityInfo,
		title:    "Dockerfile uses ADD",
		advice:   "ADD also extracts tarballs and fetches URLs implicitly. COPY is explicit and predictable; prefer it unless you need the extras (and document why).",
		check:    checkAddInstruction,
	},
	{
		id:       "missing_user",
		severity: SeverityWarning,
		title:    "Dockerfile has no USER directive",
		advice:   "Without a USER directive the container runs as root. Add a non-root USER before ENTRYPOINT/CMD.",
		check:    checkMissingUser,
	},
	{
		id:       "user_root",
		severity: SeverityWarning,
		title:    "Dockerfile USER is root",
		advice:   "Running as root inside the container expands the blast radius of any RCE. Switch to a dedicated non-root user.",
		check:    checkUserRoot,
	},
}

func checkFromUntagged(instrs []dockerfileInstr) []dockerfileViolation {
	out := []dockerfileViolation{}
	for i, ins := range instrs {
		if ins.Kind != "FROM" {
			continue
		}
		image := fromImage(ins.Args)
		// Anything with a digest is pinned by definition; allow.
		if strings.Contains(image, "@sha256:") {
			continue
		}
		// Strip platform prefix variants like "--platform=linux/amd64".
		// fromImage already does that.
		if !strings.Contains(image, ":") {
			out = append(out, dockerfileViolation{
				instrIdx: i,
				msg:      fmt.Sprintf("FROM %s has no tag", image),
			})
		}
	}
	return out
}

func checkFromLatest(instrs []dockerfileInstr) []dockerfileViolation {
	out := []dockerfileViolation{}
	for i, ins := range instrs {
		if ins.Kind != "FROM" {
			continue
		}
		image := fromImage(ins.Args)
		// Take the part after the last colon (tag) up to "@" if present.
		atIdx := strings.Index(image, "@")
		if atIdx >= 0 {
			image = image[:atIdx]
		}
		colonIdx := strings.LastIndex(image, ":")
		if colonIdx < 0 {
			continue
		}
		tag := image[colonIdx+1:]
		if tag == "latest" {
			out = append(out, dockerfileViolation{
				instrIdx: i,
				msg:      fmt.Sprintf("FROM %s pins :latest", fromImage(ins.Args)),
			})
		}
	}
	return out
}

func checkAddInstruction(instrs []dockerfileInstr) []dockerfileViolation {
	out := []dockerfileViolation{}
	for i, ins := range instrs {
		if ins.Kind == "ADD" {
			out = append(out, dockerfileViolation{
				instrIdx: i,
				msg:      "ADD instruction (consider COPY)",
			})
		}
	}
	return out
}

// checkMissingUser fires once per build stage that has an ENTRYPOINT or
// CMD but no preceding USER. A FROM resets the "has USER" state because
// each stage starts fresh.
func checkMissingUser(instrs []dockerfileInstr) []dockerfileViolation {
	out := []dockerfileViolation{}
	hasUser := false
	stageStart := -1
	for i, ins := range instrs {
		switch ins.Kind {
		case "FROM":
			stageStart = i
			hasUser = false
		case "USER":
			hasUser = true
		case "ENTRYPOINT", "CMD":
			if !hasUser && stageStart >= 0 {
				out = append(out, dockerfileViolation{
					instrIdx: i,
					msg:      "ENTRYPOINT/CMD with no preceding USER directive in this stage",
				})
				// Mark hasUser=true so we don't double-emit on
				// subsequent CMD/ENTRYPOINT in the same stage.
				hasUser = true
			}
		}
	}
	return out
}

func checkUserRoot(instrs []dockerfileInstr) []dockerfileViolation {
	out := []dockerfileViolation{}
	for i, ins := range instrs {
		if ins.Kind != "USER" {
			continue
		}
		args := strings.TrimSpace(ins.Args)
		// USER may be "user", "user:group", or "uid[:gid]".
		userPart := args
		if c := strings.Index(args, ":"); c >= 0 {
			userPart = args[:c]
		}
		if userPart == "root" || userPart == "0" {
			out = append(out, dockerfileViolation{
				instrIdx: i,
				msg:      fmt.Sprintf("USER %s sets root", args),
			})
		}
	}
	return out
}

// fromImage extracts the image reference from a FROM args string,
// stripping any leading --platform=… flag and the trailing "AS stage"
// alias. Returns the canonical image:tag@digest substring.
func fromImage(args string) string {
	tokens := strings.Fields(args)
	out := []string{}
	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		if strings.HasPrefix(t, "--platform=") || strings.HasPrefix(t, "--platform ") {
			continue
		}
		if strings.EqualFold(t, "AS") {
			break
		}
		out = append(out, t)
	}
	if len(out) == 0 {
		return ""
	}
	return out[0]
}

// dockerfileBasenames returns true for files we treat as a Dockerfile.
func isDockerfileBasename(name string) bool {
	if name == "Dockerfile" {
		return true
	}
	// Dockerfile.<suffix>, e.g. Dockerfile.dev — common in repos with
	// multiple build flavours.
	if strings.HasPrefix(name, "Dockerfile.") {
		return true
	}
	return false
}

func checkDockerfileLint(ctx context.Context, root string, opts json.RawMessage) ([]Finding, error) {
	options := parseDockerfileOptions(opts)

	// Conditional triggering: silent on repos without Dockerfile, opt-in
	// "missed-opportunity" info finding via suggest_when_missing.
	if !isGitRepo(root) {
		return []Finding{{
			Severity: SeverityInfo,
			Title:    "dockerfile_lint skipped (not a git repository)",
			Message:  "Project root has no .git/. dockerfile_lint scans tracked Dockerfiles only.",
			FilePath: ".git",
		}}, nil
	}
	files, err := gitLsFiles(ctx, root)
	if err != nil {
		return []Finding{{
			Severity: SeverityWarning,
			Title:    "dockerfile_lint failed",
			Message:  fmt.Sprintf("Could not enumerate tracked files: %v", err),
			FilePath: ".git",
		}}, nil
	}

	dockerfiles := []string{}
	for _, rel := range files {
		if options.shouldSkipContent(rel) {
			continue
		}
		if isDockerfileBasename(filepath.Base(rel)) {
			dockerfiles = append(dockerfiles, rel)
		}
	}

	if len(dockerfiles) == 0 {
		if options.SuggestWhenMissing {
			return []Finding{{
				Severity: SeverityInfo,
				Title:    "No Dockerfile detected",
				Message:  "No Dockerfile is tracked in this repo. If you ship a containerised runtime, adding one helps shipping/adoption parity.",
				FilePath: "Dockerfile",
			}}, nil
		}
		return nil, nil
	}

	disabled := map[string]bool{}
	for _, id := range options.DisabledRules {
		disabled[id] = true
	}

	out := []Finding{}
	for _, rel := range dockerfiles {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		instrs := parseDockerfile(string(data))
		out = append(out, evaluateDockerfileRules(rel, instrs, disabled)...)
	}
	return out, nil
}

// evaluateDockerfileRules runs every enabled rule, applies overrides, and
// returns the structured findings. Pulled out for direct unit-testing.
func evaluateDockerfileRules(rel string, instrs []dockerfileInstr, disabled map[string]bool) []Finding {
	out := []Finding{}
	for _, rule := range dockerfileRules {
		if disabled[rule.id] {
			continue
		}
		violations := rule.check(instrs)
		for _, v := range violations {
			instr := instrs[v.instrIdx]
			if instr.Override.matches(rule.id) {
				out = append(out, overrideAcceptedFinding("dockerfile_lint", rel, rule.id, instr))
				continue
			}
			out = append(out, Finding{
				Severity: rule.severity,
				Title:    rule.title,
				Message:  fmt.Sprintf("%s:%d %s. %s", rel, instr.Line, v.msg, rule.advice),
				FilePath: fmt.Sprintf("%s:%d:%s", rel, instr.Line, rule.id),
			})
		}
	}
	return out
}

// overrideAcceptedFinding builds the audit-trail finding emitted when a
// rule was deliberately silenced inline. The reason — when present — is
// the persisted record of *why*; missing reason → severity bumps to
// warning so silent overrides stand out.
func overrideAcceptedFinding(gateID, rel, ruleID string, instr dockerfileInstr) Finding {
	severity := SeverityInfo
	reason := instr.Override.Reason
	reasonText := reason
	if reason == "" {
		severity = SeverityWarning
		reasonText = "(no reason given — please add `reason: …` after the rule id)"
	}
	return Finding{
		Severity: severity,
		Title:    fmt.Sprintf("Override accepted: %s/%s", gateID, ruleID),
		Message: fmt.Sprintf(
			"%s:%d %s rule was overridden inline (directive on line %d). Reason: %s",
			rel, instr.Line, ruleID, instr.Override.Line, reasonText,
		),
		FilePath: fmt.Sprintf("%s:%d:override_%s", rel, instr.Line, ruleID),
	}
}

func parseDockerfileOptions(opts json.RawMessage) dockerfileLintOptions {
	if len(opts) == 0 {
		return dockerfileLintOptions{}
	}
	var o dockerfileLintOptions
	_ = json.Unmarshal(opts, &o)
	return o
}
