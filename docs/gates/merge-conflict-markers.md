---
title: "Merge conflict markers"
description: "Detects unresolved git merge conflict markers (<<<<<<<, =======, >>>>>>>) in tracked files. Anything that lands on main with these is a bug."
---

# Merge conflict markers

An unresolved conflict marker on a shipping branch is never intentional. This is one of only two gates that report at **error** severity.

<GateMeta id="merge_conflict_markers" severity="error" tags="git-hygiene" scope="Tracked files (`git ls-files`)" />

## What it checks

Detects unresolved git merge conflict markers (<<<<<<<, =======, >>>>>>>) in tracked files. Anything that lands on main with these is a bug.

Scans tracked files for `<<<<<<<`, `=======` and `>>>>>>>` at the start
of a line. Reports the file and the first offending line number.

## What a finding says

```text
src/index.js:2 contains an unresolved merge conflict marker (<<<<<<<, =======, or >>>>>>>). Resolve the conflict before committing.
```

## Turning it off

Silence the gate for the whole project in `.l0git.json`:

```json
{
  "ignore": ["merge_conflict_markers"]
}
```

Or keep it running at a lower severity:

```json
{
  "severity": { "merge_conflict_markers": "info" }
}
```

## See also

- [Secrets scan](/gates/secrets-scan)
