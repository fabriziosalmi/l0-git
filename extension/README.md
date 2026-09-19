<p align="center">
  <img src="https://raw.githubusercontent.com/fabriziosalmi/l0-git/main/docs/public/extension/banner.png" alt="l0-git — deterministic quality gates for the open workspace" width="820">
</p>

l0-git checks the repository you have open for things that are simply true or
false about it: a missing license, a tracked `.env`, a `FROM node:latest`, a
leftover merge marker. It puts the answers where you already look: a sidebar
tree, the **Problems** pane, the status bar and an Overview dashboard.

Every finding is deterministic. There are no heuristics or scores, and nothing
is sent over the network. Two runs over the same tree always agree.

**[Documentation](https://fabriziosalmi.github.io/l0-git/)** ·
[Gate reference](https://fabriziosalmi.github.io/l0-git/gates/) ·
[Changelog](https://github.com/fabriziosalmi/l0-git/blob/main/CHANGELOG.md) ·
[Issues](https://github.com/fabriziosalmi/l0-git/issues)

## The Overview dashboard

<img src="https://raw.githubusercontent.com/fabriziosalmi/l0-git/main/docs/public/extension/overview.png" alt="The l0-git Overview dashboard: 19 findings split by severity, top gates, top files, tag chips and a seven-day trend." width="820">

Open it with **l0-git: Open Overview dashboard**. Click a gate or tag to
filter the sidebar to it, or click a file to open it.

## What it checks

The extension runs 36 gates:

| Theme | Gates | Examples |
|---|---|---|
| Project hygiene | 9 | README, LICENSE, SECURITY.md, CI workflow present |
| Git hygiene | 9 | merge-conflict markers, IDE artefacts, vendored dirs, large files, files `.gitignore` excludes but git still tracks |
| Quality & release | 4 | no tests, config files that don't parse, version drift, missing `.nvmrc` |
| Security | 3 | secrets (including a tracked `.env`), credentials in connection strings, hard-coded network literals |
| Documentation | 3 | broken Markdown links, placeholder text, `.env.example` keys with no comment |
| Containers | 2 | Dockerfile and Compose lint: `latest` tags, running as root, containers handed the host |
| Governance | 2 | CODEOWNERS, declared branch protection |
| Frontend & accessibility | 2 | HTML and CSS lint |
| Git history (opt-in) | 2 | secrets and large blobs anywhere in history |

The [gate reference](https://fabriziosalmi.github.io/l0-git/gates/) has one page
per gate, covering what fires it, the severity and how to silence it on purpose.

## Where findings show up

- **Sidebar.** Group findings by severity, gate, file, tag or status. Sort,
  search, and filter by status. Filters persist across sessions.
- **Problems pane.** Each open finding is a diagnostic on its file and line,
  with the gate id as the code.
- **Status bar.** Shows `l0-git: clean`, or a count per severity.
- **Toasts.** Only for new **errors**, at most three plus a summary. Warnings
  and info never interrupt you.

The sidebar shows errors and warnings by default. Info findings, such as TODO
markers or a missing CONTRIBUTING.md, stay hidden until you turn them on with
the severity filter.

## Fixing things

- **Quick fixes.** For ten presence-style gates, the lightbulb in the Problems
  pane writes a stub file: README, LICENSE (you pick the license), SECURITY.md,
  CHANGELOG, `.gitignore`, templates or a CI workflow. The finding then clears
  on its own.
- **Fix recipes.** **Show fix recipe** prints what to do. For eight gates it
  gives the exact commands, which are safe to copy and paste.
- **Ask Claude Code.** Copies a structured prompt for the finding to your
  clipboard.

The extension never runs a fix by itself.

## One store for editor, terminal and agent

The extension runs the bundled `lgit` binary and writes to one SQLite store,
`~/.l0-git/findings.db`. The same binary is a CLI and an MCP server, so a
finding you ignore in the editor stays ignored in CI and in Claude Code.

<img src="https://raw.githubusercontent.com/fabriziosalmi/l0-git/main/docs/public/extension/cli.png" alt="Terminal session: lgit check reports 36 gates and 19 findings, lgit list filters to the errors, and lgit fix prints the exact git commands." width="560">

To give Claude Code the same findings, see
[Claude Code / MCP](https://fabriziosalmi.github.io/l0-git/guide/mcp).

## Settings

| Setting | Default | What it does |
|---|---|---|
| `l0-git.runOnStartup` | `true` | Run the gates when the workspace opens. |
| `l0-git.notifyOnNew` | `true` | Show a toast for each new error. |
| `l0-git.showBlame` | `false` | Add `git blame` (commit, author, age) to each finding. This costs one git call per file. |
| `l0-git.autoStartMCP` | `false` | Start the MCP server on activation. Claude Code normally starts it itself. |
| `l0-git.binaryPath` | `""` | Use a different `lgit` binary. User settings only. |
| `l0-git.dbPath` | `""` | Use a different findings database. User settings only. |

Per-repository behaviour lives in `.l0git.json`, which lets you disable gates,
exclude paths and set gate options. See
[Configuration](https://fabriziosalmi.github.io/l0-git/guide/configuration).

## Requirements and trust

- `git` must be on your `PATH`. The `lgit` binary is bundled for macOS
  (arm64, x64), Linux (x64, arm64) and Windows (x64).
- **Workspace trust is required.** The gates run `git` inside the repository,
  git reads that repository's own configuration, and some git settings name
  programs to run. So the extension stays off in Restricted Mode, as VS Code's
  built-in Git support does.
- `binaryPath` and `dbPath` can only be set in your user settings. A
  repository's `.vscode/settings.json` cannot choose which executable the
  extension runs.

## License

MIT. Source, issues and releases are at
[github.com/fabriziosalmi/l0-git](https://github.com/fabriziosalmi/l0-git).
