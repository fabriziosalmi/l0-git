---
title: "File name quality"
description: "Surfaces tracked filenames containing spaces, control chars, or non-ASCII characters — these break unquoted shell pipelines and CI scripts."
---

# File name quality

Filenames containing spaces or invisible characters break every shell pipeline that forgot to quote `$f`.

<GateMeta id="filename_quality" severity="info" tags="git-hygiene,quality" scope="Tracked files (`git ls-files`)" />

## What it checks

Surfaces tracked filenames containing spaces, control chars, or non-ASCII characters — these break unquoted shell pipelines and CI scripts.

Reported categories:

| Category | What it means |
|---|---|
| `spaces` | The name contains a space or tab |
| `control chars` | C0/C1 control characters, or `DEL` |
| `bidi override chars` | Unicode bidirectional overrides — the [Trojan Source](https://trojansource.codes/) class |
| `zero-width chars` | Zero-width and other invisible code points |

Bidi and zero-width characters are worth more than a style note: a filename can
render as something other than what it is.

## What a finding says

```text
docs/my report.md has spaces. Tools and shell pipelines that don't quote argv or use IFS=$'\n' break on these. Rename if you can.
```

## Turning it off

Silence the gate for the whole project in `.l0git.json`:

```json
{
  "ignore": ["filename_quality"]
}
```

Or keep it running at a lower severity:

```json
{
  "severity": { "filename_quality": "info" }
}
```
