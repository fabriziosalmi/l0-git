---
title: "CODEOWNERS present"
description: "Looks for a CODEOWNERS file at project root, .github/, or docs/. Silent on docs-only repos; fires when the project has source files in a recognised…"
---

# CODEOWNERS present

Routes pull requests to the people who actually own the touched paths.

<GateMeta id="codeowners_present" severity="info" tags="governance" scope="Project root" />

## What it checks

Looks for a CODEOWNERS file at project root, .github/, or docs/. Silent on docs-only repos; fires when the project has source files in a recognised language.

Searched at the project root, `.github/`, and `docs/`. The gate stays
silent on documentation-only repositories — it fires only once the project
contains source files in a recognised language.

## What a finding says

```text
No CODEOWNERS file found at the project root, .github/, or docs/. Declare reviewers per path so PRs route to the right people automatically.
```

## Turning it off

Silence the gate for the whole project in `.l0git.json`:

```json
{
  "ignore": ["codeowners_present"]
}
```

Or keep it running at a lower severity:

```json
{
  "severity": { "codeowners_present": "info" }
}
```

## See also

- [Branch protection declared](/gates/branch-protection-declared)
