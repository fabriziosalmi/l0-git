# Built-in Gates

l0-git comes with 34 built-in gates organized by theme. Each gate is designed to be deterministic and fast.

## Project Hygiene & Governance

| ID | Severity | Description |
|----|----------|-------------|
| `readme_present` | Warning | Ensures a README file exists at the project root. |
| `license_present` | Warning | Ensures a LICENSE file exists at the project root. |
| `contributing_present` | Info | Ensures a CONTRIBUTING.md file exists at the project root. |
| `security_present` | Info | Ensures a SECURITY.md file exists at the project root. |
| `changelog_present` | Info | Ensures a CHANGELOG.md file exists at the project root. |
| `code_of_conduct_present` | Info | Ensures a CODE_OF_CONDUCT.md file exists. |
| `codeowners_present` | Info | Ensures a CODEOWNERS file exists. |
| `pr_template_present` | Info | Ensures a Pull Request template exists. |
| `issue_template_present` | Info | Ensures Issue templates exist. |
| `ci_workflow_present` | Warning | Ensures at least one CI workflow exists. |
| `branch_protection_declared` | Info | Verifies branch protection is declared as code. |

## Git & Repository Hygiene

| ID | Severity | Description |
|----|----------|-------------|
| `gitignore_present` | Warning | Ensures a .gitignore file exists at the project root. |
| `gitignore_coverage` | Warning | Cross-checks .gitignore against the detected stack. |
| `merge_conflict_markers` | Error | Detects unresolved merge conflict markers. |
| `large_file_tracked` | Warning | Flags tracked files larger than a threshold (default 5MB). |
| `ide_artifact_tracked` | Warning | Flags tracked editor/IDE/OS artifacts. |
| `vendored_dir_tracked` | Warning | Flags tracked vendored directories (e.g., node_modules). |
| `unexpected_executable_bit` | Warning | Flags files with unexpected executable bits. |
| `filename_quality` | Info | Flags filenames with spaces or non-ASCII characters. |

## Security

| ID | Severity | Description |
|----|----------|-------------|
| `secrets_scan` | Error | Scans tracked files for hardcoded secrets. |
| `connection_strings` | Info | Scans for connection URIs with inline credentials. |
| `network_scan` | Info | Scans for public IPv4 literals and CIDRs. |
| `secrets_scan_history` | Warning | Scans git history for secrets (Opt-in). |
| `large_blob_in_history` | Warning | Flags large blobs in git history (Opt-in). |

## Quality & Release

| ID | Severity | Description |
|----|----------|-------------|
| `tests_present` | Warning | Detects the presence of tests for various languages. |
| `version_drift` | Warning | Cross-checks versions across different manifests. |
| `nvmrc_missing` | Info | Ensures .nvmrc or .node-version exists for Node projects. |

## Specialized Lints

| ID | Severity | Description |
|----|----------|-------------|
| `dockerfile_lint` | Warning | AST-based lint for Dockerfiles. |
| `compose_lint` | Warning | YAML-AST lint for Docker Compose files. |
| `html_lint` | Warning | Accessibility and WCAG lint for HTML files. |
| `css_lint` | Warning | Objective quality lint for CSS/SCSS/LESS files. |
| `markdown_lint` | Warning | AST lint for Markdown documentation. |

## Documentation

| ID | Severity | Description |
|----|----------|-------------|
| `dead_placeholders` | Info | Scans for TODO, FIXME, and placeholder text. |
| `env_example_uncommented` | Info | Ensures .env.example keys have descriptive comments. |
