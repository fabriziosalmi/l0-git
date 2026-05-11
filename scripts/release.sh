#!/usr/bin/env bash
# scripts/release.sh — one-command release flow for l0-git.
#
# Usage:
#   ./scripts/release.sh patch        # 0.1.13 -> 0.1.14
#   ./scripts/release.sh minor        # 0.1.13 -> 0.2.0
#   ./scripts/release.sh major        # 0.1.13 -> 1.0.0
#   ./scripts/release.sh 0.2.5        # explicit version
#
# Options:
#   -h, --help        Show this help and exit
#   -n, --dry-run     Print what would happen without doing it
#   -y, --yes         Skip the final confirmation prompt
#
# What it does (in order):
#   1. Verify clean working tree on `main` and up-to-date with `origin/main`
#   2. Compute the new version from current `extension/package.json`
#   3. Bump `extension/package.json` and `extension/package-lock.json`
#   4. Rotate the CHANGELOG `[Unreleased]` section to `[X.Y.Z] - YYYY-MM-DD`
#      (only if a non-empty `[Unreleased]` section exists — otherwise inserts a
#      bare new section so the format stays consistent)
#   5. Commit `chore(release): vX.Y.Z`
#   6. Create an annotated tag `vX.Y.Z`
#   7. Push `main` and the tag — triggers `.github/workflows/release.yml`
#      which publishes the GitHub Release with cross-arch binaries + .vsix.
#
# Designed to be safe: every destructive step (commit, tag, push) is gated by a
# preceding sanity check, and the whole script aborts on the first failure
# (`set -euo pipefail`).

set -euo pipefail
IFS=$'\n\t'

# ── locate repo root ──────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$REPO_ROOT"

# ── colours ───────────────────────────────────────────────────────────────────
if [ -t 1 ] && [ "${NO_COLOR:-}" = "" ]; then
  RED=$'\033[0;31m'; GRN=$'\033[0;32m'; YEL=$'\033[0;33m'
  CYN=$'\033[0;36m'; BLD=$'\033[1m';   DIM=$'\033[2m'; RST=$'\033[0m'
else
  RED=''; GRN=''; YEL=''; CYN=''; BLD=''; DIM=''; RST=''
fi

step()  { echo "${BLD}${CYN}  ▶${RST}${BLD} $*${RST}"; }
ok()    { echo "${GRN}  ✓${RST} $*"; }
warn()  { echo "${YEL}  !${RST} $*" >&2; }
die()   { echo "${RED}  ✗${RST} $*" >&2; exit 1; }
info()  { echo "${DIM}    $*${RST}"; }

usage() {
  sed -n '2,/^$/p' "$0" | sed 's/^# \{0,1\}//'
  exit 0
}

# ── parse args ────────────────────────────────────────────────────────────────
DRY_RUN=0
ASSUME_YES=0
BUMP=""

while [ $# -gt 0 ]; do
  case "$1" in
    -h|--help)    usage ;;
    -n|--dry-run) DRY_RUN=1 ;;
    -y|--yes)     ASSUME_YES=1 ;;
    patch|minor|major) BUMP="$1" ;;
    [0-9]*.[0-9]*.[0-9]*) BUMP="$1" ;;
    *) die "unknown argument: $1 (use --help)" ;;
  esac
  shift
done

[ -n "$BUMP" ] || die "missing bump kind: patch | minor | major | X.Y.Z"

run() {
  if [ "$DRY_RUN" -eq 1 ]; then
    echo "${DIM}    [dry-run] $*${RST}"
  else
    eval "$@"
  fi
}

# ── pre-flight checks ─────────────────────────────────────────────────────────
step "Pre-flight checks"

command -v git >/dev/null || die "git not found"
command -v node >/dev/null || die "node not found (needed to read package.json)"

[ -f extension/package.json ] || die "extension/package.json not found"
[ -f extension/package-lock.json ] || die "extension/package-lock.json not found"
[ -f CHANGELOG.md ] || die "CHANGELOG.md not found"
[ -f .github/workflows/release.yml ] || die ".github/workflows/release.yml not found"

# Branch + clean tree + sync with origin
current_branch="$(git rev-parse --abbrev-ref HEAD)"
[ "$current_branch" = "main" ] || die "must be on 'main' (current: $current_branch)"

if ! git diff --quiet || ! git diff --cached --quiet; then
  die "working tree is dirty — commit or stash first"
fi

git fetch --tags origin >/dev/null 2>&1 || die "git fetch failed"

local_sha="$(git rev-parse HEAD)"
remote_sha="$(git rev-parse origin/main)"
[ "$local_sha" = "$remote_sha" ] || die "local main and origin/main differ — pull/rebase first"

ok "on main, clean, in sync with origin"

# ── compute versions ──────────────────────────────────────────────────────────
step "Computing version"

CURRENT="$(node -p "require('./extension/package.json').version")"
[ -n "$CURRENT" ] || die "could not read current version"
info "current: $CURRENT"

