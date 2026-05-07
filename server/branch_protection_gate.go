package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// branch_protection_declared is a *deterministic* gate over a non-deterministic
// concept. The actual branch-protection state lives on GitHub's servers and
// can only be read via the REST API with auth; from the local filesystem we
// can only verify whether the project tracks protection-as-code.
//
// Detection scope (single canonical convention, by design):
//   - .github/settings.yml in the Probot Settings format
//     (https://github.com/repository-settings/app), with at least one
//     `branches:` entry carrying a `protection:` block.
//
// Other valid mechanisms (terraform/pulumi providers, GitHub native
// rulesets, ad-hoc gh-api scripts) are NOT recognised — projects that
// use them disable this gate via .l0git.json. Severity is `info`,
// gate is opt-in, so the false-negative cost (real protection set via
// UI) only surfaces for users who explicitly enabled the check.

type branchProtectionOptions struct {
	Enabled bool `json:"enabled,omitempty"`
}

const probotSettingsPath = ".github/settings.yml"

func checkBranchProtectionDeclared(_ context.Context, root string, opts json.RawMessage) ([]Finding, error) {
	options := parseBranchProtectionOptions(opts)
	if !options.Enabled {
		return nil, nil
	}
	full := filepath.Join(root, probotSettingsPath)
	data, err := os.ReadFile(full)
	if err != nil {
		if os.IsNotExist(err) {
			return []Finding{noProtectionFinding("no .github/settings.yml tracked")}, nil
		}
		return nil, err
	}
	if !hasBranchesWithProtection(data) {
		return []Finding{noProtectionFinding(".github/settings.yml has no `branches:` entry with a `protection:` block")}, nil
	}
	return nil, nil
}

// hasBranchesWithProtection looks at parsed Probot Settings YAML and
// returns true when the document contains at least one branch entry that
// declares protection. Anything malformed, missing keys, or wrong shape
// counts as "not declared".
func hasBranchesWithProtection(data []byte) bool {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return false
	}
	doc := mappingNode(&root) // shared helper from compose_gate.go
	if doc == nil {
		return false
	}
	branches := mappingValue(doc, "branches")
	if branches == nil || branches.Kind != yaml.SequenceNode {
		return false
	}
	for _, item := range branches.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		if p := mappingValue(item, "protection"); p != nil && p.Kind == yaml.MappingNode {
			return true
		}
	}
	return false
}

func noProtectionFinding(why string) Finding {
	return Finding{
		Severity: SeverityInfo,
		Title:    "Branch protection not declared as code",
		Message: fmt.Sprintf(
			"%s. l0-git can only verify protection-as-code (Probot Settings format) — actual branch-protection rules live server-side on GitHub and aren't readable from the filesystem. If you protect main via the GitHub UI, disable this gate with `\"branch_protection_declared\": false` in your .l0git.json gate_options. Otherwise, install the Settings app (https://github.com/apps/settings) and commit a .github/settings.yml — use the quick-fix to scaffold one.",
			why,
		),
		FilePath: probotSettingsPath,
	}
}

func parseBranchProtectionOptions(opts json.RawMessage) branchProtectionOptions {
	if len(opts) == 0 {
		return branchProtectionOptions{}
	}
	var o branchProtectionOptions
	_ = json.Unmarshal(opts, &o)
	return o
}
