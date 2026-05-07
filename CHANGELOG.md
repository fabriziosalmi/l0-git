# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project follows
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- Findings are also published as VSCode `Diagnostic`s — they appear in the
  standard *Problems* pane (`Ctrl/Cmd+Shift+M`) with the gate ID as the
  diagnostic code, alongside the dedicated l0-git TreeView.
- Hygiene gates beyond `readme_present`: `license_present`,
  `contributing_present`, `security_present`, `changelog_present`,
  `gitignore_present`, `ci_workflow_present`, `pr_template_present`,
  `issue_template_present`.
- Root project files: `LICENSE` (MIT), `README.md`, `CONTRIBUTING.md`,
  `SECURITY.md`, `CHANGELOG.md`, `Makefile`.
- `.github/` skeleton: PR template, bug/feature issue templates, CI workflow,
  tag-driven release workflow.

## [0.1.0] - 2026-05-07

- Initial public commit: Go MCP stdio server + SQLite findings store +
  VSCode TreeView UI + first gate (`readme_present`).
