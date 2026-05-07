# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project follows
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added — gates (33 total, was 9)

**Project hygiene** — `code_of_conduct_present`, `codeowners_present` (silent
on docs-only repos via language-extension heuristic).

**Quality / release hygiene** — `tests_present` (multi-language test
detection: Go / Python / TS-JSX / Rust / Java / Kotlin / Ruby), `version_drift`
(cross-checks `package.json`, `Cargo.toml`, `pyproject.toml`, `mix.exs`,
`pom.xml`, `VERSION`), `nvmrc_missing`.

**Git hygiene** — `merge_conflict_markers` (line-precise, byte-pattern),
`large_file_tracked` (LFS-aware via `.gitattributes`), `unexpected_executable_bit`
(filters by extension whitelist of "definitely-not-script"), `vendored_dir_tracked`
(one finding per top-level dir), `ide_artifact_tracked` (`.vscode`/`.idea`/
`.DS_Store`/swap files), `filename_quality` (spaces / control / non-ASCII),
`gitignore_coverage` (stack-aware: `package.json` → `node_modules`, `Cargo.toml`
→ `target`, `pyproject.toml` → `__pycache__`+`.venv`, etc.).

**Security** — `secrets_scan` (10 high-precision patterns + `.env` tracked
detection, scoped to `git ls-files`), `connection_strings` (URI-style
detection: credentials inline → `error`; ftp/telnet/smb/nfs/rsync →
`warning`; cleartext `http://` non-local + `ldap://` + DB URIs → `info`),
`network_scan` (IPv4/CIDR/ASN classification: public → `warning`,
private/loopback/RFC-doc → `info`).

**Containers** — `dockerfile_lint` (hand-rolled AST: `from_untagged`,
`from_latest`, `add_instruction`, `missing_user`, `user_root`),
`compose_lint` (`yaml.v3` AST: `yaml_invalid`, `privileged_true`,
`network_mode_host`, `docker_socket_mount`, `missing_memory_limit`).

**Frontend / accessibility** — `html_lint` (`golang.org/x/net/html`
tokenizer with per-line tracking: viewport-blocks-zoom, autoplay-without-muted,
target-`_blank`-without-rel, mystery-meat-nav, placeholder-as-label,
reset-button), `css_lint` (hand-rolled: hidden-scrollbar, thin-font-weight
on body-text, justified-text).

**Documentation** — `markdown_lint` (goldmark AST: `image_no_alt`,
`link_local_broken`, `link_anchor_broken` with GitHub-style slug,
`codeblock_no_language`, `codeblock_invalid_payload` for ` ```json ` /
` ```yaml ` blocks), `dead_placeholders` (TODO/FIXME/XXX/HACK/Lorem ipsum
across tracked text files), `env_example_uncommented` (each `KEY=` line
must have an inline or preceding `#` comment).

**History scanning (opt-in)** — `secrets_scan_history` and
`large_blob_in_history` walk every blob reachable from any ref via
`git rev-list --all --objects` + `git cat-file --batch-check`. Both opt-in
via `gate_options.<gate>.enabled = true` because the walk is slow on big
repos. Findings carry `history:<sha>:…` paths and remediation messages
point at `git filter-repo`.

### Added — extension UI

**Tier 1 (in-tree controls)** — group findings by severity / gate / file /
tag / status / none; sort by updated / created / severity / gate / file;
status filter (open / ignored / resolved / all); per-severity multi-select
toggle; substring search across title/message/file/gate. State persists in
`globalState` across sessions; active state surfaces in `treeView.description`
("12 findings · group: severity · sort: severity · status: open").

**Tier 2 (Overview dashboard)** — webview with severity bars (open-only),
top gates, top files, tag chips (click to filter the tree), 7-day trend
sparkline. Backed by a new `findings_stats` MCP tool / `lgit stats` CLI
that returns aggregations in one round trip. Auto-refreshes after every
`runChecksAndRefresh`.

**CodeAction quick-fixes** — lightbulb action on every finding for a
fixable presence-style gate. LICENSE picker offers MIT / Apache-2.0 /
BSD-3-Clause / GPL-3.0 / MPL-2.0 / Unlicense; other gates write a
canonical scaffold and re-run gates so the diagnostic clears.

**Inline override directive** — `# l0git: ignore <rule_id> reason: …`
(plus YAML / Markdown / HTML / CSS comment variants) silences a single
rule on a single line. Override emits an `override_accepted` info
finding for audit. Missing `reason: …` bumps it to `warning`.

**Status bar item** — bottom-left, severity-aware: `$(check) l0-git: clean`
or `$(error|warning|info) l0-git: N` with tooltip breakdown. Clicks focus
the tree view.

