#!/usr/bin/env bash
# Cross-compile the lgit Go binary for every platform the VSCode extension supports.
# Output goes into extension/bin/<platform>-<arch>/lgit[.exe], picked at runtime.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
ext_root="$(cd "$here/.." && pwd)"
server_root="$(cd "$ext_root/../server" && pwd)"
out_root="$ext_root/bin"

rm -rf "$out_root"
mkdir -p "$out_root"

# Stamp the same version string the standalone release binaries carry — no
# leading "v". Without this, the binary bundled in the .vsix reported "dev"
# while the release it shipped in was 0.2.0, so there was no way to tell from
# inside VS Code which version was running.
#   CI (release.yml): GITHUB_REF_NAME is the tag, e.g. v0.2.0 -> 0.2.0
#   local `make vsix`: git describe, e.g. v0.2.0-3-gabc1234-dirty -> 0.2.0-3-gabc1234-dirty
version="${VERSION:-}"
if [ -z "$version" ] && [ -n "${GITHUB_REF_NAME:-}" ]; then
  version="${GITHUB_REF_NAME#v}"
fi
if [ -z "$version" ]; then
  version="$(git -C "$ext_root" describe --tags --always --dirty 2>/dev/null || echo dev)"
  version="${version#v}"
fi
echo "version: $version"

build() {
  local goos="$1" goarch="$2" suffix="${3:-}"
  local dir="$out_root/${goos}-${goarch}"
  local bin="lgit${suffix}"
  mkdir -p "$dir"
  echo "→ $goos/$goarch"
  (cd "$server_root" && \
    GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 \
    go build -trimpath -ldflags="-s -w -X main.Version=${version}" -o "$dir/$bin" .)
}

build darwin  arm64
build darwin  amd64
build linux   amd64
build linux   arm64
build windows amd64 .exe

echo
echo "Built binaries:"
find "$out_root" -type f -exec ls -lh {} \;
