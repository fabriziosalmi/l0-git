---
title: "Editor/IDE artefact tracked"
description: "Flags tracked editor/IDE/OS artefacts (.vscode/, .idea/, .DS_Store, Thumbs.db, *.swp, *~, …)."
---

# Editor/IDE artefact tracked

User-local editor state does not belong in shared history.

<GateMeta id="ide_artifact_tracked" severity="warning" tags="git-hygiene" scope="Tracked files (`git ls-files`)" />

## What it checks

Flags tracked editor/IDE/OS artefacts (.vscode/, .idea/, .DS_Store, Thumbs.db, *.swp, *~, …).

Flagged: `.DS_Store`, `Thumbs.db`, `desktop.ini`, `*.swp`, `*.swo`,
`*~`, and anything under `.vscode/`, `.idea/`, `.vs/`, `.sublime-project/`,
`.sublime-workspace/`.

**Deliberate exception.** `.vscode/settings.json`, `tasks.json`, `launch.json`,
`extensions.json` and `mcp.json` are *not* flagged. VS Code defines these as
project-level and shareable, and GitHub's own `VisualStudioCode.gitignore`
ignores `.vscode/*` and then explicitly un-ignores exactly these five.
Committing them is the documented convention, not an accident.

## What a finding says

```text
.idea/workspace.xml is a user-local editor/IDE/OS artefact and shouldn't live in shared history. Add it to .gitignore and remove with `git rm --cached .idea/workspace.xml`.
```

## Turning it off

Silence the gate for the whole project in `.l0git.json`:

```json
{
  "ignore": ["ide_artifact_tracked"]
}
```

Or keep it running at a lower severity:

```json
{
  "severity": { "ide_artifact_tracked": "info" }
}
```

## See also

- [.gitignore coverage](/gates/gitignore-coverage)
