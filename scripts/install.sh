#!/bin/sh
# scripts/install.sh: install the lgit binary from a GitHub release.
#
# Usage:
#   curl -fsSL https://github.com/fabriziosalmi/l0-git/releases/latest/download/install.sh | sh
#
# Environment:
#   LGIT_VERSION      release to install, e.g. 0.3.3 (default: latest)
#   LGIT_INSTALL_DIR  where to put the binary (default: $HOME/.local/bin)
#   LGIT_BASE_URL     download base, for mirrors and tests
#                     (default: https://github.com/fabriziosalmi/l0-git/releases)
#
# What it does:
#   1. Picks the archive for this OS and CPU (linux, darwin; amd64, arm64).
#   2. Downloads it with checksums.txt from the same release and refuses to
#      install unless the sha256 matches.
#   3. Installs lgit into LGIT_INSTALL_DIR without sudo, then runs
#      `lgit version` so a binary that cannot start is reported here.
#
# Everything sits inside main(), called on the last line, so a download cut
# short by the network never runs half a script.

set -eu

say() { printf '%s\n' "$*"; }
die() { printf 'lgit install: %s\n' "$*" >&2; exit 1; }

need() { command -v "$1" >/dev/null 2>&1 || die "'$1' is required but not found"; }

detect_os() {
  case "$(uname -s)" in
    Darwin) echo darwin ;;
    Linux) echo linux ;;
    *) die "unsupported OS '$(uname -s)'. On Windows, download lgit-windows-amd64.zip from the releases page." ;;
  esac
}

detect_arch() {
  arch="$(uname -m)"
  # A shell running under Rosetta reports x86_64 on Apple Silicon: install
  # the native binary anyway.
  if [ "$arch" = x86_64 ] && [ "$(sysctl -n sysctl.proc_translated 2>/dev/null || echo 0)" = 1 ]; then
    arch=arm64
  fi
  case "$arch" in
    x86_64 | amd64) echo amd64 ;;
    arm64 | aarch64) echo arm64 ;;
    *) die "unsupported CPU '$arch'" ;;
  esac
}

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    die "need sha256sum or shasum to verify the download"
  fi
}

main() {
  need curl
  need tar
  need uname

  os="$(detect_os)"
  arch="$(detect_arch)"
  base="${LGIT_BASE_URL:-https://github.com/fabriziosalmi/l0-git/releases}"
  version="${LGIT_VERSION:-latest}"
  version="${version#v}"
  if [ "$version" = latest ]; then
    url="$base/latest/download"
  else
    url="$base/download/v$version"
  fi
  dest="${LGIT_INSTALL_DIR:-$HOME/.local/bin}"
  name="lgit-$os-$arch"
  archive="$name.tar.gz"

  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' EXIT INT TERM

  say "Downloading $archive ($version)"
  curl -fsSL -o "$tmp/$archive" "$url/$archive" || die "download failed: $url/$archive"
  curl -fsSL -o "$tmp/checksums.txt" "$url/checksums.txt" ||
    die "this release publishes no checksums.txt, refusing to install an unverified binary (set LGIT_VERSION to 0.3.3 or later)"

  want="$(awk -v f="$archive" '$2 == f || $2 == "*" f {print $1}' "$tmp/checksums.txt")"
  [ -n "$want" ] || die "checksums.txt has no entry for $archive"
  got="$(sha256_of "$tmp/$archive")"
  [ "$want" = "$got" ] || die "sha256 mismatch for $archive: expected $want, got $got"
  say "Checksum verified"

  tar -xzf "$tmp/$archive" -C "$tmp" "$name" || die "archive does not contain $name"
  mkdir -p "$dest"
  # Copy beside the target, then rename: a running lgit is never overwritten
  # in place.
  cp "$tmp/$name" "$dest/.lgit.tmp.$$"
  chmod 0755 "$dest/.lgit.tmp.$$"
  mv -f "$dest/.lgit.tmp.$$" "$dest/lgit"

  installed="$("$dest/lgit" version 2>&1)" || die "installed $dest/lgit but it does not run: $installed"
  say "Installed lgit $installed to $dest/lgit"

  case ":$PATH:" in
    *":$dest:"*) ;;
    *)
      say ""
      say "$dest is not on your PATH. Add this line to ~/.zshrc or ~/.bashrc:"
      say "  export PATH=\"$dest:\$PATH\""
      ;;
  esac
}

main "$@"
