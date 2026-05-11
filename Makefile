# l0-git developer Makefile.
# Convenience targets only — CI does the canonical builds.

VERSION ?= dev
LDFLAGS  := -s -w -X main.Version=$(VERSION)

.PHONY: help build test vet extension-bins extension-compile vsix clean install-mcp update update-local status release-patch release-minor release-major release-dry

help:
	@echo "Targets:"
	@echo "  build             — build server binary into server/lgit"
	@echo "  test              — go vet + go test (race) for server"
	@echo "  vet               — go vet ./... in server/"
	@echo "  extension-bins    — cross-compile lgit into extension/bin/<os>-<arch>/"
	@echo "  extension-compile — tsc compile of the extension"
	@echo "  vsix              — package the extension (.vsix)"
	@echo "  install-mcp       — register the local lgit with claude code"
	@echo "  update            — pull latest, rebuild, re-register MCP (+ restart hints)"
	@echo "  update-local      — same as 'update' but without git pull"
	@echo "  status            — show binary version and MCP registration state"
	@echo "  release-patch     — bump patch (X.Y.Z+1), tag, push → triggers GH release"
	@echo "  release-minor     — bump minor (X.Y+1.0), tag, push → triggers GH release"
	@echo "  release-major     — bump major (X+1.0.0), tag, push → triggers GH release"
	@echo "  release-dry       — show what 'release-patch' WOULD do (no changes)"
	@echo "  clean             — remove server binary, extension/bin, .vsix files"

build:
	cd server && go build -trimpath -ldflags="$(LDFLAGS)" -o lgit .

vet:
	cd server && go vet ./...

test:
	cd server && go vet ./... && go test -count=1 -race -timeout 90s ./...

extension-bins:
	cd extension && ./scripts/build-bins.sh

extension-compile:
	cd extension && npm ci && npm run compile

vsix: extension-bins extension-compile
	cd extension && rm -f *.vsix && npx --yes @vscode/vsce package

install-mcp: build
	@echo "Registering lgit MCP server with Claude Code…"
	@command -v claude >/dev/null 2>&1 || { echo "claude CLI not found in PATH" >&2; exit 1; }
	claude mcp add l0-git $(CURDIR)/server/lgit mcp

update:
	@bash scripts/update.sh

# make update -- no-pull skips git pull (build from current working tree)
update-local:
	@bash scripts/update.sh --no-pull

status:
	@echo "=== lgit binary ==="
	@if [ -x server/lgit ]; then \
		echo "  path    : $(CURDIR)/server/lgit"; \
		echo "  version : $$(server/lgit version 2>/dev/null || echo '?')"; \
		echo "  built   : $$(stat -f '%Sm' -t '%Y-%m-%d %H:%M' server/lgit 2>/dev/null || stat -c '%y' server/lgit 2>/dev/null | cut -d. -f1)"; \
	else \
		echo "  NOT BUILT — run: make build"; \
	fi
	@echo ""
	@echo "=== running lgit processes ==="
	@pids=$$(pgrep -f "lgit mcp" 2>/dev/null || true); \
	if [ -n "$$pids" ]; then \
		echo "  PIDs: $$pids"; \
		ps -p $$pids -o pid,etime,command 2>/dev/null | sed 's/^/  /' || true; \
	else \
		echo "  none"; \
	fi
	@echo ""
	@echo "=== Claude Code MCP registration ==="
	@if command -v claude >/dev/null 2>&1; then \
		claude mcp list 2>/dev/null | grep -E "l0-git|lgit" | sed 's/^/  /' || echo "  l0-git not registered"; \
	else \
		echo "  claude CLI not found in PATH"; \
	fi

release-patch:
	@bash scripts/release.sh patch

release-minor:
	@bash scripts/release.sh minor

release-major:
	@bash scripts/release.sh major

release-dry:
	@bash scripts/release.sh --dry-run patch

clean:
	rm -f server/lgit server/lgit.exe
	rm -rf extension/bin
	rm -f extension/*.vsix
