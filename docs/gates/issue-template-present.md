---
title: "Issue templates present"
description: "At least one .github/ISSUE_TEMPLATE/*.md helps reporters file useful bug reports and requests."
---

# Issue templates present

Turns "it doesn't work" into a report you can actually act on.

<GateMeta id="issue_template_present" severity="info" tags="project-hygiene" scope="Project root" />

## What it checks

At least one .github/ISSUE_TEMPLATE/*.md helps reporters file useful bug reports and requests.

Satisfied by at least one `.github/ISSUE_TEMPLATE/*.md`.

## What a finding says

```text
No .github/ISSUE_TEMPLATE/. Add at least one bug_report.md / feature_request.md template.
```

## Turning it off

Silence the gate for the whole project in `.l0git.json`:

```json
{
  "ignore": ["issue_template_present"]
}
```

Or keep it running at a lower severity:

```json
{
  "severity": { "issue_template_present": "info" }
}
```

## See also

- [Pull request template present](/gates/pr-template-present)
