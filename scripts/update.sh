#!/usr/bin/env bash
# scripts/update.sh — pull, rebuild, re-register, and remind you to restart.
#
# Usage:
#   ./scripts/update.sh [options]
#
# Options:
#   -h, --help        Show this help and exit
#   -n, --no-pull     Skip git pull (build from current working tree)
#   -d, --dry-run     Print what would happen without doing it
#   -q, --quiet       Suppress informational output; only print warnings/errors
#   -f, --force       Skip the dirty-tree check and build anyway
#       --no-mcp      Skip Claude Code MCP re-registration
#       --no-restart-hint  Suppress the editor restart reminder
#
# Run from the repo root or any subdirectory — the script always cd's to root.
set -euo pipefail
IFS=$'\n\t'

# ── locate repo root ──────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$REPO_ROOT"

# ── colours ───────────────────────────────────────────────────────────────────
if [ -t 1 ] && [ "${NO_COLOR:-}" = "" ]; then
  RED='\033[0;31]'; GRN='\033[0;32]'; YEL='\033[0;33]'
  CYN='\033[0;36]'; BLD='\033[1m';   DIM='\033[2m'; RST='\033[0m'
else
  RED=''; GRN=''; YEL=''; CYN=''; BLD=''; DIM=''; RST=''
fi

# ── logging helpers ───────────────────────────────────────────────────────────
QUIET=0
DRY_RUN=0

_echo() { [ "$QUIET" -eq 0 ] && echo -e "$*" || true; }
step()  { _echo "${BLD}${CYN}  ▶${RST}${BLD} $*${RST}"; }
ok()    { _echo "${GRN}  ✓${RST} $*"; }
info()  { _echo "${DIM}    $*${RST}"; }
warn()  { echo -e "${YEL}  ⚠${RST}  $*" >&2; }
fatal() { echo -e "${RED}  ✗${RST}  $*" >&2; exit 1; }
dry()   { _echo "${YEL}  ~${RST}  ${DIM}[dry-run]${RST} $*"; }
sep()   { _echo "${DIM}  ────────────────────────────────────────────────${RST}"; }
header(){ _echo ""; _echo "${BLD}$*${RST}"; sep; }

# ── argument parsing ──────────────────────────────────────────────────────────
NO_PULL=0
FORCE=0
NO_MCP=0
NO_RESTART_HINT=0

usage() {
  grep '^#' "$0" | grep -v '#!/' | sed 's/^# \{0,1\}//'
  exit 0
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help)            usage ;;
    -n|--no-pull)         NO_PULL=1 ;;
    -d|--dry-run)         DRY_RUN=1 ;;
    -q|--quiet)           QUIET=1 ;;
    -f|--force)           FORCE=1 ;;
    --no-mcp)             NO_MCP=1 ;;
    --no-restart-hint)    NO_RESTART_HINT=1 ;;
    *) fatal "Unknown option: $1 (try --help)" ;;
  esac
  shift
done

run() {
  # run <desc> <cmd> [args…]  — honours --dry-run
  local desc="$1"; shift
  if [ "$DRY_RUN" -eq 1 ]; then
    dry "$desc"
    dry "  \$ $*"
  else
    "$@"
  fi
}

# ── dependency check ──────────────────────────────────────────────────────────
for cmd in git go; do
  command -v "$cmd" >/dev/null 2>&1 || fatal "'$cmd' not found in PATH — cannot continue"
done

# ── 0. header + current state ─────────────────────────────────────────────────
BINARY="$REPO_ROOT/server/lgit"
CURRENT_VERSION="(not built)"
CURRENT_COMMIT="—"
if [ -x "$BINARY" ]; then
  CURRENT_VERSION="$("$BINARY" version 2>/dev/null || echo "unknown")"
fi
BRANCH="$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo '?')"
CURRENT_COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo '?')"
DIRTY=""
[ -n "$(git status --porcelain 2>/dev/null)" ] && DIRTY=" ${YEL}(dirty)${RST}"

header "l0-git update$([ "$DRY_RUN" -eq 1 ] && echo " ${YEL}[DRY RUN]${RST}" || true)"
_echo "  binary   ${DIM}:${RST} $BINARY"
_echo "  version  ${DIM}:${RST} ${BLD}${CURRENT_VERSION}${RST}"
_echo "  commit   ${DIM}:${RST} $CURRENT_COMMIT${DIRTY}"
_echo "  branch   ${DIM}:${RST} $BRANCH"
_echo ""

# ── dirty-tree warning ────────────────────────────────────────────────────────
if [ -n "$DIRTY" ] && [ "$FORCE" -eq 0 ] && [ "$NO_PULL" -eq 0 ]; then
  warn "Working tree has uncommitted changes — git pull --rebase may fail."
  warn "Use --force to build anyway, or --no-pull to skip pull."
  read -r -p "  Continue anyway? [y/N] " _ans
  [[ "$_ans" =~ ^[Yy]$ ]] || { info "Aborted."; exit 0; }
