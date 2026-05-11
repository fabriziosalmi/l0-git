# Model Context Protocol (MCP)

l0-git supports the [Model Context Protocol](https://modelcontextprotocol.io/), allowing it to act as an MCP server. This enables seamless integration with LLM agents like Claude Code, providing them with "eyes" on your repository's hygiene.

## Usage

To start l0-git in MCP mode:

```bash
lgit mcp
```

## Tools

l0-git exposes several tools to MCP clients:

| Tool | Description |
|------|-------------|
| `gates_check` | Run all gates (or one) and persist results. |
| `gates_list` | Inspect the registered gate set. |
| `findings_list` | Rich filter + sort + pagination over findings. |
| `findings_stats` | Aggregate stats (severity, status, top files). |
| `findings_ignore` | Mark a finding as ignored. |
| `findings_delete` | Drop a finding. |
| `findings_remediate` | Return a structured remediation recipe. |

## Integration with Claude Code

Add l0-git to your `~/.claude.json` (or use `claude mcp add`):

```json
{
  "mcpServers": {
    "l0-git": {
      "command": "/usr/local/bin/lgit",
      "args": ["mcp"]
    }
  }
}
```

Once registered, Claude can automatically run checks and suggest fixes for hygiene violations.
