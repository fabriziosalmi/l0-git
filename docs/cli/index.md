# CLI Reference

The `lgit` binary is the heart of l0-git. It provides a set of commands for running checks, listing findings, and managing the project's hygiene state.

## Commands

### `lgit check`

Run gates against a project and persist results to the database.

```bash
lgit check <project_path> [gate_id]
```

- `<project_path>`: The root directory of the project to scan.
- `[gate_id]`: Optional. Run only a specific gate.

### `lgit list`

List findings from the database with rich filtering options.

```bash
lgit list [-project=...] [-status=...] [-severity=...] [-gate=...] [-tag=...]
```

### `lgit stats`

Get aggregated statistics for a project in JSON format. Used by the VS Code dashboard.

```bash
lgit stats [-project=...]
```

### `lgit gates`

List all registered gates and their metadata.

```bash
lgit gates
```

### `lgit fix`

Print a remediation recipe for a specific finding.

```bash
lgit fix <finding_id> [--json]
```

### `lgit ignore`

Mark a specific finding as ignored.

```bash
lgit ignore <finding_id>
```

### `lgit delete`

Delete a specific finding from the database.

```bash
lgit delete <finding_id>
```

### `lgit clear`

Wipe all findings for a specific project.

```bash
lgit clear <project_path>
```

## Environment Variables

| Variable | Description |
|----------|-------------|
| `LGIT_DB` | Override the default SQLite database path (default: `~/.l0-git/findings.db`). |
