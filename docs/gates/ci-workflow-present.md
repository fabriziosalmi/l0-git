---
title: "CI workflow present"
description: "At least one workflow under .github/workflows/ should exist so builds and tests run on push."
---

# CI workflow present

If nothing runs the tests on push, the tests are decoration.

<GateMeta id="ci_workflow_present" severity="warning" tags="project-hygiene,build" scope="Project root" />

## What it checks

At least one workflow under .github/workflows/ should exist so builds and tests run on push.

Looks for any of: a file under `.github/workflows/`, `.gitlab-ci.yml`,
`.circleci/`, `Jenkinsfile`, and the other common pipeline entry points. One
match anywhere is enough to satisfy the gate.

## What a finding says

```text
No CI pipeline found (.github/workflows/, .gitlab-ci.yml, .circleci/, Jenkinsfile, …). Add one so tests run on push and pull requests.
```

## Turning it off

Silence the gate for the whole project in `.l0git.json`:

```json
{
  "ignore": ["ci_workflow_present"]
}
```

Or keep it running at a lower severity:

```json
{
  "severity": { "ci_workflow_present": "info" }
}
```

## See also

- [Tests present](/gates/tests-present)
