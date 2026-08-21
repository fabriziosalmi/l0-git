---
title: Getting started
description: Install lgit, run the first scan, and read what comes back.
---

# Getting started

## Install

### From a release

Pre-built binaries are published for `linux-amd64`, `linux-arm64`,
`darwin-amd64`, `darwin-arm64` and `windows-amd64`.

```sh
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m); [ "$ARCH" = x86_64 ] && ARCH=amd64; [ "$ARCH" = aarch64 ] && ARCH=arm64

curl -fsSL "https://github.com/fabriziosalmi/l0-git/releases/latest/download/lgit-$OS-$ARCH.tar.gz" | tar xz
sudo mv "lgit-$OS-$ARCH" /usr/local/bin/lgit
```

Confirm it landed:

```sh
$ lgit version
0.1.27
```

::: tip Downloaded through a browser on macOS?
Gatekeeper quarantines it. `xattr -d com.apple.quarantine lgit` clears the flag.
Fetching with `curl` as above does not set it in the first place.
:::

### From source

Needs Go — the version is pinned in `server/go.mod`.

```sh
git clone https://github.com/fabriziosalmi/l0-git.git
cd l0-git
make build          # → server/lgit
```

## The first scan

`lgit check` takes a project root and runs every gate that is not disabled:

```sh
lgit check .
```

Output is JSON on stdout, always:

```json
{
  "project": "/Users/you/src/payments-api",
  "gates_run": ["readme_present", "license_present", "..."],
  "findings": [
    {
      "id": 9,
      "gate_id": "secrets_scan",
      "severity": "error",
      "title": "Tracked .env file",
      "message": ".env is tracked in git. .env files typically hold secrets and shouldn't be committed. …",
      "file_path": ".env:0:env_tracked",
      "status": "open"
    }
  ]
}
```

Counting what you got:

```sh
lgit check . | jq '{gates: (.gates_run|length), findings: (.findings|length)}'
```

## Reading the results

`check` persists everything, so you query it afterwards rather than re-scanning.

```sh
# just the errors
lgit list -project=$PWD -severity=error

# everything one gate found
lgit list -project=$PWD -gate=dockerfile_lint

# the shape of the problem
lgit stats -project=$PWD
```

::: warning `-project` wants an absolute path
It has to match the path `check` recorded. `-project=.` matches nothing — use
`-project=$PWD`.
:::

## Fixing something

Take a finding id from `lgit list` and ask what to do about it:

```sh
lgit fix 16
```

For the eight gates with deterministic recipes you get the exact commands and
file edits. For the rest you get the framing an agent needs to decide. Either
way `lgit fix` only prints — it never touches your tree.

Confirm the fix by re-running just that gate:

```sh
lgit check . ide_artifact_tracked
```

The finding flips from `open` to `resolved` rather than disappearing, so the
history of what you fixed stays intact.

## Turning down the noise

The first scan on a mature repository usually surfaces a pile of info-level
findings. That is the audit layer, not a work queue — start with
`-severity=error`.

When a gate is wrong for your project rather than merely unwelcome, disable it
in `.l0git.json` at the project root:

```json
{
  "ignore": ["changelog_present"],
  "severity": { "dead_placeholders": "info" },
  "gate_options": {
    "large_file_tracked": { "threshold_mb": 20 }
  }
}
```

See [Configuration](./configuration) for the full schema, including the
`exclude_paths` and fixture-skipping knobs every content-scanning gate accepts.

## Where next

- [VS Code extension](./vscode) — the same findings in the editor.
- [Claude Code / MCP](./mcp) — let an agent run the gates and read the fixes.
- [Gate reference](/gates/) — what all 35 gates actually check.
