---
title: "CONTRIBUTING present"
description: "A CONTRIBUTING.md helps outside contributors know how to set up and submit changes."
---

# CONTRIBUTING present

Tells an outside contributor how to build, test and submit a change before they burn an afternoon guessing.

<GateMeta id="contributing_present" severity="info" tags="project-hygiene" scope="Project root" />

## What it checks

A CONTRIBUTING.md helps outside contributors know how to set up and submit changes.

Accepted: `CONTRIBUTING`, `CONTRIBUTING.*` (any extension).

## What a finding says

```text
No CONTRIBUTING file at the project root. Document how to build, test, and submit PRs.
```

## Turning it off

Silence the gate for the whole project in `.l0git.json`:

```json
{
  "ignore": ["contributing_present"]
}
```

Or keep it running at a lower severity:

```json
{
  "severity": { "contributing_present": "info" }
}
```

## See also

- [CODE_OF_CONDUCT present](/gates/code-of-conduct-present)
- [Pull request template present](/gates/pr-template-present)
