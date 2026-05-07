# l0-git

[![ci](https://github.com/fabriziosalmi/l0-git/actions/workflows/ci.yml/badge.svg)](https://github.com/fabriziosalmi/l0-git/actions/workflows/ci.yml)
[![release](https://img.shields.io/github/v/release/fabriziosalmi/l0-git?sort=semver)](https://github.com/fabriziosalmi/l0-git/releases)
[![go.mod](https://img.shields.io/github/go-mod/go-version/fabriziosalmi/l0-git?filename=server%2Fgo.mod)](server/go.mod)
[![license](https://img.shields.io/badge/license-MIT-green)](LICENSE)

Deterministic project-hygiene quality gates for the open workspace. A single
Go binary speaks the [Model Context Protocol](https://modelcontextprotocol.io/)
over stdio and exposes the same SQLite-backed findings store via a small CLI.
A companion VSCode extension watches the workspace, runs the gates, and
surfaces findings in a sidebar tree, the standard *Problems* pane, a status
bar item, and an Overview dashboard.

The whole thing is built around a single principle: **a gate fires if and
only if the violation can be expressed as a binary, mathematically
unambiguous condition over the file system or an AST**. Heuristics that
need "context understanding" go through a separate LLM-backed path; this
project never blocks on probabilistic signals.

- **Pure Go** — `modernc.org/sqlite`, `golang.org/x/net/html`, `gopkg.in/yaml.v3`,
  `github.com/yuin/goldmark`. No CGO, no Python, no rule engine, no YAML DSL.
- **One binary, two modes**: `lgit mcp` (stdio MCP server) and `lgit <subcmd>` CLI.
- **34 built-in gates** across project hygiene, security (incl. history scanning),
  git hygiene, accessibility, frontend quality, containers, governance,
  documentation, release hygiene.
- **`.l0git.json`** per-project config: ignore gates, override severity,
  pass per-gate options (`exclude_paths`, `skip_default_fixture_paths`,
  `disabled_rules`, thresholds, …) without touching code.
- **Inline overrides** via `# l0git: ignore <rule_id> reason: …` (and the
  YAML / HTML / CSS / Markdown comment variants) — every override emits
  an `override_accepted` audit-trail finding.
- **Quick fixes** in the Problems pane generate stubs (LICENSE picker with
  6 SPDX choices, README/CHANGELOG/CI workflow/Probot Settings, …).
- **Findings persist**: re-running a gate retires findings the rule no
  longer reports (`status = resolved`) and never resurfaces ones the user
  marked `ignored`.

## Status

- 34 gates registered. l0-git scans clean against itself with the
  bundled [`.l0git.json`](.l0git.json) (test fixtures and gate-source
  self-references suppressed).
- 179 PASS, race-clean. CI matrix: Linux / macOS / Windows × Go 1.22 / 1.23.
- Released artefacts at every tag (`v0.1.x`): cross-compiled binaries
  for darwin / linux / windows × amd64 / arm64, plus the bundled `.vsix`.

## Repository layout

```text
server/      Go MCP server + CLI (the `lgit` binary)
extension/   VSCode extension (TreeView + Overview + bundled binaries)
```

## Built-in gates (34)

Grouped by theme. Severity is the *default* — every gate's severity can be
overridden per-project via `.l0git.json`. Tags are CSV strings used for
filtering in the UI and the dashboard.

### Project hygiene + governance (presence-style, 12)

| Gate ID                       | Severity | What it checks                                                       |
|-------------------------------|----------|----------------------------------------------------------------------|
| `readme_present`              | warning  | `README` / `README.*` at the project root                            |
| `license_present`             | warning  | `LICENSE` / `COPYING` / SPDX-named at root                           |
| `contributing_present`        | info     | `CONTRIBUTING.md` at root                                            |
| `security_present`            | info     | `SECURITY.md` at root                                                |
| `changelog_present`           | info     | `CHANGELOG.md` / `CHANGES` / `HISTORY` at root                       |
| `gitignore_present`           | warning  | `.gitignore` at root                                                 |
| `ci_workflow_present`         | warning  | Any workflow under `.github/workflows/`                              |
| `pr_template_present`         | info     | `.github/PULL_REQUEST_TEMPLATE.md`                                   |
| `issue_template_present`      | info     | At least one file in `.github/ISSUE_TEMPLATE/`                       |
| `code_of_conduct_present`     | info     | `CODE_OF_CONDUCT.md` at root, `.github/`, or `docs/`                 |
| `codeowners_present`          | info     | `CODEOWNERS` at root, `.github/`, or `docs/` — silent on docs-only repos |
| `branch_protection_declared`  | info     | **Opt-in.** Verifies `.github/settings.yml` (Probot Settings) declares `branches: [{protection: …}]`. Can't read the server-side GitHub state — that needs an auth'd API call, out of scope |

### Quality + release hygiene (3)

| Gate ID            | Severity        | What it checks                                                              |
|--------------------|-----------------|-----------------------------------------------------------------------------|
| `tests_present`    | warning / info  | Multi-language test detection (Go, Python, TS/JS, Rust, Java, Kotlin, Ruby) |
| `version_drift`    | warning         | Cross-checks declared versions across `package.json`, `Cargo.toml`, `pyproject.toml`, `mix.exs`, `pom.xml`, `VERSION` |
| `nvmrc_missing`    | info            | `package.json` exists but no `.nvmrc` / `.node-version` pins the runtime    |

### Git hygiene (7)

| Gate ID                     | Severity | What it checks                                                                              |
|-----------------------------|----------|---------------------------------------------------------------------------------------------|
| `merge_conflict_markers`    | error    | `<<<<<<<` / `>>>>>>>` / `\|\|\|\|\|\|\|` lines in tracked text files                       |
| `large_file_tracked`        | warning  | Tracked files > 5 MiB (configurable). Honours `.gitattributes` `filter=lfs` markers.        |
| `unexpected_executable_bit` | warning  | Tracked files with mode `100755` whose extension/name says they're text/data                |
| `vendored_dir_tracked`      | warning  | Committed `node_modules/` / `vendor/` / `target/` / `dist/` / … (one finding per dir, not per file) |
| `ide_artifact_tracked`      | warning  | Committed `.vscode/` / `.idea/` / `.DS_Store` / `Thumbs.db` / `*.swp` / `*~`                |
| `filename_quality`          | info     | Filenames with spaces / control chars / non-ASCII chars                                     |
| `gitignore_coverage`        | warning  | Stack-aware: `package.json` → `node_modules`, `Cargo.toml` → `target`, `pyproject.toml` → `__pycache__`+`.venv`, … |

### Security (2 + 2 history)

| Gate ID                  | Severity | What it checks                                                                      |
|--------------------------|----------|-------------------------------------------------------------------------------------|
| `secrets_scan`           | error    | Tracked-file scan for AWS / GitHub / OpenAI / Anthropic / Google / Slack / Stripe API keys, JWTs, PEM-private-key headers, plus tracked `.env` files |
| `connection_strings`     | warning  | URI-style scan: credentials inline (any scheme) → `error`; legacy/cleartext schemes (ftp/telnet/smb/nfs/rsync) → `warning`; DB URIs / cleartext `http://` → `info` |
| `secrets_scan_history`   | warning  | **opt-in** — walks every blob reachable from any ref, reports secrets that survive in `.git/objects` even after working-tree removal (remediation: `git filter-repo`) |
| `large_blob_in_history`  | warning  | **opt-in** — same walk, flags blobs above the threshold (default 5 MiB) so users see what `git filter-repo --strip-blobs-bigger-than NM` would target |

### Network (1)

| Gate ID         | Severity tier              | What it checks                                                                |
|-----------------|----------------------------|-------------------------------------------------------------------------------|
| `network_scan`  | warning (public IP) / info | IPv4 literals, CIDRs, ASN references in tracked files. Public IPv4 → warning; loopback / RFC1918 / RFC 5737 doc ranges → info |

### Containers (2)

| Gate ID            | Severity | What it checks                                                                                       |
|--------------------|----------|------------------------------------------------------------------------------------------------------|
| `dockerfile_lint`  | warning  | AST lint: `from_untagged`, `from_latest`, `add_instruction`, `missing_user`, `user_root`. Inline override `# l0git: ignore <rule> reason: …` |
| `compose_lint`     | warning  | YAML-AST lint: `yaml_invalid`, `privileged_true`, `network_mode_host`, `docker_socket_mount`, `missing_memory_limit`. Inline override via YAML comment |

### Frontend / accessibility (2)

| Gate ID       | Severity | What it checks                                                                                     |
|---------------|----------|----------------------------------------------------------------------------------------------------|
| `html_lint`   | warning  | Tokenizer-based scan of `.html`/`.htm`: `viewport_no_zoom`, `autoplay_with_sound`, `target_blank_no_rel`, `mystery_meat_nav`, `placeholder_as_label`, `reset_button`. Per-line pin via line-tracking. Inline override `<!-- l0git: ignore … -->` |
| `css_lint`    | warning  | Hand-rolled scanner of `.css`/`.scss`/`.less`/`.sass`/`.styl` (skip `.min.css`): `hidden_scrollbar`, `thin_font_weight` (only on body-text selectors), `justified_text`. Inline override `/* l0git: ignore … */` |

### Documentation (3)

| Gate ID                 | Severity | What it checks                                                                                    |
|-------------------------|----------|---------------------------------------------------------------------------------------------------|
| `markdown_lint`         | warning  | goldmark AST lint: `image_no_alt`, `link_local_broken`, `link_anchor_broken`, `codeblock_no_language`, `codeblock_invalid_payload` (parses ` ```json `/` ```yaml ` blocks). HTTP link liveness intentionally NOT checked. |
| `dead_placeholders`     | info     | `TODO:` / `FIXME:` / `XXX:` / `HACK:` / "update this later" / "Lorem ipsum" in tracked text files (word-boundary + colon strict — `package todoist` doesn't trip it) |
| `env_example_uncommented` | info   | Each `KEY=` line in `.env.example` / `.env.sample` / `.env.template` / `.env.dist` must have a `#` comment inline or on the line above |

Adding a gate is a one-line append in [`server/gates.go`](server/gates.go).

## Per-project config (`.l0git.json`)

Optional file at the project root. Unknown fields are rejected loudly so
typos don't silently no-op.

```json
{
  "ignore": ["changelog_present", "pr_template_present"],
  "severity": {
    "readme_present": "info",
    "secrets_scan": "warning"
  },
  "gate_options": {
    "large_file_tracked": { "threshold_mb": 10, "exclude_paths": ["dist/**"] },
    "secrets_scan": { "exclude_paths": ["test/fixtures/**"] },
    "secrets_scan_history": { "enabled": true, "max_blobs": 10000 },
    "large_blob_in_history": { "enabled": true, "threshold_mb": 5 },
    "dead_placeholders": { "disabled_patterns": ["lorem_ipsum"] },
    "dockerfile_lint": { "disabled_rules": ["add_instruction"], "suggest_when_missing": true },
    "compose_lint": { "suggest_when_missing": true },
    "html_lint": { "exclude_paths": ["docs/legacy/**"] },
    "css_lint": { "disabled_rules": ["thin_font_weight"] },
    "gitignore_coverage": { "disabled_patterns": [".DS_Store"] }
  }
}
```

| Field          | Effect                                                                                              |
|----------------|-----------------------------------------------------------------------------------------------------|
| `ignore`       | List of gate IDs to skip entirely. Pre-existing open findings auto-resolve on next run.            |
| `severity`     | Override default severity per gate. Allowed values: `error`, `warning`, `info`.                    |
| `gate_options` | Per-gate JSON sub-tree; schemas are gate-specific (`disabled_rules`, `disabled_patterns`, `exclude_paths`, `threshold_mb`, `suggest_when_missing`, `enabled`, …). |

A malformed config does **not** abort the run; the parse error surfaces in
the response's `config_error` field and in the extension's output channel.

### Inline override directive

For Dockerfile / Compose / HTML / Markdown / CSS, the rule fires unless an
adjacent comment opts out:

```dockerfile
# l0git: ignore from_latest reason: dev base image, never released
FROM node:latest
```

```yaml
services:
  proxy:
    image: traefik:v3
    # l0git: ignore docker_socket_mount reason: traefik label-based routing
    volumes:
      - "/var/run/docker.sock:/var/run/docker.sock:ro"
```

The override produces an info-level `override_accepted` finding so the
deviation lands in the audit trail with the user-supplied reason. An
override with no `reason: …` is bumped to `warning` so silent overrides
stand out.

## VSCode extension

The extension auto-discovers the `lgit` binary in this order:

1. The path set in `l0-git.binaryPath` (settings).
2. The bundled binary inside the extension (`bin/<goos>-<goarch>/lgit`).
3. The dev layout (`../server/lgit` next to the extension folder).
4. Common system locations: `/usr/local/bin`, `/opt/homebrew/bin`, `~/.local/bin`, `~/go/bin`.
5. Whatever `lgit` resolves to on `PATH`.

If the binary is missing, the sidebar offers a one-click action to open the
relevant setting or the output channel.

### Where findings show up

- **Activity-bar view "l0-git"** (icon: shield) — tree of findings with
  configurable group/sort/filter (see below).
- **Problems pane** (`Ctrl/Cmd+Shift+M`) — every open finding becomes a
  `vscode.Diagnostic` keyed by `(file, line)` with `code = gate_id`.
- **Status bar item** (bottom-left) — `$(shield) l0-git: clean` when there
  are zero open findings, otherwise `$(error|warning|info) l0-git: N` with
  a per-severity tooltip breakdown. Click to focus the tree.
- **Overview dashboard** — webview with severity bars, top-gates, top-files,
  tag chips, and a 7-day trend sparkline. Open via the `$(graph)` icon in
  the view title or the command palette ("l0-git: Open Overview dashboard").

### View controls (sidebar)

The findings tree is configurable from the view title bar:

| Icon                    | Command                  | What it does                                                       |
|-------------------------|--------------------------|--------------------------------------------------------------------|
| `$(play)`               | `l0-git.runChecks`       | Run all gates against the workspace                                |
| `$(search)`             | `l0-git.search`          | Substring filter across title / message / file / gate              |
| `$(symbol-namespace)`   | `l0-git.setGroupBy`      | Group by: severity / gate / file / tag / status / none             |
| `$(sort-precedence)`    | `l0-git.setSortBy`       | Sort by: updated / created / severity / gate / file                |
| `$(filter)`             | `l0-git.setStatusFilter` | Status filter: open / ignored / resolved / all                     |
| `$(filter-filled)`      | `l0-git.toggleSeverity`  | Multi-select severity inclusion (error / warning / info)           |
| `$(graph)`              | `l0-git.showOverview`    | Open the dashboard webview                                         |
| `$(clear-all)`          | `l0-git.clearFilters`    | Reset every view filter to defaults                                |

The active state ("12 findings · group: severity · status: ignored") is
shown in the view's description line. View state persists across sessions
in `globalState`.

**Default visibility:** the sidebar shows `error` + `warning` only. `info`
findings are hidden by default — toggle them on via the severity filter
when you want the audit-trail layer (TODO/FIXME, missing CONTRIBUTING.md,
…). `override_accepted` is suppressed from the tree at every severity
level — overrides still land in the DB and surface in the dashboard /
`lgit list -gate=override_accepted`. Toasts fire for `error` only;
warnings and info live exclusively in the sidebar and Problems pane.

### Quick fixes

Most presence-style gates ship a stub generator. From the Problems pane,
click the lightbulb on an l0-git diagnostic and pick **"Generate stub for
&lt;gate_id&gt;"** — the extension writes a sensible scaffold and re-runs the
gate so the finding clears immediately:

| Gate                    | Stub written                                                                                                |
|-------------------------|-------------------------------------------------------------------------------------------------------------|
| `readme_present`        | `README.md` skeleton                                                                                        |
| `license_present`       | Picks one of MIT / Apache-2.0 / BSD-3-Clause / GPL-3.0 / MPL-2.0 / Unlicense, prompts for the holder, writes `LICENSE` |
| `contributing_present`  | `CONTRIBUTING.md` outline                                                                                   |
| `security_present`      | `SECURITY.md` reporting policy                                                                              |
| `changelog_present`     | Keep-a-Changelog-style `CHANGELOG.md`                                                                       |
| `gitignore_present`     | `.gitignore` with common OS / dependency / DB patterns                                                      |
| `pr_template_present`        | `.github/PULL_REQUEST_TEMPLATE.md`                                                                     |
| `issue_template_present`     | `.github/ISSUE_TEMPLATE/bug_report.md`                                                                 |
| `ci_workflow_present`        | Minimal `.github/workflows/ci.yml` placeholder                                                         |
| `branch_protection_declared` | `.github/settings.yml` Probot Settings scaffold — PR review required, no force-push, no deletions     |

The extension watches every file the gates use as input (~30 patterns
covering README/LICENSE/CHANGELOG, `.gitignore`, `.gitattributes`,
`.nvmrc`, `CODEOWNERS`, `CODE_OF_CONDUCT*`, `.env.example*`,
`Dockerfile*`, `docker-compose*.yml`, manifests, `.github/**`), so adding
a file (manually or via the quick fix) re-runs the gates without you
having to trigger anything.

### History scanning (opt-in)

Working-tree gates miss secrets and large blobs that were committed and
later removed — they're still in `.git/objects`. Enable history-aware
gates per project:

```json
{
  "gate_options": {
    "secrets_scan_history": { "enabled": true },
    "large_blob_in_history": { "enabled": true, "threshold_mb": 5 }
  }
}
```

Both walk every reachable blob via `git rev-list --all --objects` +
`git cat-file --batch-check`, dedupe by hash, and report findings keyed by
`history:<short-sha>:<line>:<rule>`. Remediation messages point at
`git filter-repo` (or BFG) — these gates don't pretend the secret can be
"fixed" in-place.

### Blame annotation (opt-in)

Set `l0-git.showBlame: true` to annotate every finding row with
`<short-sha> · <author> · <relative-time>` from `git blame --line-porcelain`.
One blame call per affected file, fired in parallel; off by default to
avoid the cost on very large repos.

### All settings

| Setting                | Default | Description                                                                        |
|------------------------|---------|------------------------------------------------------------------------------------|
| `l0-git.binaryPath`    | `""`    | Absolute path to `lgit`. Empty = use the discovery rules above.                    |
| `l0-git.dbPath`        | `""`    | Override SQLite DB path (sets `LGIT_DB`). Empty = `~/.l0-git/findings.db`.         |
| `l0-git.notifyOnNew`   | `true`  | Show a toast for each newly-detected **error** (cap 3 + summary). Warnings and info never toast — they live in the sidebar / Problems pane only. |
| `l0-git.runOnStartup`  | `true`  | Run gate checks automatically when the workspace opens.                            |
| `l0-git.autoStartMCP`  | `false` | Spawn the MCP stdio server in the background on activation. Usually unneeded.      |
| `l0-git.showBlame`     | `false` | Annotate finding rows with `git blame` info. One git invocation per affected file. |

## Use with Claude Code

Register the local binary as an MCP server:

```sh
make install-mcp
# equivalent to:
# claude mcp add l0-git $(pwd)/server/lgit mcp
```

Or edit `~/.claude.json` manually:

```json
{
  "mcpServers": {
    "l0-git": {
      "command": "/absolute/path/to/lgit",
      "args": ["mcp"]
    }
  }
}
```

### Available MCP tools (7)

| Tool              | Args                                                                          | What it does                                                            |
|-------------------|-------------------------------------------------------------------------------|-------------------------------------------------------------------------|
| `gates_check`     | `project`, `gate_id?`                                                         | Run all gates (or one) against a project root, persist results          |
| `gates_list`      | —                                                                             | Inspect the registered gate set (id, title, description, severity, tags) |
| `findings_list`   | `project?`, `status?`, `severity?`, `gate?`, `tag?`, `query?`, `sort?`, `limit?`, `offset?` | Rich filter + sort + pagination over the findings store      |
| `findings_stats`  | `project?`                                                                    | Aggregate stats: by_severity (open), by_status, by_gate, top_files, by_tag, last_7_days trend |
| `findings_ignore` | `id`                                                                          | Mark a finding ignored so future runs don't resurface it                |
| `findings_delete` | `id`                                                                          | Drop a finding                                                          |
| `findings_clear`  | `project`                                                                     | Wipe all findings for a project                                         |

## CLI reference

```sh
lgit check <project> [gate_id]   # run gates (or one) and persist findings
lgit list  [-project=…] [-status=…] [-severity=…] [-gate=…] [-tag=…]
           [-query=…] [-sort=…] [-limit=N] [-offset=N]
lgit stats [-project=…]          # JSON aggregates for the dashboard
lgit gates                       # list registered gates with metadata
lgit ignore <id>
lgit delete <id>
lgit clear  <project>            # delete every finding for a project
lgit path                        # prints the SQLite DB path
lgit version
```

The DB lives at `~/.l0-git/findings.db`. Override with `LGIT_DB=/path/to.db`.

## Install

### Pre-built (recommended)

Grab the latest release from the [releases page](https://github.com/fabriziosalmi/l0-git/releases):

- `lgit-<os>-<arch>.tar.gz` (or `.zip` on Windows) — extract and place
  `lgit` on your `PATH`.
- `l0-git-<version>.vsix` — install with
  `code --install-extension l0-git-<version>.vsix`.

### From source

```sh
make build           # builds server/lgit
make test            # go vet + go test -race
make vsix            # cross-compiles all platform binaries and packages the .vsix
```

## Development

```sh
make build        # server/lgit
make test         # go vet + go test -race  (179 PASS, race-clean)
make vsix         # full extension build incl. cross-compiled binaries
make clean
```

CI runs the same checks on Linux/macOS/Windows × Go 1.22 and 1.23.

The SQLite store opens with `journal_mode=WAL` and `busy_timeout=15000`
so concurrent processes (the extension shelling out + a Claude-Code-managed
MCP server hitting the same DB) don't trip `SQLITE_BUSY` under normal use.
The extension serialises every `lgit` invocation and debounces file-watcher
bursts so it never spawns two writers at once.

See [CONTRIBUTING.md](CONTRIBUTING.md) for more.

## License

MIT — see [LICENSE](LICENSE).
