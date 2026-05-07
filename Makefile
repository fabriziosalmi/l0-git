# l0-git developer Makefile.
# Convenience targets only — CI does the canonical builds.

VERSION ?= dev
LDFLAGS  := -s -w -X main.Version=$(VERSION)

.PHONY: help build test vet extension-bins extension-compile vsix clean install-mcp

help:
	@echo "Targets:"
	@echo "  build             — build server binary into server/lgit"
	@echo "  test              — go vet + go test (race) for server"
	@echo "  vet               — go vet ./... in server/"
	@echo "  extension-bins    — cross-compile lgit into extension/bin/<os>-<arch>/"
	@echo "  extension-compile — tsc compile of the extension"
	@echo "  vsix              — package the extension (.vsix)"
	@echo "  install-mcp       — register the local lgit with claude code"
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

clean:
	rm -f server/lgit server/lgit.exe
	rm -rf extension/bin
	rm -f extension/*.vsix
