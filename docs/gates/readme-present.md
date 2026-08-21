---
title: "README present"
description: "Project root must contain a README file (README, README.md, README.rst, README.txt)."
---

# README present

A repository with no README is a repository nobody can adopt. This is the cheapest gate in the set and the one most worth keeping green.

<GateMeta id="readme_present" severity="warning" tags="project-hygiene" scope="Project root" />

## What it checks

Project root must contain a README file (README, README.md, README.rst, README.txt).

Accepted names, case-insensitive: `README`, `README.md`, `README.rst`,
`README.txt`. The file only has to exist — l0-git does not judge its contents.

## What a finding says

```text
No README file found in the project root. Add a README.md describing what this project is and how to use it.
```

## Turning it off

Silence the gate for the whole project in `.l0git.json`:

```json
{
  "ignore": ["readme_present"]
}
```

Or keep it running at a lower severity:

```json
{
  "severity": { "readme_present": "info" }
}
```

## See also

- [CONTRIBUTING present](/gates/contributing-present)
- [LICENSE present](/gates/license-present)