**Diagnostics integration** — every open finding becomes a
`vscode.Diagnostic` keyed by `(file, line)` with `source = l0-git` and
`code = gate_id`. Showed in the Problems pane and on file-gutter icons.

**File watchers extended** — ~30 patterns covering README/LICENSE/CHANGELOG,
`.gitignore`, `.gitattributes`, `.nvmrc`, `CODEOWNERS`, `CODE_OF_CONDUCT*`,
`.env.example*`, `Dockerfile*`, `docker-compose*.yml`, manifests
(`package.json`, `Cargo.toml`, `pyproject.toml`, `mix.exs`, `pom.xml`),
`.github/**`. Adding/editing any input file re-runs gates without manual
refresh.

**Blame annotation** — opt-in via `l0-git.showBlame: true`. After each
fetch, runs `git blame --line-porcelain` per affected file (in parallel)
and appends `<short-sha> · <author> · <relative-time>` to each row's
description and tooltip.

### Added — backend API

**Rich `findings_list`** — both CLI (`lgit list -project=…
-severity=…  -gate=… -tag=… -query=… -sort=… -limit=N -offset=N`) and
MCP (`findings_list { … }`) accept the same filter set. Tag matching is
CSV-aware: `git` does NOT match `git-hygiene`. Sort whitelist:
`updated` / `created` / `severity` (worst-first) / `gate` / `file`.

**`findings_stats`** — new MCP tool / `lgit stats` CLI returning the
aggregate the Overview webview needs in one trip. `by_severity` is
open-only for consistency with the rest of the dashboard; `by_status`
spans every row.

**`gateRegistryMarshallable`** — fixes a long-standing bug where
`lgit gates` and the `gates_list` MCP tool tried to JSON-encode `Gate`
values directly, failing because `Check` is a `func`. The endpoints
now serialise descriptive metadata only (id, title, description,
severity, tags).

**`Tags` field on findings** — gates declare a comma-separated tag set
(`security`, `git-hygiene`, `accessibility`, …) propagated to every
finding they emit. Stored in a new `findings.tags` column with auto-
migration via `PRAGMA table_info` + `ALTER TABLE`.

**Severity precedence rework** — finding severity is now
`config_override > gate_set > gate_default`, so tiered scanners
(`secrets_scan`, `connection_strings`, `network_scan`) keep their per-
match severities unless the user explicitly overrides at the gate
level via `.l0git.json`.

**`gate_options` map in `.l0git.json`** — typed, gate-specific JSON
sub-tree passed to each gate's `Check` function. Schemas include
`disabled_rules`, `disabled_patterns`, `exclude_paths`, `threshold_mb`,
`suggest_when_missing`, `enabled` (history gates).

### Changed

- SQLite store: `busy_timeout` raised from 5 s to 15 s to absorb
  cross-process WAL contention (extension + Claude-Code MCP server).
  Migration runs on every open via `PRAGMA table_info` to add the new
  `tags` column on legacy DBs without rewriting the schema.
- `lgit list` switched from positional args to flag-based for the rich
  filter set. The extension and tests are the only consumers.
- `lgit gates` and MCP `gates_list` now return JSON-safe metadata
  (`gateRegistryMarshallable`) instead of the runtime `Gate` struct.

### Fixed

- Watcher serialisation: `lgitQueue` Promise chain ensures the
  extension never spawns two `lgit` processes against the same SQLite
  DB at once. `runChecksAndRefresh` debounces watcher bursts so a
  multi-file save collapses to a single check pass.
- Overview `By severity` panel previously mixed open + resolved
  counts, contradicting the (open-only) tree below it. Now both are
  open-only; the dashboard explicitly labels the "Total (all statuses)"
  card.
- Webview button label rendered literally as `$(play) Run all checks`
  (codicon syntax isn't expanded inside webview HTML). Replaced with
  a Unicode play arrow.
- `mustWrite` test helper now `os.MkdirAll`s parent dirs so subpath
  fixtures like `.github/CODE_OF_CONDUCT.md` no longer fail before
  the assertion runs.

### Documentation

- README rewritten to document all 33 gates (grouped by theme), the
  `.l0git.json` schema with `gate_options`, the inline override
  directive, the Tier 1 view controls, the Overview dashboard, the
  history-aware gates, the blame annotation setting, the 7 MCP tools,
  and the flag-based CLI.

## [0.1.0] - 2026-05-07

- Initial public commit: Go MCP stdio server + SQLite findings store +
  VSCode TreeView UI + first gate (`readme_present`).
