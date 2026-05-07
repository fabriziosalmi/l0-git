package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Gate is a single rule that scans a project root and emits findings.
// Findings are keyed by (project, gate_id, file_path); a gate that wants to
// flag the project as a whole leaves FilePath empty.
type Gate struct {
	ID          string
	Title       string
	Description string
	Severity    string
	Check       func(ctx context.Context, projectRoot string) ([]Finding, error)
}

// gateRegistry returns all built-in gates. Adding a new gate is a one-line
// append here plus its Check implementation below.
func gateRegistry() []Gate {
	return []Gate{
		{
			ID:          "readme_present",
			Title:       "README missing",
			Description: "Project root must contain a README file (README, README.md, README.rst, README.txt).",
			Severity:    SeverityWarning,
			Check:       presenceGate("readme", presenceArgs{names: []string{"readme"}, prefixes: []string{"readme."}, message: "No README file found in the project root. Add a README.md describing what this project is and how to use it."}),
		},
		{
			ID:          "license_present",
			Title:       "LICENSE missing",
			Description: "Project root should declare a license (LICENSE / LICENSE.md / LICENSE.txt / COPYING).",
			Severity:    SeverityWarning,
			Check:       presenceGate("license", presenceArgs{names: []string{"license", "copying", "unlicense"}, prefixes: []string{"license.", "copying."}, message: "No LICENSE file at the project root. Pick a license (e.g. MIT, Apache-2.0) and add it as LICENSE."}),
		},
		{
			ID:          "contributing_present",
			Title:       "CONTRIBUTING missing",
			Description: "A CONTRIBUTING.md helps outside contributors know how to set up and submit changes.",
			Severity:    SeverityInfo,
			Check:       presenceGate("contributing", presenceArgs{names: []string{"contributing"}, prefixes: []string{"contributing."}, message: "No CONTRIBUTING file at the project root. Document how to build, test, and submit PRs."}),
		},
		{
			ID:          "security_present",
			Title:       "SECURITY policy missing",
			Description: "A SECURITY.md tells users how to responsibly disclose vulnerabilities.",
			Severity:    SeverityInfo,
			Check:       presenceGate("security", presenceArgs{names: []string{"security"}, prefixes: []string{"security."}, message: "No SECURITY file at the project root. Add SECURITY.md with a contact and disclosure process."}),
		},
		{
			ID:          "changelog_present",
			Title:       "CHANGELOG missing",
			Description: "A CHANGELOG.md gives users a single place to see what changed between releases.",
			Severity:    SeverityInfo,
			Check:       presenceGate("changelog", presenceArgs{names: []string{"changelog", "changes", "history"}, prefixes: []string{"changelog.", "changes.", "history."}, message: "No CHANGELOG file at the project root. Consider adopting Keep a Changelog and adding CHANGELOG.md."}),
		},
		{
			ID:          "gitignore_present",
			Title:       ".gitignore missing",
			Description: "A .gitignore at the project root prevents accidental commits of build artefacts and secrets.",
			Severity:    SeverityWarning,
			Check:       presenceGate("gitignore", presenceArgs{names: []string{".gitignore"}, message: "No .gitignore at the project root. Add one to keep build artefacts and secrets out of the repo."}),
		},
		{
			ID:          "ci_workflow_present",
			Title:       "CI workflow missing",
			Description: "At least one workflow under .github/workflows/ should exist so builds and tests run on push.",
			Severity:    SeverityWarning,
			Check:       checkCIWorkflow,
		},
		{
			ID:          "pr_template_present",
			Title:       "Pull request template missing",
			Description: "A .github/PULL_REQUEST_TEMPLATE.md (or pull_request_template.md) standardises PR descriptions.",
			Severity:    SeverityInfo,
			Check:       checkPRTemplate,
		},
		{
			ID:          "issue_template_present",
			Title:       "Issue templates missing",
			Description: "At least one .github/ISSUE_TEMPLATE/*.md helps reporters file useful bug reports and requests.",
			Severity:    SeverityInfo,
			Check:       checkIssueTemplates,
		},
		{
			ID:          "secrets_scan",
			Title:       "Secret detected in tracked file",
			Description: "Scans every git-tracked file for AWS/GitHub/OpenAI/Anthropic/Google/Slack/Stripe API keys, JWTs, private-key headers, and tracked .env files. Honours .gitignore via git ls-files.",
			Severity:    SeverityError,
			Check:       checkSecretsScan,
		},
		{
			ID:          "tests_present",
			Title:       "No tests found",
			Description: "Scans the project for common test file/dir conventions (*_test.go, test_*.py, *.test.{ts,js}, *.spec.{ts,js}, *_test.rs, *Test.java, *_spec.rb, conftest.py, tests/ directories).",
			Severity:    SeverityWarning,
			Check:       checkTestsPresent,
		},
	}
}

func gateByID(id string) (Gate, bool) {
	for _, g := range gateRegistry() {
		if g.ID == id {
			return g, true
		}
	}
	return Gate{}, false
}

// CheckResult is what RunChecks returns: the findings written and the gates
// that ran cleanly. Useful for the CLI/MCP response shape.
type CheckResult struct {
	Project     string    `json:"project"`
	GatesRun    []string  `json:"gates_run"`
	GatesIgnored []string `json:"gates_ignored,omitempty"`
	Findings    []Finding `json:"findings"`
	ConfigError string    `json:"config_error,omitempty"`
}

