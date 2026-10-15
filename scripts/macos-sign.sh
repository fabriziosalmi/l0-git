#!/usr/bin/env bash
# scripts/macos-sign.sh: sign lgit binaries with a Developer ID, notarize
# them, and prove Gatekeeper accepts them as if a browser had downloaded them.
#
# Usage:
#   scripts/macos-sign.sh <binary> [<binary>...]
#
# Signing identity (must be in a keychain on the search list):
#   SIGN_IDENTITY      default: "Developer ID Application"
#
# Notary credentials, one of:
#   NOTARY_PROFILE     a profile saved with `xcrun notarytool store-credentials`
#                      (local use)
#   APPLE_API_KEY_PATH, APPLE_API_KEY_ID, APPLE_API_ISSUER_ID
#                      an App Store Connect API key (CI)
#
# A bare Mach-O cannot carry a stapled ticket, so Gatekeeper looks the
# notarization up online on first launch. The last step does the same lookup
# here, on a copy carrying the quarantine attribute a browser sets, and fails
# unless Gatekeeper answers "Notarized Developer ID".

set -euo pipefail

die() { echo "macos-sign: $*" >&2; exit 1; }

[ "$(uname -s)" = Darwin ] || die "must run on macOS"
[ $# -gt 0 ] || die "usage: $0 <binary> [<binary>...]"

identity="${SIGN_IDENTITY:-Developer ID Application}"
bundle_id="io.github.fabriziosalmi.lgit"

if [ -n "${NOTARY_PROFILE:-}" ]; then
  auth=(--keychain-profile "$NOTARY_PROFILE")
elif [ -n "${APPLE_API_KEY_PATH:-}" ] && [ -n "${APPLE_API_KEY_ID:-}" ] && [ -n "${APPLE_API_ISSUER_ID:-}" ]; then
  auth=(--key "$APPLE_API_KEY_PATH" --key-id "$APPLE_API_KEY_ID" --issuer "$APPLE_API_ISSUER_ID")
else
  die "no notary credentials: set NOTARY_PROFILE or APPLE_API_KEY_PATH/APPLE_API_KEY_ID/APPLE_API_ISSUER_ID"
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
mkdir "$work/bins"

echo "==> signing"
for bin in "$@"; do
  [ -f "$bin" ] || die "no such file: $bin"
  # Hardened runtime and a secure timestamp are both required by the notary.
  codesign --force --options runtime --timestamp \
    --identifier "$bundle_id" --sign "$identity" "$bin"
  codesign --verify --strict --verbose=2 "$bin"
  cp "$bin" "$work/bins/"
done

echo "==> notarizing"
# The notary takes zip, pkg or dmg: submit the signed binaries zipped. The
# ticket is keyed on each binary's code hash, so the binaries shipped in the
# tarballs are the ones that are notarized.
zip="$work/notarize.zip"
ditto -c -k "$work/bins" "$zip"
result="$(xcrun notarytool submit "$zip" "${auth[@]}" --wait --timeout 30m --output-format json)"
echo "$result"
id="$(plutil -extract id raw -o - - <<<"$result")"
status="$(plutil -extract status raw -o - - <<<"$result")"
if [ "$status" != Accepted ]; then
  xcrun notarytool log "$id" "${auth[@]}" || true
  die "notarization status is '$status', not Accepted"
fi

echo "==> Gatekeeper, as after a browser download"
# `spctl --type execute` only assesses app bundles and answers "does not seem
# to be an app" for a command-line tool, notarized or not. The open context on
# the primary signature is the assessment that tells the two apart, and
# codesign checks the notarization requirement on its own.
for bin in "$@"; do
  probe="$work/probe-$(basename "$bin")"
  cp "$bin" "$probe"
  xattr -w com.apple.quarantine "0081;$(printf '%x' "$(date +%s)");Safari;" "$probe"
  verdict=""
  # The ticket can take a moment to be served after Accepted.
  for _ in 1 2 3 4 5 6; do
    verdict="$(spctl --assess --type open --context context:primary-signature -vv "$probe" 2>&1 || true)"
    case "$verdict" in *"source=Notarized Developer ID"*) break ;; esac
    sleep 10
  done
  echo "$verdict"
  case "$verdict" in
    *": accepted"*"source=Notarized Developer ID"*) ;;
    *) die "Gatekeeper does not accept $bin as notarized" ;;
  esac
  codesign --verify -R="notarized" --check-notarization "$probe" ||
    die "$bin does not satisfy the notarized requirement"
done

echo "==> done: $# binaries signed, notarized and accepted"
