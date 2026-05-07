# l0-git

[![ci](https://github.com/fabriziosalmi/l0-git/actions/workflows/ci.yml/badge.svg)](https://github.com/fabriziosalmi/l0-git/actions/workflows/ci.yml)
[![release](https://img.shields.io/github/v/release/fabriziosalmi/l0-git?sort=semver)](https://github.com/fabriziosalmi/l0-git/releases)
[![go.mod](https://img.shields.io/github/go-mod/go-version/fabriziosalmi/l0-git?filename=server%2Fgo.mod)](server/go.mod)
[![license](https://img.shields.io/github/license/fabriziosalmi/l0-git)](LICENSE)

Project-hygiene quality gates for the open workspace. A single Go binary
speaks the [Model Context Protocol](https://modelcontextprotocol.io/) over
stdio and exposes the same SQLite-backed findings store via a small CLI. A
companion VSCode extension watches the workspace, runs the gates, and surfaces
findings in a sidebar tree **and** in the standard *Problems* pane.

- **No Python**, no rule engine, no YAML — just Go and SQLite.
- **One binary, two modes**: `lgit mcp` (stdio MCP server) and
  `lgit <check|list|gates|ignore|delete|clear>` (CLI).
- **Pure Go SQLite** (`modernc.org/sqlite`) — no CGO, builds anywhere.
- **11 built-in gates** — project-hygiene presence checks, multi-language
  test detection, and a high-precision secrets scanner that respects
  `.gitignore` via `git ls-files`.
- **`.l0git.json`** lets a project ignore specific gates or override their
  severity without touching code.
- **Quick fixes**: lightbulb actions in the Problems pane generate stub
  files (LICENSE picker with 6 SPDX choices, README/CHANGELOG/CI workflow
  scaffolds, …).
- **VSCode extension bundles binaries** for darwin/linux/windows so it works
  out-of-the-box from a `.vsix`.
- **Findings persist**: re-running a gate retires findings the rule no longer
  reports (`status = resolved`) and never resurfaces ones you marked
  `ignored`.

## Repository layout

```
server/      Go MCP server + CLI (the `lgit` binary)
extension/   VSCode extension (TreeView + diagnostics + bundled binaries)
```

## Built-in gates

| Gate ID                 | Severity         | What it checks                                                                                              |
|-------------------------|------------------|-------------------------------------------------------------------------------------------------------------|
| `readme_present`        | warning          | A README at the project root.                                                                               |
| `license_present`       | warning          | A LICENSE / COPYING file at the project root.                                                               |
| `gitignore_present`     | warning          | A `.gitignore` at the project root.                                                                         |
| `ci_workflow_present`   | warning          | At least one workflow under `.github/workflows/`.                                                           |
| `tests_present`         | warning / info   | Detects test files across Go, Python, TS/JS, Rust, Java, Kotlin, Ruby (file conventions + `tests/` dirs). Severity is `warning` when a project marker is present (`go.mod`, `package.json`, `pyproject.toml`, `Cargo.toml`, …), `info` otherwise. |
| `secrets_scan`          | error            | Scans every git-tracked file (size ≤ 2 MB, binaries skipped) for AWS / GitHub / OpenAI / Anthropic / Google / Slack / Stripe API keys, JWTs, private-key headers, and tracked `.env` files. Honours `.gitignore` via `git ls-files`. Findings are pinned at `<file>:<line>:<pattern_id>` so each match is its own line in the Problems pane. |
| `contributing_present`  | info             | A CONTRIBUTING file at the project root.                                                                    |
| `security_present`      | info             | A SECURITY policy file at the project root.                                                                 |
| `changelog_present`     | info             | A CHANGELOG file at the project root.                                                                       |
| `pr_template_present`   | info             | `.github/PULL_REQUEST_TEMPLATE.md`.                                                                         |
| `issue_template_present`| info             | At least one file in `.github/ISSUE_TEMPLATE/`.                                                             |

Adding a gate is a one-line append in [`server/gates.go`](server/gates.go).

## Per-project config (`.l0git.json`)

Drop a `.l0git.json` at the project root to opt out of specific gates or
override their severity. The file is optional; unknown fields are rejected
loudly so typos don't silently no-op.

```json
{
  "ignore": ["changelog_present", "pr_template_present"],
  "severity": {
    "readme_present": "info",
    "secrets_scan": "warning"
  }
}
```

- `ignore` — gate IDs that should be skipped. Existing open findings for
  these gates are auto-resolved on the next run.
- `severity` — overrides the default severity. Allowed values: `error`,
  `warning`, `info`.

A malformed config does **not** abort the run; the parse error is returned
in the `config_error` field of the response so you can see it in the
extension's output channel and fix it.

## Install

### Pre-built (recommended)

Grab the latest release from the [releases page](https://github.com/fabriziosalmi/l0-git/releases):

- `lgit-<os>-<arch>.tar.gz` (or `.zip` on Windows) — extract and place `lgit`
  on your `PATH`.
- `l0-git-<version>.vsix` — install with
  `code --install-extension l0-git-<version>.vsix`.

### From source

```sh
make build           # builds server/lgit
make test            # go vet + go test -race
make vsix            # cross-compiles all platform binaries and packages the .vsix
```

The findings DB lives at `~/.l0-git/findings.db`. Override with
`LGIT_DB=/path/to.db`.

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

### Available MCP tools

| Tool              | Args                              | What it does                                                  |
|-------------------|-----------------------------------|---------------------------------------------------------------|
| `gates_check`     | `project`, `gate_id?`             | Run all gates (or one) against a project root, persist results|
| `gates_list`      | —                                 | Inspect the registered gate set                               |
| `findings_list`   | `project?`, `status?`, `limit?`   | Read findings (default: open)                                 |
| `findings_ignore` | `id`                              | Mark a finding ignored so future runs don't resurface it      |
| `findings_delete` | `id`                              | Drop a finding                                                |
| `findings_clear`  | `project`                         | Wipe all findings for a project                               |

## CLI reference

```sh
lgit check <project> [gate_id]   # run gates and persist findings
lgit list  [project] [status]    # status = open|resolved|ignored|all (default: open)
lgit gates                       # list registered gates
lgit ignore <id>
lgit delete <id>
lgit clear  <project>
lgit path                        # prints the SQLite DB path
lgit version
```

## VSCode extension

The extension auto-discovers the `lgit` binary in this order:

1. The path set in `l0-git.binaryPath` (settings).
2. The bundled binary inside the extension (`bin/<goos>-<goarch>/lgit`).
3. The dev layout (`../server/lgit` next to the extension folder, when running
   from this repo).
4. Common system locations: `/usr/local/bin`, `/opt/homebrew/bin`,
   `~/.local/bin`, `~/go/bin`.
5. Whatever `lgit` resolves to on `PATH`.

If the binary is missing, the sidebar offers a one-click action to open the
relevant setting or the output channel.

Findings appear in three places:

- The **l0-git** activity-bar view (icon: shield) — tree of open findings,
  one row per finding with a severity-coloured icon.
- The standard VSCode **Problems** pane (`Ctrl/Cmd+Shift+M`), grouped by file
  with the gate ID as the diagnostic code.
- A **status-bar item** (bottom-left) showing totals: `$(check) l0-git: clean`
  when the workspace is clean, otherwise `$(error|warning|info) l0-git: N`
  with a tooltip breakdown. Clicking it focuses the activity-bar view.

### Quick fixes

Most presence-style gates ship a stub generator. From the *Problems* pane,
click the lightbulb on an l0-git diagnostic and pick **"Generate stub for
&lt;gate_id&gt;"** — the extension writes a sensible scaffold and re-runs the
gate so the finding clears immediately:

| Gate                    | Stub written                                             |
|-------------------------|----------------------------------------------------------|
| `readme_present`        | `README.md` skeleton                                     |
| `license_present`       | Picks one of MIT / Apache-2.0 / BSD-3-Clause / GPL-3.0 / MPL-2.0 / Unlicense, prompts for the copyright holder, writes `LICENSE` |
| `contributing_present`  | `CONTRIBUTING.md` outline                                |
| `security_present`      | `SECURITY.md` reporting policy                           |
| `changelog_present`     | Keep-a-Changelog-style `CHANGELOG.md`                    |
| `gitignore_present`     | `.gitignore` with common OS / dependency / DB patterns   |
| `pr_template_present`   | `.github/PULL_REQUEST_TEMPLATE.md`                       |
| `issue_template_present`| `.github/ISSUE_TEMPLATE/bug_report.md`                   |
| `ci_workflow_present`   | Minimal `.github/workflows/ci.yml` placeholder           |

The extension watches all of the above paths plus `.l0git.json`, so adding
a file (manually or via the quick fix) re-runs the gates without you having
to trigger anything.

### Settings

| Setting                  | Default | Description                                                                    |
|--------------------------|---------|--------------------------------------------------------------------------------|
| `l0-git.binaryPath`      | `""`    | Absolute path to `lgit`. Empty = use the discovery rules above.                |
| `l0-git.dbPath`          | `""`    | Override the SQLite DB path (sets `LGIT_DB`). Empty = `~/.l0-git/findings.db`. |
| `l0-git.notifyOnNew`     | `true`  | Show a toast for each newly-detected finding.                                  |
| `l0-git.runOnStartup`    | `true`  | Run gate checks automatically when the workspace opens.                        |
| `l0-git.autoStartMCP`    | `false` | Spawn the MCP stdio server in the background on activation. Usually unneeded.  |

## Development

```sh
make build        # server/lgit
make test         # go vet + go test -race  (37 functions / 89 sub-tests)
make vsix         # full extension build incl. cross-compiled binaries
make clean
```

CI runs the same checks on Linux/macOS/Windows × Go 1.22 and 1.23.

The SQLite store opens with `journal_mode=WAL` and `busy_timeout=15000` so
concurrent processes (the extension shelling out + a Claude-Code-managed
MCP server hitting the same DB) don't trip `SQLITE_BUSY` under normal use.
The extension additionally serialises every `lgit` invocation and debounces
file-watcher bursts so it never spawns two writers at once.

See [CONTRIBUTING.md](CONTRIBUTING.md) for more.

## License

MIT — see [LICENSE](LICENSE).
