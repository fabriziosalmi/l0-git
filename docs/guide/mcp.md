---
title: Claude Code / MCP
description: Register lgit as an MCP server and let an agent read and act on the same findings store.
---

# Claude Code / MCP

`lgit mcp` speaks the [Model Context Protocol](https://modelcontextprotocol.io/)
over stdio. Registering it gives an agent read and write access to the same
findings store the CLI and the VS Code extension use — so an agent can run the
gates, read what came back, and ask for a remediation recipe without you
copying anything between windows.

## Register the server

```sh
make install-mcp
```

That is a thin wrapper around:

```sh
claude mcp add l0-git /absolute/path/to/lgit mcp
```

Or write it into `~/.claude.json` yourself:

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

Any MCP client works — there is nothing Claude-specific in the protocol
surface. Check the registration with:

```sh
make status
```

## Tools

| Tool | Arguments | What it does |
|---|---|---|
| `gates_check` | `project`, `gate_id?` | Run all gates, or one, against a project root and persist the results |
| `gates_list` | — | The registered gate set: id, title, description, severity, tags |
| `findings_list` | `project?`, `status?`, `severity?`, `gate?`, `tag?`, `query?`, `sort?`, `limit?`, `offset?` | Filter, sort and paginate the findings store |
| `findings_stats` | `project?` | `by_severity`, `by_status`, `by_gate`, `top_files`, `by_tag`, and a 7-day trend |
| `findings_ignore` | `id` | Mark a finding ignored so later runs do not resurface it |
| `findings_delete` | `id` | Drop a single finding |
| `findings_clear` | `project` | Wipe every finding for a project |
| `findings_remediate` | `id` | The structured remediation for one finding |

## What `findings_remediate` returns

This is the tool worth understanding, because it is the one that decides how
much the agent should trust itself.

- `summary` — what needs to happen, in one line.
- `confidence` — `deterministic` or `guided`.
- `recipe` — for the eight deterministic gates: exact commands, exact file
  edits, and the caveats. Empty for everything else.
- `claude_prompt` — a self-contained prompt framing the fix, always present.

`deterministic` means the fix is mechanical and the recipe can be applied as
written. `guided` means the gate found something real but the fix needs
judgement — which image tag to pin, whether a credential is live, whether a
placeholder is still wanted.

::: warning The server never executes anything
`findings_remediate` returns text. Applying it goes through the agent's own
tools and its own permission prompts. l0-git deliberately has no write path
into your working tree.
:::

## A typical loop

1. `gates_check` with the project root.
2. `findings_list` filtered to `severity=error`.
3. `findings_remediate` on each id.
4. The agent applies what it can and asks about the rest.
5. `gates_check` again to confirm the findings resolved.

Step 5 matters: findings are keyed by `(project, gate_id, file_path)`, so a
re-run flips a fixed finding to `resolved` rather than leaving a stale row
behind.

## Running the server from the extension

The VS Code extension can spawn `lgit mcp` for you — command
**l0-git: Start MCP server (manual)**, or the `l0-git.autoStartMCP` setting.

You usually do not want this. Claude Code spawns its own copy of the server
from the registration above, and a second process against the same SQLite file
buys you nothing. The setting exists for clients that expect to attach to an
already-running server.