fi

# ── 1. pull latest ────────────────────────────────────────────────────────────
if [ "$NO_PULL" -eq 0 ]; then
  step "Pulling latest commits from origin/main…"
  if [ "$DRY_RUN" -eq 0 ]; then
    git pull --rebase origin main 2>&1 | sed 's/^/    /'
    NEW_COMMIT="$(git rev-parse --short HEAD)"
    if [ "$NEW_COMMIT" = "$CURRENT_COMMIT" ]; then
      ok "Already at latest ($NEW_COMMIT)"
    else
      ok "Fast-forwarded $CURRENT_COMMIT → $NEW_COMMIT"
    fi
  else
    dry "git pull --rebase origin main"
  fi
else
  warn "Skipping git pull (--no-pull)"
fi

# ── 2. stop running lgit MCP processes ───────────────────────────────────────
step "Stopping running lgit processes…"
PIDS="$(pgrep -f "lgit mcp" 2>/dev/null || true)"
if [ -n "$PIDS" ]; then
  # shellcheck disable=SC2086
  PCT="$(echo $PIDS | wc -w | tr -d ' ')"
  info "Found $PCT process(es): $PIDS"
  if [ "$DRY_RUN" -eq 0 ]; then
    # shellcheck disable=SC2086
    kill $PIDS 2>/dev/null && ok "Stopped $PCT process(es)" \
      || warn "Some processes may have already exited"
    sleep 0.4
  else
    dry "kill $PIDS"
  fi
else
  info "No running lgit mcp process — nothing to stop"
fi

# ── 3. build ──────────────────────────────────────────────────────────────────
step "Building lgit binary…"
NEW_VERSION="$(git describe --tags --always --dirty 2>/dev/null \
               || git rev-parse --short HEAD 2>/dev/null \
               || echo "dev")"
if [ "$DRY_RUN" -eq 0 ]; then
  BUILD_START="$(date +%s)"
  (cd server && go build -trimpath \
      -ldflags="-s -w -X main.Version=${NEW_VERSION}" \
      -o lgit . 2>&1 | sed 's/^/    /')
  BUILD_END="$(date +%s)"
  BUILD_SECS="$(( BUILD_END - BUILD_START ))"
  ok "Built ${BLD}${NEW_VERSION}${RST} in ${BUILD_SECS}s → server/lgit"
else
  dry "cd server && go build … -X main.Version=${NEW_VERSION} -o lgit ."
fi

# ── 4. register MCP with Claude Code ─────────────────────────────────────────
if [ "$NO_MCP" -eq 0 ]; then
  step "Registering MCP server with Claude Code…"
  if command -v claude >/dev/null 2>&1; then
    if [ "$DRY_RUN" -eq 0 ]; then
      claude mcp remove l0-git 2>/dev/null || true
      if claude mcp add l0-git "$BINARY" mcp 2>&1 | sed 's/^/    /'; then
        ok "MCP registered: l0-git → $BINARY"
      else
        warn "claude mcp add failed — verify with: claude mcp list"
      fi
    else
      dry "claude mcp remove l0-git"
      dry "claude mcp add l0-git $BINARY mcp"
    fi
  else
    warn "claude CLI not found in PATH — skipping MCP registration"
    info "Run manually: claude mcp add l0-git $BINARY mcp"
  fi
else
  info "MCP registration skipped (--no-mcp)"
fi

# ── 5. summary ────────────────────────────────────────────────────────────────
_echo ""
sep
_echo ""
_echo "  ${BLD}${GRN}Update complete${RST}"
_echo ""
_echo "  ${DIM}before${RST}  ${CURRENT_VERSION}"
_echo "  ${DIM}after ${RST}  ${BLD}${NEW_VERSION}${RST}"
_echo ""

if [ "$NO_RESTART_HINT" -eq 0 ]; then
  _echo "  ${YEL}${BLD}Restart required to load the new binary:${RST}"
  _echo ""
  _echo "  ${BLD}Claude Code${RST}"
  _echo "  ${DIM}  The MCP process was re-registered but the running session${RST}"
  _echo "  ${DIM}  still holds the old binary. Quit and reopen Claude Code,${RST}"
  _echo "  ${DIM}  or run /restart inside the session.${RST}"
  _echo ""
  _echo "  ${BLD}VSCode${RST}"
  _echo "  ${DIM}  Cmd/Ctrl+Shift+P → Developer: Reload Window${RST}"
  _echo "  ${DIM}  Then: Cmd/Ctrl+Shift+P → l0-git: Run checks${RST}"
  _echo ""
fi

sep
_echo ""