case "$BUMP" in
  patch|minor|major)
    NEW="$(node -e "
      const [maj,min,pat] = '$CURRENT'.split('.').map(Number);
      const kind = '$BUMP';
      let out;
      if (kind === 'patch') out = [maj, min, pat+1];
      else if (kind === 'minor') out = [maj, min+1, 0];
      else out = [maj+1, 0, 0];
      console.log(out.join('.'));
    ")"
    ;;
  *)
    NEW="$BUMP"
    ;;
esac

[[ "$NEW" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "invalid version: $NEW"
[ "$NEW" != "$CURRENT" ] || die "new version equals current ($NEW) — nothing to do"

TAG="v$NEW"
git rev-parse "$TAG" >/dev/null 2>&1 && die "tag $TAG already exists locally"
if git ls-remote --tags origin "$TAG" 2>/dev/null | grep -q "refs/tags/$TAG"; then
  die "tag $TAG already exists on origin"
fi

ok "$CURRENT  →  $NEW   (tag: $TAG)"

# ── confirmation ──────────────────────────────────────────────────────────────
if [ "$ASSUME_YES" -ne 1 ] && [ "$DRY_RUN" -ne 1 ]; then
  echo ""
  echo "${BLD}About to:${RST}"
  echo "  • bump extension/package.json + package-lock.json to $NEW"
  echo "  • rotate CHANGELOG [Unreleased] → [$NEW] - $(date +%F)"
  echo "  • commit + create annotated tag $TAG"
  echo "  • push main + $TAG to origin (triggers release.yml on GitHub)"
  echo ""
  read -r -p "Proceed? [y/N] " ans
  case "$ans" in
    y|Y|yes|YES) ;;
    *) die "aborted by user" ;;
  esac
fi

# ── 1. bump package files ─────────────────────────────────────────────────────
step "Bumping extension/package.json + package-lock.json"

run "node -e \"
const fs = require('fs');
for (const f of ['extension/package.json', 'extension/package-lock.json']) {
  const j = JSON.parse(fs.readFileSync(f, 'utf8'));
  if (j.version) j.version = '$NEW';
  if (j.packages && j.packages['']) j.packages[''].version = '$NEW';
  fs.writeFileSync(f, JSON.stringify(j, null, 2) + '\n');
  console.log('  updated', f);
}
\""

# ── 2. rotate CHANGELOG ──────────────────────────────────────────────────────
step "Rotating CHANGELOG"

TODAY="$(date +%F)"
if grep -qE '^## \[Unreleased\]' CHANGELOG.md; then
  # If the [Unreleased] section has content, rename it. Otherwise insert a
  # blank versioned section right after it.
  unreleased_body="$(awk '
    /^## \[Unreleased\]/  { in_unrel=1; next }
    /^## \[/              { in_unrel=0 }
    in_unrel              { print }
  ' CHANGELOG.md | sed -e '/^$/d')"

  if [ -n "$unreleased_body" ]; then
    info "promoting [Unreleased] → [$NEW] - $TODAY"
    run "awk -v new=\"$NEW\" -v today=\"$TODAY\" '
      /^## \[Unreleased\]/ {
        print \"## [Unreleased]\"
        print \"\"
        print \"## [\" new \"] - \" today
        next
      }
      { print }
    ' CHANGELOG.md > CHANGELOG.md.tmp && mv CHANGELOG.md.tmp CHANGELOG.md"
  else
    info "[Unreleased] is empty — inserting placeholder section for $NEW"
    run "awk -v new=\"$NEW\" -v today=\"$TODAY\" '
      /^## \[Unreleased\]/ {
        print
        print \"\"
        print \"## [\" new \"] - \" today
        print \"\"
        print \"### Changed\"
        print \"\"
        print \"- Release \" new \" (no notes — fill me in)\"
        seen=1
        next
      }
      { print }
    ' CHANGELOG.md > CHANGELOG.md.tmp && mv CHANGELOG.md.tmp CHANGELOG.md"
  fi
else
  warn "CHANGELOG.md has no [Unreleased] section — skipping rotation"
fi

# ── 3. commit ────────────────────────────────────────────────────────────────
step "Creating release commit"
run "git add extension/package.json extension/package-lock.json CHANGELOG.md"
run "git commit -m 'chore(release): $TAG'"

# ── 4. tag ───────────────────────────────────────────────────────────────────
step "Tagging $TAG"
run "git tag -a '$TAG' -m '$TAG'"

# ── 5. push ──────────────────────────────────────────────────────────────────
step "Pushing main + $TAG to origin"
run "git push origin main"
run "git push origin '$TAG'"

# ── 6. summary ───────────────────────────────────────────────────────────────
echo ""
ok "Released $TAG"
info "GitHub release workflow should now be running:"
info "  https://github.com/fabriziosalmi/l0-git/actions/workflows/release.yml"
info "Once it finishes, the release + .vsix will appear at:"
info "  https://github.com/fabriziosalmi/l0-git/releases/tag/$TAG"
