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

| Gate ID                 | Severity | What it checks                                              |
|-------------------------|----------|-------------------------------------------------------------|
| `readme_present`        | warning  | A README at the project root.                               |
| `license_present`       | warning  | A LICENSE / COPYING file at the project root.               |
| `gitignore_present`     | warning  | A `.gitignore` at the project root.                         |
| `ci_workflow_present`   | warning  | At least one workflow under `.github/workflows/`.           |
| `contributing_present`  | info     | A CONTRIBUTING file at the project root.                    |
| `security_present`      | info     | A SECURITY policy file at the project root.                 |
| `changelog_present`     | info     | A CHANGELOG file at the project root.                       |
| `pr_template_present`   | info     | `.github/PULL_REQUEST_TEMPLATE.md`.                         |
| `issue_template_present`| info     | At least one file in `.github/ISSUE_TEMPLATE/`.             |

Adding a gate is a one-line append in [`server/gates.go`](server/gates.go).

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

Findings appear in two places:

- The **l0-git** activity-bar view (icon: shield).
- The standard VSCode **Problems** pane (`Ctrl/Cmd+Shift+M`), grouped by file
  with the gate ID as the diagnostic code.

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
make test         # go vet + go test -race
make vsix         # full extension build incl. cross-compiled binaries
make clean
```

CI runs the same checks on Linux/macOS/Windows × Go 1.22 and 1.23.

See [CONTRIBUTING.md](CONTRIBUTING.md) for more.

## License

MIT — see [LICENSE](LICENSE).
