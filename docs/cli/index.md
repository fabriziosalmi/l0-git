---
title: CLI reference
description: Every lgit subcommand, its arguments, and the shape of what it prints.
---

# CLI reference

One binary, two modes. `lgit mcp` starts the [MCP server](/guide/mcp);
everything else is the CLI documented here.

Every command prints **JSON on stdout**, with one exception: `lgit fix` prints
human-readable text by default and JSON only with `--json`. Errors go to stderr
and set a non-zero exit code.

## Running gates

### `lgit check`

Run gates against a project root and persist the results.

```sh
lgit check <project_root> [gate_id]
```

`project_root` is required and positional. Pass a `gate_id` to run exactly one
gate — useful for confirming a fix without re-scanning everything:

```sh
lgit check . secrets_scan
```

Returns the project path, the list of gates that ran, and the findings:

```json
{
  "project": "/Users/you/src/payments-api",
  "gates_run": ["readme_present", "license_present", "..."],
  "findings": [ ]
}
```

Gates disabled in `.l0git.json` appear under `gates_ignored` instead of
`gates_run`.

### `lgit gates`

The registered gate set — id, title, description, severity, tags. This is the
authoritative list; the [gate reference](/gates/) is generated from it.

```sh
lgit gates
```

## Reading findings

### `lgit list`

```sh
lgit list [-project=…] [-status=…] [-severity=…] [-gate=…] [-tag=…] \
          [-query=…] [-sort=…] [-limit=…] [-offset=…]
```

| Flag | Values |
|---|---|
| `-project` | Absolute project path, as stored by `lgit check` |
| `-status` | `open`, `ignored`, `resolved`, or `all` |
| `-severity` | `error`, `warning`, `info` |
| `-gate` | A gate id |
| `-tag` | A tag, e.g. `security` |
| `-query` | Substring match across title, message and file path |
| `-sort` | `updated`, `created`, `severity`, `gate`, `file` |
| `-limit` / `-offset` | Pagination |

`-project` must be the same absolute path `check` recorded. A relative `.` will
match nothing:

```sh
lgit list -project=$PWD -severity=error
```

Unknown flags and stray positional arguments are rejected with an error rather
than ignored.

### `lgit stats`

Aggregate counts — this is what the VS Code dashboard reads.

```sh
lgit stats -project=$PWD
```

Returns `total`, `by_severity`, `by_status`, `by_gate`, `top_files`, `by_tag`
and a 7-day trend. **Omitting `-project` aggregates across every project in the
store**, which is a useful view in its own right but rarely what you meant.

## Acting on findings

### `lgit fix`

Print the remediation for one finding. Never executes anything.

```sh
lgit fix <finding_id> [--json]
```

Default output is plain text — what to do, the exact commands, file edits,
confidence, a verification step, and a prompt block you can hand to an agent.
`--json` emits the structured form for tooling.

Eight gates carry a `deterministic` recipe with exact commands. The rest return
`guided`: the finding is real, the fix needs judgement, and the output says so
rather than dressing up a restatement of the problem as a solution.

### `lgit ignore`

```sh
lgit ignore <finding_id>
```

Marks the finding ignored so later runs do not resurface it. The row stays in
the store.

### `lgit delete`

```sh
lgit delete <finding_id>
```

### `lgit clear`

```sh
lgit clear <project_root>
```

Removes every finding for that project and prints how many rows went.

## Utilities

### `lgit path`

Print the SQLite store the binary is using.

```sh
$ lgit path
/Users/you/.l0-git/findings.db
```

### `lgit version`

```sh
lgit version
```

`--version` and `-v` are accepted as aliases.

## Flag syntax

Flags that take a value accept all four spellings, and they mean the same thing:

```sh
lgit list -project=/path/to/repo
lgit list --project=/path/to/repo
lgit list -project /path/to/repo
lgit list --project /path/to/repo
```

`--json` on `lgit fix` is a switch and takes no value.

An unknown flag, a missing value, or a stray positional argument is an error.
Nothing is silently ignored — a command that cannot honour what you asked for
says so and exits non-zero, rather than answering a different question.

## Environment

| Variable | Default | Description |
|---|---|---|
| `LGIT_DB` | `~/.l0-git/findings.db` | Path to the SQLite store. The VS Code setting `l0-git.dbPath` sets this. |
