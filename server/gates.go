package main

import (
	"context"
	"encoding/json"
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
	// Tags is a comma-separated list applied to every finding the gate
	// emits — useful for grouping gates by theme (security, git-hygiene,
	// release-hygiene, …) in future UIs.
	Tags string
	// Check returns the findings observed in projectRoot. opts carries the
	// per-gate JSON sub-tree from .l0git.json's `gate_options.<gate_id>`,
	// or nil when the user didn't configure anything; gates that don't
	// take options simply ignore it.
	Check func(ctx context.Context, projectRoot string, opts json.RawMessage) ([]Finding, error)
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
			Tags:        "project-hygiene",
			Check:       presenceGate("readme", presenceArgs{names: []string{"readme"}, prefixes: []string{"readme."}, message: "No README file found in the project root. Add a README.md describing what this project is and how to use it."}),
		},
		{
			ID:          "license_present",
			Title:       "LICENSE missing",
			Description: "Project root should declare a license (LICENSE / LICENSE.md / LICENSE.txt / COPYING).",
			Severity:    SeverityWarning,
			Tags:        "project-hygiene",
			Check:       presenceGate("license", presenceArgs{names: []string{"license", "copying", "unlicense"}, prefixes: []string{"license.", "copying."}, message: "No LICENSE file at the project root. Pick a license (e.g. MIT, Apache-2.0) and add it as LICENSE."}),
		},
		{
			ID:          "contributing_present",
			Title:       "CONTRIBUTING missing",
			Description: "A CONTRIBUTING.md helps outside contributors know how to set up and submit changes.",
			Severity:    SeverityInfo,
			Tags:        "project-hygiene",
			Check:       presenceGate("contributing", presenceArgs{names: []string{"contributing"}, prefixes: []string{"contributing."}, message: "No CONTRIBUTING file at the project root. Document how to build, test, and submit PRs."}),
		},
		{
			ID:          "security_present",
			Title:       "SECURITY policy missing",
			Description: "A SECURITY.md tells users how to responsibly disclose vulnerabilities.",
			Severity:    SeverityInfo,
			Tags:        "project-hygiene,security",
			Check:       presenceGate("security", presenceArgs{names: []string{"security"}, prefixes: []string{"security."}, message: "No SECURITY file at the project root. Add SECURITY.md with a contact and disclosure process."}),
		},
		{
			ID:          "changelog_present",
			Title:       "CHANGELOG missing",
			Description: "A CHANGELOG.md gives users a single place to see what changed between releases.",
			Severity:    SeverityInfo,
			Tags:        "project-hygiene,release-hygiene",
			Check:       presenceGate("changelog", presenceArgs{names: []string{"changelog", "changes", "history"}, prefixes: []string{"changelog.", "changes.", "history."}, message: "No CHANGELOG file at the project root. Consider adopting Keep a Changelog and adding CHANGELOG.md."}),
		},
		{
			ID:          "gitignore_present",
			Title:       ".gitignore missing",
			Description: "A .gitignore at the project root prevents accidental commits of build artefacts and secrets.",
			Severity:    SeverityWarning,
			Tags:        "project-hygiene,git-hygiene",
			Check:       presenceGate("gitignore", presenceArgs{names: []string{".gitignore"}, message: "No .gitignore at the project root. Add one to keep build artefacts and secrets out of the repo."}),
		},
		{
			ID:          "ci_workflow_present",
			Title:       "CI workflow missing",
			Description: "At least one workflow under .github/workflows/ should exist so builds and tests run on push.",
			Severity:    SeverityWarning,
			Tags:        "project-hygiene,build",
			Check:       checkCIWorkflow,
		},
		{
			ID:          "pr_template_present",
			Title:       "Pull request template missing",
			Description: "A .github/PULL_REQUEST_TEMPLATE.md (or pull_request_template.md) standardises PR descriptions.",
			Severity:    SeverityInfo,
			Tags:        "project-hygiene",
			Check:       checkPRTemplate,
		},
		{
			ID:          "issue_template_present",
			Title:       "Issue templates missing",
			Description: "At least one .github/ISSUE_TEMPLATE/*.md helps reporters file useful bug reports and requests.",
			Severity:    SeverityInfo,
			Tags:        "project-hygiene",
			Check:       checkIssueTemplates,
		},
		{
			ID:          "secrets_scan",
			Title:       "Secret detected in tracked file",
			Description: "Scans every git-tracked file for AWS/GitHub/OpenAI/Anthropic/Google/Slack/Stripe API keys, JWTs, private-key headers, and tracked .env files. Honours .gitignore via git ls-files.",
			Severity:    SeverityError,
			Tags:        "security,git-hygiene",
			Check:       checkSecretsScan,
		},
		{
			ID:          "tests_present",
			Title:       "No tests found",
			Description: "Scans the project for common test file/dir conventions (*_test.go, test_*.py, *.test.{ts,js}, *.spec.{ts,js}, *_test.rs, *Test.java, *_spec.rb, conftest.py, tests/ directories).",
			Severity:    SeverityWarning,
			Tags:        "quality",
			Check:       checkTestsPresent,
		},
		{
			ID:          "merge_conflict_markers",
			Title:       "Merge conflict markers in tracked file",
			Description: "Detects unresolved git merge conflict markers (<<<<<<<, =======, >>>>>>>) in tracked files. Anything that lands on main with these is a bug.",
			Severity:    SeverityError,
			Tags:        "git-hygiene",
			Check:       checkMergeConflictMarkers,
		},
		{
			ID:          "large_file_tracked",
			Title:       "Large file tracked in git",
			Description: "Flags files in `git ls-files` larger than the configured threshold (default 5 MiB). Tune via gate_options.large_file_tracked.threshold_mb in .l0git.json.",
			Severity:    SeverityWarning,
			Tags:        "git-hygiene",
			Check:       checkLargeFileTracked,
		},
		{
			ID:          "network_scan",
			Title:       "Network address detected",
			Description: "Scans tracked files for IPv4 literals, CIDRs, and ASN references. Public addresses get warning severity, private/loopback/doc ranges get info.",
			Severity:    SeverityInfo,
			Tags:        "security,network",
			Check:       checkNetworkScan,
		},
		{
			ID:          "connection_strings",
			Title:       "Connection string detected",
			Description: "Scans tracked files for connection URIs (legacy schemes like FTP/Telnet/SMB/NFS/rsync, database schemes like MongoDB/Postgres/MySQL/Redis, JDBC, plain HTTP, plain LDAP). URIs with inline credentials are reported as errors.",
			Severity:    SeverityInfo,
			Tags:        "security,network",
			Check:       checkConnectionStrings,
		},
		{
			ID:          "version_drift",
			Title:       "Version mismatch across manifests",
			Description: "Cross-checks declared versions across package.json, Cargo.toml, pyproject.toml, mix.exs, pom.xml, and a top-level VERSION file. Disagreement is a release-hygiene smell.",
			Severity:    SeverityWarning,
			Tags:        "release-hygiene",
			Check:       checkVersionDrift,
		},
		{
			ID:          "dockerfile_lint",
			Title:       "Dockerfile policy violation",
			Description: "Deterministic AST-based lint of tracked Dockerfiles. Fires for: untagged FROM, FROM :latest, ADD instruction, missing USER, USER root. Inline override via `# l0git: ignore <rule_id> reason: …`. Silent on repos without Dockerfile (set gate_options.dockerfile_lint.suggest_when_missing to opt in).",
			Severity:    SeverityWarning,
			Tags:        "containers,security,build",
			Check:       checkDockerfileLint,
		},
		{
			ID:          "compose_lint",
			Title:       "Docker Compose policy violation",
			Description: "Deterministic YAML-AST lint of tracked compose files. Fires for: invalid YAML, privileged services, host networking, /var/run/docker.sock mounts, missing memory limits. Inline override via `# l0git: ignore <rule_id> reason: …`. Silent on repos without a compose file (set gate_options.compose_lint.suggest_when_missing to opt in).",
			Severity:    SeverityWarning,
			Tags:        "containers,security,build",
			Check:       checkComposeLint,
		},
		{
			ID:          "unexpected_executable_bit",
			Title:       "Unexpected executable bit",
			Description: "Flags tracked files with git mode 100755 whose extension/name suggests a text/data file (e.g. README.md tracked as executable).",
			Severity:    SeverityWarning,
			Tags:        "git-hygiene",
			Check:       checkUnexpectedExecutableBit,
		},
		{
			ID:          "vendored_dir_tracked",
			Title:       "Vendored directory tracked",
			Description: "Flags tracked files under well-known vendored directories (node_modules, vendor, target, dist, build, …). One finding per offending top-level directory.",
			Severity:    SeverityWarning,
			Tags:        "git-hygiene",
			Check:       checkVendoredDirTracked,
		},
		{
			ID:          "ide_artifact_tracked",
			Title:       "Editor/IDE artefact tracked",
			Description: "Flags tracked editor/IDE/OS artefacts (.vscode/, .idea/, .DS_Store, Thumbs.db, *.swp, *~, …).",
			Severity:    SeverityWarning,
			Tags:        "git-hygiene",
			Check:       checkIdeArtifactTracked,
		},
		{
			ID:          "filename_quality",
			Title:       "File name quality",
			Description: "Surfaces tracked filenames containing spaces, control chars, or non-ASCII characters — these break unquoted shell pipelines and CI scripts.",
			Severity:    SeverityInfo,
			Tags:        "git-hygiene,quality",
			Check:       checkFilenameQuality,
		},
		{
			ID:          "nvmrc_missing",
			Title:       "Missing .nvmrc / .node-version",
			Description: "Fires when package.json exists but no .nvmrc / .node-version pins the Node runtime. nvm/asdf/Volta users (and CI runners) need this for reproducible toolchains.",
			Severity:    SeverityInfo,
			Tags:        "release-hygiene,quality",
			Check:       checkNvmrcMissing,
		},
		{
			ID:          "html_lint",
			Title:       "HTML accessibility / WCAG violation",
			Description: "Deterministic AST lint of tracked .html/.htm files via golang.org/x/net/html. Fires for: viewport blocking zoom, autoplay video without muted, target=_blank without rel=noopener, icon-only controls without an accessible name, placeholders used as labels, and form reset buttons. Inline override via `<!-- l0git: ignore <rule_id> reason: … -->`. (Note: findings currently pin to file:1 — line-precise pin is queued as Phase B-bis.)",
			Severity:    SeverityWarning,
			Tags:        "accessibility,frontend",
			Check:       checkHtmlLint,
		},
		{
			ID:          "css_lint",
			Title:       "Objective CSS crime",
			Description: "Hand-rolled scan of tracked .css/.scss/.less/.sass/.styl files (skipping .min.css). Fires for: hidden scrollbar (display:none on ::-webkit-scrollbar), thin font-weight (100/200) on body-text selectors, text-align: justify. Inline override via `/* l0git: ignore <rule_id> reason: … */`.",
			Severity:    SeverityWarning,
			Tags:        "frontend,quality",
			Check:       checkCssLint,
		},
		{
			ID:          "gitignore_coverage",
			Title:       "Missing .gitignore entries for the detected stack",
			Description: "Cross-checks .gitignore against a hardcoded `if-stack-then-must-ignore` table: package.json → node_modules, Cargo.toml → target, pyproject.toml/setup.py → __pycache__/.venv, Gemfile → .bundle/vendor/bundle, plus the universal .DS_Store. Silent on repos with no recognised stack markers.",
			Severity:    SeverityWarning,
			Tags:        "git-hygiene",
			Check:       checkGitignoreCoverage,
		},
		{
			ID:          "code_of_conduct_present",
			Title:       "CODE_OF_CONDUCT missing",
			Description: "Looks for CODE_OF_CONDUCT.md at project root, .github/, or docs/. Adopt the Contributor Covenant or similar so contributors know the rules of engagement.",
			Severity:    SeverityInfo,
			Tags:        "project-hygiene,governance",
			Check:       checkCodeOfConductPresent,
		},
		{
			ID:          "codeowners_present",
			Title:       "CODEOWNERS missing",
			Description: "Looks for a CODEOWNERS file at project root, .github/, or docs/. Silent on docs-only repos; fires when the project has source files in a recognised language.",
			Severity:    SeverityInfo,
			Tags:        "governance",
			Check:       checkCodeownersPresent,
		},
		{
			ID:          "env_example_uncommented",
			Title:       "Uncommented .env.example key",
			Description: "For every .env.example / .env.sample / .env.template / .env.dist, every KEY= line must have a `# …` comment either inline or on the line above. A list of bare keys with no context is a broken contract for new contributors.",
			Severity:    SeverityInfo,
			Tags:        "documentation",
			Check:       checkEnvExampleUncommented,
		},
		{
			ID:          "markdown_lint",
			Title:       "Markdown documentation issue",
			Description: "Deterministic AST lint of tracked .md/.markdown files via goldmark. Fires for: image with empty alt, broken local-file link, broken in-document anchor, fenced code block without language tag, and `json`/`yaml` blocks whose payload doesn't parse. Inline override via `<!-- l0git: ignore <rule_id> reason: … -->`. HTTP link liveness is intentionally NOT checked (would require network).",
			Severity:    SeverityWarning,
			Tags:        "documentation,accessibility",
			Check:       checkMarkdownLint,
		},
		{
			ID:          "dead_placeholders",
			Title:       "Unfinished-work placeholder",
			Description: "Scans every tracked text file (≤ 2 MiB, binaries skipped) for TODO:/FIXME:/XXX:/HACK: markers, the phrase \"update this later\", and \"Lorem ipsum\" filler. Severity info — these are intentional signals, but easy to miss before release. Disable individual patterns via gate_options.dead_placeholders.disabled_patterns.",
			Severity:    SeverityInfo,
			Tags:        "documentation,quality",
			Check:       checkDeadPlaceholders,
		},
		{
			ID:          "secrets_scan_history",
			Title:       "Secret in git history",
			Description: "Walks every blob reachable from any ref and scans its content for the same patterns as secrets_scan. Catches secrets that were committed and later removed from the working tree but still live in .git/objects. Opt-in (set gate_options.secrets_scan_history.enabled = true) because the walk is slow on big repos.",
			Severity:    SeverityWarning,
			Tags:        "security,history",
			Check:       checkSecretsScanHistory,
		},
		{
			ID:          "large_blob_in_history",
			Title:       "Large blob in git history",
			Description: "Walks every blob reachable from any ref and reports those above the configured threshold (default 5 MiB). Catches files that bloat .git even after deletion from the working tree. Opt-in (gate_options.large_blob_in_history.enabled = true).",
			Severity:    SeverityWarning,
			Tags:        "git-hygiene,history",
			Check:       checkLargeBlobInHistory,
		},
		{
			ID:          "branch_protection_declared",
			Title:       "Branch protection not declared as code",
			Description: "Verifies the repo tracks branch-protection rules as code via .github/settings.yml (Probot Settings format). Cannot verify the actual GitHub server-side state — that's reachable only via the REST API with auth. Opt-in (gate_options.branch_protection_declared.enabled = true) so users who manage protection via the UI don't get a false signal.",
			Severity:    SeverityInfo,
			Tags:        "governance,security",
			Check:       checkBranchProtectionDeclared,
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

// gateMetadata is the JSON-serialisable view of a Gate. The runtime
// Check func can't go over the wire, and consumers (CLI, MCP, future
// dashboards) only ever need the descriptive fields.
type gateMetadata struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Severity    string `json:"severity"`
	Tags        string `json:"tags"`
}

