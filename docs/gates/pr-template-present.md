---
title: "Pull request template present"
description: "A .github/PULL_REQUEST_TEMPLATE.md (or pull_request_template.md) standardises PR descriptions."
---

# Pull request template present

Standardises what a PR description contains, so reviewers stop asking the same three questions.

<GateMeta id="pr_template_present" severity="info" tags="project-hygiene" scope="Project root" />

## What it checks

A .github/PULL_REQUEST_TEMPLATE.md (or pull_request_template.md) standardises PR descriptions.

Accepted: `.github/PULL_REQUEST_TEMPLATE.md` or `.github/pull_request_template.md`.

## What a finding says

```text
No .github/PULL_REQUEST_TEMPLATE.md. Add one so PR descriptions follow a consistent shape.
```

## Turning it off

Silence the gate for the whole project in `.l0git.json`:

```json
{
  "ignore": ["pr_template_present"]
}
```

Or keep it running at a lower severity:

```json
{
  "severity": { "pr_template_present": "info" }
}
```

## See also

- [Issue templates present](/gates/issue-template-present)
