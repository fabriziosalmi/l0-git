---
title: "CODE_OF_CONDUCT present"
description: "Looks for CODE_OF_CONDUCT.md at project root, .github/, or docs/. Adopt the Contributor Covenant or similar so contributors know the rules of…"
---

# CODE_OF_CONDUCT present

States the rules of engagement before you need them, not during the incident.

<GateMeta id="code_of_conduct_present" severity="info" tags="project-hygiene,governance" scope="Project root" />

## What it checks

Looks for CODE_OF_CONDUCT.md at project root, .github/, or docs/. Adopt the Contributor Covenant or similar so contributors know the rules of engagement.

Searched at the project root, `.github/`, and `docs/`.

## What a finding says

```text
No CODE_OF_CONDUCT.md found at the project root, .github/, or docs/. Adopt the Contributor Covenant and add CODE_OF_CONDUCT.md so contributors know the rules of engagement.
```

## Turning it off

Silence the gate for the whole project in `.l0git.json`:

```json
{
  "ignore": ["code_of_conduct_present"]
}
```

Or keep it running at a lower severity:

```json
{
  "severity": { "code_of_conduct_present": "info" }
}
```

## See also

- [CONTRIBUTING present](/gates/contributing-present)