// gateRegistryMarshallable returns the registered gates as plain data.
// Used by `lgit gates` and the `gates_list` MCP tool — both originally
// tried to JSON-encode Gate values directly, which fails because Check
// is a func.
func gateRegistryMarshallable() []gateMetadata {
	gates := gateRegistry()
	out := make([]gateMetadata, 0, len(gates))
	for _, g := range gates {
		out = append(out, gateMetadata{
			ID:          g.ID,
			Title:       g.Title,
			Description: g.Description,
			Severity:    g.Severity,
			Tags:        g.Tags,
		})
	}
	return out
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
		fs, err := g.Check(ctx, abs, cfg.optionsFor(g.ID))
		if err != nil {
			return nil, fmt.Errorf("gate %s: %w", g.ID, err)
		}
		// Severity precedence:
		//   1. config severity override (forces all findings of this gate)
		//   2. severity the gate set on the finding (tiered scanners)
		//   3. gate's default severity
		override, hasOverride := cfg.severityOverride(g.ID)
		keep := make([]string, 0, len(fs))
		for _, f := range fs {
			f.Project = abs
			f.GateID = g.ID
			switch {
			case hasOverride:
				f.Severity = override
			case f.Severity == "":
				f.Severity = g.Severity
			}
			if f.Title == "" {
				f.Title = g.Title
			}
			if f.Tags == "" {
				f.Tags = g.Tags
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

func presenceGate(_ string, args presenceArgs) func(context.Context, string, json.RawMessage) ([]Finding, error) {
	return func(_ context.Context, root string, _ json.RawMessage) ([]Finding, error) {
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

func checkCIWorkflow(_ context.Context, root string, _ json.RawMessage) ([]Finding, error) {
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

func checkPRTemplate(_ context.Context, root string, _ json.RawMessage) ([]Finding, error) {
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

func checkIssueTemplates(_ context.Context, root string, _ json.RawMessage) ([]Finding, error) {
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
