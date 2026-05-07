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
./lgit gates                       # list registered gates
./lgit check /path/to/project      # run all gates, persist findings
./lgit list /path/to/project       # show open findings
./lgit mcp                         # MCP stdio mode (used by Claude Code, etc.)
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

1. Append a `Gate{}` entry to `gateRegistry()` in `server/gates.go`.
2. Implement its `Check` function (returns `[]Finding` for what's wrong, `nil` for clean). For "file at root" rules use the `presenceGate` helper.
3. If a finding has a meaningful file path, set `FilePath` so it ends up in the right Problems-pane group. Project-wide rules leave it empty.
4. Add a smoke test if the rule is non-trivial.

That's it — both the CLI and the MCP server pick up new gates automatically.

## Pull requests

1. Open an issue first if the change is non-trivial — small scope wins.
2. Keep PRs focused: one logical change per PR.
3. Run the same checks CI runs:
   - `make test` — `go vet` + `go test -race ./...` for the server.
   - `cd extension && npm ci && npm run compile` — TypeScript compile.
   - `make vsix` — full extension build with bundled binaries (only required if you change `extension/` or the build script).
4. No new dependencies without a clear reason.

## Code style

- Go: standard `gofmt`. No frameworks; stdlib + `modernc.org/sqlite` only.
- TypeScript: `strict` mode (and the additional checks in `extension/tsconfig.json`). No bundler — `tsc` only.

## Reporting bugs

Use GitHub Issues. Include:
- OS, Go version, Node/VSCode version
- Exact steps to reproduce
- What you expected vs. what happened
