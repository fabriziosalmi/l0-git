---
title: ".gitignore present"
description: "A .gitignore at the project root prevents accidental commits of build artefacts and secrets."
---

# .gitignore present

The first line of defence against committing build artefacts and secrets by accident. Checked at the project root only.

<GateMeta id="gitignore_present" severity="warning" tags="project-hygiene,git-hygiene" scope="Project root" />

## What it checks

A .gitignore at the project root prevents accidental commits of build artefacts and secrets.

## What a finding says

```text
No .gitignore at the project root. Add one to keep build artefacts and secrets out of the repo.
```

## Turning it off

Silence the gate for the whole project in `.l0git.json`:

```json
{
  "ignore": ["gitignore_present"]
}
```

Or keep it running at a lower severity:

```json
{
  "severity": { "gitignore_present": "info" }
}
```

## See also

- [.gitignore coverage](/gates/gitignore-coverage)
