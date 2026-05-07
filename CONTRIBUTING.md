# Contributing to l0-git

Thanks for your interest. l0-git is intentionally small — keep contributions in that spirit.

## Project layout

- `server/` — Go binary (`lgit`). Acts as both an MCP stdio server and a CLI for the same SQLite findings store.
- `extension/` — VSCode extension. Watches the workspace, runs gates, surfaces findings in a TreeView and the Problems pane. Shells out to the `lgit` binary.

## Development

### Server

```sh
cd server
go build -o lgit .
./lgit gates                                 # list registered gates
./lgit check /path/to/project                # run all gates, persist findings
./lgit list -project=/path/to/project        # show open findings (flag-based)
./lgit list -project=/path/to/project -severity=warning -sort=severity
./lgit stats -project=/path/to/project       # JSON aggregates for the dashboard
./lgit mcp                                   # MCP stdio mode (used by Claude Code, etc.)
```

The DB lives at `~/.l0-git/findings.db` by default. Override with `LGIT_DB=/path/to/file.db`.

### Extension

```sh
cd extension
npm install
npm run compile
```

Open `extension/` in VSCode and press `F5` to launch the Extension Development Host.

## Adding a new gate

1. Append a `Gate{}` entry to `gateRegistry()` in `server/gates.go` with
   ID, title, description, default severity, comma-separated `Tags`, and
   the `Check` function reference.
2. Implement `Check(ctx, projectRoot, opts json.RawMessage) ([]Finding, error)`
   in a new file (e.g. `server/<gate_id>_gate.go`). Return `nil` for
   clean, a `[]Finding` for violations. For "file at root" rules use the
   `presenceGate` helper; for tracked-file scans use `gitLsFiles`; for
   git history walks use `enumerateHistoryBlobs`.
3. Set `Finding.FilePath` to a meaningful value:
   - `"<rel>:<line>:<rule_id>"` for scan-style gates (one finding per
     match per file per line); the unique constraint then dedupes
     correctly across re-runs.
   - just `"<rel>"` for file-level findings.
   - empty for project-level findings.
   - `"history:<short_sha>:<line>:<rule_id>"` for history gates.
4. If the gate has tunables (thresholds, exclude lists, opt-in flag),
   define a typed options struct embedding `scanOptions`, parse it from
   the `opts` parameter, and document the schema in the README.
5. If the gate emits a tiered set of severities (like `secrets_scan`),
   set each `Finding.Severity` directly — the runner respects it unless
   the user pins the gate's severity in `.l0git.json`.
6. Add a test file `server/<gate_id>_gate_test.go`. Table-driven where
   it fits (positive + negative cases). For history gates,
   `commitAndRemove` in `git_history_test.go` is a useful pattern.
7. Run `make test` — both vet and `-race` must stay clean.

The CLI, the MCP server, and the extension's tree all pick up new gates
automatically via `gateRegistry()`.

## Pull requests

1. Open an issue first if the change is non-trivial — small scope wins.
2. Keep PRs focused: one logical change per PR.
3. Run the same checks CI runs:
   - `make test` — `go vet` + `go test -race ./...` for the server.
   - `cd extension && npm ci && npm run compile` — TypeScript compile.
   - `make vsix` — full extension build with bundled binaries (only required if you change `extension/` or the build script).
4. No new dependencies without a clear reason.

## Code style

- Go: standard `gofmt`. No frameworks. Direct deps are deliberately small:
  `modernc.org/sqlite` (pure-Go SQLite, no CGO), `golang.org/x/net/html`
  (HTML tokenizer), `gopkg.in/yaml.v3` (YAML AST for compose linting),
  `github.com/yuin/goldmark` (Markdown AST for `markdown_lint`). New deps
  need a justification and ideally MIT/BSD/Apache-2.0 licence.
- TypeScript: `strict` mode (and the additional checks in
  `extension/tsconfig.json`). No bundler — `tsc` only.

## Reporting bugs

Use GitHub Issues. Include:
- OS, Go version, Node/VSCode version
- Exact steps to reproduce
- What you expected vs. what happened
