#!/usr/bin/env bash
# update.sh — rebuild lgit, re-register the MCP server, and remind you
# which editors need a restart to pick up the new binary.
#
# Usage: ./scripts/update.sh [--no-pull]
#   --no-pull   skip `git pull` (useful when you just built from a local branch)
#
# Run from the repo root or any subdirectory; the script cd's to the root.
set -euo pipefail

# ── locate repo root ──────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$REPO_ROOT"

# ── colours (no-op when not a terminal) ───────────────────────────────────────
if [ -t 1 ]; then
  RED='\033[0;31m'; YEL='\033[0;33m'; GRN='\033[0;32m'
  BLD='\033[1m'; DIM='\033[2m'; RST='\033[0m'
else
  RED=''; YEL=''; GRN=''; BLD=''; DIM=''; RST=''
fi

step()  { echo -e "${BLD}▶ $*${RST}"; }
ok()    { echo -e "${GRN}✓ $*${RST}"; }
warn()  { echo -e "${YEL}⚠ $*${RST}"; }
fatal() { echo -e "${RED}✗ $*${RST}" >&2; exit 1; }
sep()   { echo -e "${DIM}────────────────────────────────────────────${RST}"; }

NO_PULL=0
for arg in "$@"; do
  [[ "$arg" == "--no-pull" ]] && NO_PULL=1
done

sep
echo -e "${BLD}l0-git update${RST}"
sep

# ── 0. current state ──────────────────────────────────────────────────────────
BINARY="$REPO_ROOT/server/lgit"
CURRENT_VERSION="(not built)"
if [ -x "$BINARY" ]; then
  CURRENT_VERSION="$("$BINARY" version 2>/dev/null || echo "unknown")"
fi
echo "  binary   : $BINARY"
echo "  version  : $CURRENT_VERSION"
echo "  branch   : $(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo '?')"
echo ""

# ── 1. pull latest ───────────────────────────────────────────────────────────
if [ "$NO_PULL" -eq 0 ]; then
  step "Pulling latest commits…"
  git pull --rebase 2>&1 | sed 's/^/  /'
  ok "Up to date"
else
  warn "Skipping git pull (--no-pull)"
fi

# ── 2. kill any running lgit MCP processes ────────────────────────────────────
step "Stopping running lgit processes…"
PIDS=$(pgrep -f "lgit mcp" 2>/dev/null || true)
if [ -n "$PIDS" ]; then
  echo "  Found PIDs: $PIDS"
  # shellcheck disable=SC2086
  kill $PIDS 2>/dev/null && ok "Stopped" || warn "Could not stop some processes (may have already exited)"
  sleep 0.5
else
  echo "  No running lgit mcp process found — nothing to stop"
fi

# ── 3. build ─────────────────────────────────────────────────────────────────
step "Building lgit binary…"
NEW_VERSION="$(git describe --tags --always --dirty 2>/dev/null || git rev-parse --short HEAD)"
(cd server && go build -trimpath -ldflags="-s -w -X main.Version=${NEW_VERSION}" -o lgit . 2>&1 | sed 's/^/  /')
ok "Built lgit ${NEW_VERSION} → server/lgit"

# ── 4. register MCP with Claude Code ─────────────────────────────────────────
step "Registering MCP server with Claude Code…"
if command -v claude >/dev/null 2>&1; then
  # Remove stale registration (ignore error if not registered).
  claude mcp remove l0-git 2>/dev/null || true
  if claude mcp add l0-git "$BINARY" mcp 2>&1 | sed 's/^/  /'; then
    ok "MCP server registered: l0-git → $BINARY"
  else
    warn "claude mcp add failed — check 'claude mcp list'"
  fi
else
  warn "claude CLI not found in PATH — skipping MCP registration"
  warn "Run manually: claude mcp add l0-git $BINARY mcp"
fi

# ── 5. summary + restart warnings ────────────────────────────────────────────
sep
echo ""
echo -e "${BLD}Update complete${RST}"
echo "  old version : $CURRENT_VERSION"
echo "  new version : $NEW_VERSION"
echo ""
echo -e "${YEL}${BLD}⚠  Restart required in the following tools to load the new binary:${RST}"
echo ""
echo -e "  ${BLD}Claude Code${RST}"
echo -e "    The MCP server is launched once per session by the Claude Code"
echo -e "    desktop app or CLI. The running process still points at the"
echo -e "    old binary. ${YEL}Quit and reopen Claude Code (or reload the window).${RST}"
echo ""
echo -e "  ${BLD}VSCode — l0-git extension${RST}"
echo -e "    The extension spawns lgit on activation. Already-open windows"
echo -e "    are still using the old process. ${YEL}Run the command palette →${RST}"
echo -e "    ${YEL}'Developer: Reload Window' (Cmd/Ctrl+Shift+P)${RST}${YEL} in every VSCode window,${RST}"
echo -e "    ${YEL}or quit and reopen VSCode.${RST}"
echo ""
echo -e "  ${DIM}(If the extension shows stale findings after reload, run${RST}"
echo -e "  ${DIM}'l0-git: Run checks' from the command palette.)${RST}"
echo ""
sep
