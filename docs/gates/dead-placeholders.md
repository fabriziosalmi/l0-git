---
title: "Dead placeholders"
description: "Scans every tracked text file (≤ 2 MiB, binaries skipped) for TODO:/FIXME:/XXX:/HACK: markers, the phrase \"update this later\", and \"Lorem ipsum\"…"
---

# Dead placeholders

Finds the unfinished-work markers that were meant to be temporary.

<GateMeta id="dead_placeholders" severity="info" tags="documentation,quality" scope="Tracked files (`git ls-files`)" />

## What it checks

Scans every tracked text file (≤ 2 MiB, binaries skipped) for TODO:/FIXME:/XXX:/HACK: markers, the phrase "update this later", and "Lorem ipsum" filler. Severity info — these are intentional signals, but easy to miss before release. Disable individual patterns via gate_options.dead_placeholders.disabled_patterns.

Scans every tracked text file up to 2 MiB. Detected patterns:
`todo`, `fixme`, `xxx`, `hack`, `update_later` (the phrase "update this later")
and `lorem_ipsum`.

Severity is info by design: these are intentional signals from the author, not
mistakes. The value is in seeing them all at once before a release.

## What a finding says

```text
README.md:2 TODO: — unfinished-work placeholders are easy to miss before release; chase them down or replace with a tracked issue.
```

## Options

```json
{
  "gate_options": {
    "dead_placeholders": { "disabled_patterns": ["todo", "xxx"] }
  }
}
```

## Turning it off

Silence the gate for the whole project in `.l0git.json`:

```json
{
  "ignore": ["dead_placeholders"]
}
```

Or keep it running at a lower severity:

```json
{
  "severity": { "dead_placeholders": "info" }
}
```

## See also

- [Markdown lint](/gates/markdown-lint)
