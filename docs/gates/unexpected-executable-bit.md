---
title: "Unexpected executable bit"
description: "Flags tracked files with git mode 100755 whose extension/name suggests a text/data file (e.g. README.md tracked as executable)."
---

# Unexpected executable bit

A `README.md` tracked as mode 100755 is a mistake that survives every clone and confuses every packaging script.

<GateMeta id="unexpected_executable_bit" severity="warning" tags="git-hygiene" scope="Tracked files (`git ls-files`)" />

## What it checks

Flags tracked files with git mode 100755 whose extension/name suggests a text/data file (e.g. README.md tracked as executable).

Flags tracked files whose git mode is `100755` but whose extension or
name says text or data rather than script. The remediation is deterministic:
`git update-index --chmod=-x <path>`.

## What a finding says

```text
README.md is tracked with mode 100755 (executable), but its extension/name suggests a text/data file. Run `git update-index --chmod=-x README.md` and commit the fix.
```

## Turning it off

Silence the gate for the whole project in `.l0git.json`:

```json
{
  "ignore": ["unexpected_executable_bit"]
}
```

Or keep it running at a lower severity:

```json
{
  "severity": { "unexpected_executable_bit": "info" }
}
```
