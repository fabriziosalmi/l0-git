---
title: VS Code extension
description: Install the extension, read findings in the sidebar and Problems pane, and apply quick fixes.
---

# VS Code extension

The extension watches the workspace, runs the same gates as the CLI, and puts
the results where you already look: a sidebar tree, the **Problems** pane, a
status-bar item, and an Overview dashboard.

It shells out to the same `lgit` binary and writes to the same SQLite store, so
a finding you ignore in the editor stays ignored in CI.

## Install

From the Marketplace: open the Extensions view (`Ctrl/Cmd+Shift+X`), search for
**l0-git**, install.

From a release artefact:

```sh
code --install-extension l0-git-<version>.vsix
```

## Finding the binary

The extension resolves `lgit` in this order, and stops at the first hit:

1. `l0-git.binaryPath`, if set.
2. The binary bundled inside the extension — `bin/<goos>-<goarch>/lgit`.
3. The dev layout — `../server/lgit`, next to the extension folder.
4. `/usr/local/bin`, `/opt/homebrew/bin`, `~/.local/bin`, `~/go/bin`.
5. Whatever `lgit` resolves to on `PATH`.

If none of them match, the sidebar offers a one-click action to open the
setting or the output channel — it does not fail silently.

## Where findings show up

| Surface | What it gives you |
|---|---|
| **Activity-bar view** | Tree of findings, with grouping, sorting and filtering |
| **Problems pane** | Every open finding as a `vscode.Diagnostic`, keyed by `(file, line)` with `code = gate_id` |
| **Status bar** | `l0-git: clean`, or a per-severity count with a tooltip breakdown |
| **Overview dashboard** | Severity bars, top gates, top files, tag chips, 7-day trend |

### What is visible by default

The sidebar shows **error and warning only**. Info findings — TODO markers, a
missing `CONTRIBUTING.md`, network literals — are hidden until you turn them on
with the severity filter. They are the audit layer, not the work queue.

Toasts fire for **errors only**, and cap at three plus a summary. Warnings and
info never interrupt.

`override_accepted` findings are suppressed from the tree at every severity.
They still land in the store and show up in the dashboard and in
`lgit list -gate=override_accepted`.

## View controls

| Command | What it does |
|---|---|
| `l0-git.runChecks` | Run all gates against the workspace |
| `l0-git.search` | Substring filter across title, message, file and gate |
| `l0-git.setGroupBy` | Group by severity / gate / file / tag / status / none |
| `l0-git.setSortBy` | Sort by updated / created / severity / gate / file |
| `l0-git.setStatusFilter` | Status filter: open / ignored / resolved / all |
| `l0-git.toggleSeverity` | Multi-select which severities are shown |
| `l0-git.showOverview` | Open the dashboard webview |
| `l0-git.clearFilters` | Reset every view filter |

The active state — `12 findings · group: severity · status: ignored` — is shown
in the view's description line, and persists across sessions.

## Quick fixes

Most presence-style gates ship a stub generator. Click the lightbulb on an
l0-git diagnostic in the Problems pane and pick **Generate stub for
&lt;gate_id&gt;**. The extension writes a scaffold and re-runs the gate, so the
finding clears immediately.

| Gate | Stub written |
|---|---|
| `readme_present` | `README.md` skeleton |
| `license_present` | Prompts for MIT / Apache-2.0 / BSD-3-Clause / GPL-3.0 / MPL-2.0 / Unlicense and a holder, writes `LICENSE` |
| `contributing_present` | `CONTRIBUTING.md` outline |
| `security_present` | `SECURITY.md` reporting policy |
| `changelog_present` | Keep-a-Changelog-style `CHANGELOG.md` |
| `gitignore_present` | `.gitignore` with common OS / dependency / DB patterns |
| `pr_template_present` | `.github/PULL_REQUEST_TEMPLATE.md` |
| `issue_template_present` | `.github/ISSUE_TEMPLATE/bug_report.md` |
| `ci_workflow_present` | Minimal `.github/workflows/ci.yml` |
| `branch_protection_declared` | `.github/settings.yml` Probot Settings scaffold |

The extension watches roughly thirty file patterns that the gates use as input,
so adding one of these files re-runs the affected gates without you triggering
anything.

## Remediation recipes

Every finding row has two inline actions beyond ignore and delete:

| Command | What it does |
|---|---|
| `l0-git.showRemediation` | Opens the `lgit fix <id>` output: summary, exact commands, file edits, caveats, verification step |
| `l0-git.copyClaudePrompt` | Copies a structured prompt to the clipboard for Claude Code to act on |

Recipes are deterministic for eight gates — `vendored_dir_tracked`,
`ide_artifact_tracked`, `gitignore_coverage`, `unexpected_executable_bit`,
`env_example_uncommented`, `merge_conflict_markers`, `large_blob_in_history`
and `secrets_scan_history`. For those you get exact commands, safe to
copy-paste. For the rest the recipe is empty and the prompt frames the ask for
an agent instead — rotate this credential first, pick a specific image tag, and
so on.

::: tip The extension never runs the fix
`lgit fix` only prints. Applying is your call, or Claude Code's under its own
permission model.
:::

## Settings

| Setting | Default | Description |
|---|---|---|
| `l0-git.binaryPath` | `""` | Absolute path to `lgit`. Empty uses the discovery order above. |
| `l0-git.dbPath` | `""` | Override the SQLite path (sets `LGIT_DB`). Empty uses `~/.l0-git/findings.db`. |
| `l0-git.notifyOnNew` | `true` | Toast on each new **error**. Warnings and info never toast. |
| `l0-git.runOnStartup` | `true` | Run gate checks when the workspace opens. |
| `l0-git.autoStartMCP` | `false` | Spawn the MCP stdio server on activation. Usually unnecessary — see [Claude Code / MCP](./mcp). |
| `l0-git.showBlame` | `false` | Annotate rows with `git blame` — commit, author, relative time. One git call per affected file. |

## Blame annotation

With `l0-git.showBlame` on, each finding row gets
`<short-sha> · <author> · <relative-time>` from `git blame --line-porcelain`.
One blame call per affected file, fired in parallel. Off by default because the
cost is real on very large repositories.