// RunChecks runs every registered gate (or only gateID if non-empty) against
// projectRoot and persists results. Findings the gate no longer reports are
// marked resolved.
func RunChecks(ctx context.Context, store *Store, projectRoot, gateID string) (*CheckResult, error) {
	abs, err := filepath.Abs(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve project root: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("stat project root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("project root is not a directory: %s", abs)
	}

	gates := gateRegistry()
	if gateID != "" {
		g, ok := gateByID(gateID)
		if !ok {
			return nil, fmt.Errorf("unknown gate: %s", gateID)
		}
		gates = []Gate{g}
	}

	cfg, cfgErr := loadProjectConfig(abs)
	if errors.Is(cfgErr, ErrNoConfig) {
		cfg, cfgErr = nil, nil
	}

	out := &CheckResult{Project: abs, GatesRun: []string{}, Findings: []Finding{}}
	if cfgErr != nil {
		// Surface but don't abort — bad config shouldn't take the whole
		// run with it, and the user needs visibility to fix it.
		out.ConfigError = cfgErr.Error()
	}

	for _, g := range gates {
		if cfg.ignored(g.ID) {
			out.GatesIgnored = append(out.GatesIgnored, g.ID)
			// Retire any prior open findings the user just chose to ignore
			// at the project level; otherwise resolved + ignore_in_config
			// would leave stale entries forever.
			if _, err := store.MarkResolved(ctx, abs, g.ID, nil); err != nil {
				return nil, fmt.Errorf("retire ignored gate %s: %w", g.ID, err)
			}
			continue
		}
		out.GatesRun = append(out.GatesRun, g.ID)
		fs, err := g.Check(ctx, abs)
		if err != nil {
			return nil, fmt.Errorf("gate %s: %w", g.ID, err)
		}
		effectiveSeverity := cfg.severityFor(g.ID, g.Severity)
		keep := make([]string, 0, len(fs))
		for _, f := range fs {
			f.Project = abs
			f.GateID = g.ID
			f.Severity = effectiveSeverity
			if f.Title == "" {
				f.Title = g.Title
			}
			saved, err := store.Upsert(ctx, f)
			if err != nil {
				return nil, fmt.Errorf("persist finding for gate %s: %w", g.ID, err)
			}
			out.Findings = append(out.Findings, *saved)
			keep = append(keep, f.FilePath)
		}
		if _, err := store.MarkResolved(ctx, abs, g.ID, keep); err != nil {
			return nil, fmt.Errorf("retire stale findings for gate %s: %w", g.ID, err)
		}
	}
	return out, nil
}

// presenceArgs captures the matching rules for a "this file/category should
// exist at the project root" gate. names matches the lowercased basename
// exactly; prefixes matches with HasPrefix (use the trailing dot, e.g.
// "readme.").
type presenceArgs struct {
	names    []string
	prefixes []string
	message  string
}

func presenceGate(_ string, args presenceArgs) func(context.Context, string) ([]Finding, error) {
	return func(_ context.Context, root string) ([]Finding, error) {
		hit, err := dirContainsFile(root, func(name string) bool {
			for _, n := range args.names {
				if name == n {
					return true
				}
			}
			for _, p := range args.prefixes {
				if strings.HasPrefix(name, p) {
					return true
				}
			}
			return false
		})
		if err != nil {
			return nil, err
		}
		if hit {
			return nil, nil
		}
		return []Finding{{Message: args.message}}, nil
	}
}

// dirContainsFile returns true when dir has at least one regular file whose
// lowercased basename matches predicate. A missing dir is reported as no
// match (no error) — gates use this to detect "directory absent" too.
func dirContainsFile(dir string, predicate func(lowerName string) bool) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if predicate(strings.ToLower(e.Name())) {
			return true, nil
		}
	}
	return false, nil
}

func checkCIWorkflow(_ context.Context, root string) ([]Finding, error) {
	dir := filepath.Join(root, ".github", "workflows")
	hit, err := dirContainsFile(dir, func(name string) bool {
		return strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml")
	})
	if err != nil {
		return nil, err
	}
	if hit {
		return nil, nil
	}
	return []Finding{{
		Message:  "No workflow files found under .github/workflows/. Add a CI workflow (e.g. ci.yml) so tests run on push and pull requests.",
		FilePath: ".github/workflows",
	}}, nil
}

func checkPRTemplate(_ context.Context, root string) ([]Finding, error) {
	githubDir := filepath.Join(root, ".github")
	hit, err := dirContainsFile(githubDir, func(name string) bool {
		return name == "pull_request_template.md"
	})
	if err != nil {
		return nil, err
	}
	if hit {
		return nil, nil
	}
	return []Finding{{
		Message:  "No .github/PULL_REQUEST_TEMPLATE.md. Add one so PR descriptions follow a consistent shape.",
		FilePath: ".github/PULL_REQUEST_TEMPLATE.md",
	}}, nil
}

func checkIssueTemplates(_ context.Context, root string) ([]Finding, error) {
	dir := filepath.Join(root, ".github", "ISSUE_TEMPLATE")
	hit, err := dirContainsFile(dir, func(name string) bool {
		return strings.HasSuffix(name, ".md") || strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml")
	})
	if err != nil {
		return nil, err
	}
	if hit {
		return nil, nil
	}
	return []Finding{{
		Message:  "No .github/ISSUE_TEMPLATE/. Add at least one bug_report.md / feature_request.md template.",
		FilePath: ".github/ISSUE_TEMPLATE",
	}}, nil
}
