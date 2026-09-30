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

### A conflict shown in a Markdown code block

A rule page or a git tutorial shows a conflict inside a fenced code block, and
its lines start with exactly the text the gate looks for. In a `.md`, `.markdown`
or `.mdx` file each fenced block is judged on its own: a block whose markers are
**exactly one complete conflict** — `<<<<<<<`, then `=======`, then `>>>>>>>`, in
that order (a `|||||||` base line in between is allowed), and nothing else — is
reported at **warning**, not error. A marker outside every block, in a block that
holds anything other than that one conflict (two conflicts, an unfinished one, the
markers in the wrong order), or in a block that is never closed, is still an error,
and a real conflict anywhere in the file wins over an example. Files of any other
type are unchanged.

It is a warning and not `info` on purpose. The same text is what a **real**
conflict in a README code sample looks like — two branches editing the same
example is the commonest shape a conflict in a Markdown file takes — and `info`
is hidden by default in the editor. The message says that it cannot tell which it
is.

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
