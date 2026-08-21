---
title: "CHANGELOG present"
description: "A CHANGELOG.md gives users a single place to see what changed between releases."
---

# CHANGELOG present

One place users can look to see what changed between releases, instead of reading the commit log.

<GateMeta id="changelog_present" severity="info" tags="project-hygiene,release-hygiene" scope="Project root" />

## What it checks

A CHANGELOG.md gives users a single place to see what changed between releases.

Accepted: `CHANGELOG`, `CHANGES`, `HISTORY`, with or without an extension.

## What a finding says

```text
No CHANGELOG file at the project root. Consider adopting Keep a Changelog and adding CHANGELOG.md.
```

## Turning it off

Silence the gate for the whole project in `.l0git.json`:

```json
{
  "ignore": ["changelog_present"]
}
```

Or keep it running at a lower severity:

```json
{
  "severity": { "changelog_present": "info" }
}
```

## See also

- [Version drift](/gates/version-drift)
