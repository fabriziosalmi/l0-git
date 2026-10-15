#!/usr/bin/env bash
# scripts/bump-formula.sh: point the Homebrew formula at a new release.
#
# Usage:
#   scripts/bump-formula.sh <formula.rb> <version> <checksums.txt>
#
# Rewrites the version in each target's url and the sha256 under it, taking
# the hashes from the release's checksums.txt. Fails unless every target ends
# up with exactly the expected url and hash, so a formula it cannot parse is
# never pushed half updated.

set -euo pipefail

die() { echo "bump-formula: $*" >&2; exit 1; }

[ $# -eq 3 ] || die "usage: $0 <formula.rb> <version> <checksums.txt>"
formula="$1"
version="${2#v}"
sums="$3"

for target in darwin-arm64 darwin-amd64 linux-arm64 linux-amd64; do
  archive="lgit-$target.tar.gz"
  sha="$(awk -v f="$archive" '$2 == f {print $1}' "$sums")"
  [[ "$sha" =~ ^[0-9a-f]{64}$ ]] || die "no sha256 for $archive in $sums"
  V="$version" A="$archive" S="$sha" perl -0pi -e \
    's{(/releases/download/)v[^/]+(/\Q$ENV{A}\E"\s*\n\s*sha256 ")[0-9a-f]{64}}{$1v$ENV{V}$2$ENV{S}}' \
    "$formula"
  grep -A1 -F "/releases/download/v$version/$archive\"" "$formula" | grep -qF "sha256 \"$sha\"" ||
    die "$archive was not updated to v$version"
done

echo "bumped $formula to v$version"
