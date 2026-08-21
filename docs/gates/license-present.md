---
title: "LICENSE present"
description: "Project root should declare a license (LICENSE / LICENSE.md / LICENSE.txt / COPYING)."
---

# LICENSE present

Without a license file, default copyright applies and nobody may legally reuse the code — however open the repository looks.

<GateMeta id="license_present" severity="warning" tags="project-hygiene" scope="Project root" />

## What it checks

Project root should declare a license (LICENSE / LICENSE.md / LICENSE.txt / COPYING).

Accepted at the project root only: `LICENSE`, `LICENSE.*`, `COPYING`,
`COPYING.*`, `UNLICENSE`. Unlike the README check this one does **not** search
subdirectories: a license buried in `docs/` is not where tooling looks for it.

## What a finding says

```text
No LICENSE file at the project root. Pick a license (e.g. MIT, Apache-2.0) and add it as LICENSE.
```

## Turning it off

Silence the gate for the whole project in `.l0git.json`:

```json
{
  "ignore": ["license_present"]
}
```

Or keep it running at a lower severity:

```json
{
  "severity": { "license_present": "info" }
}
```

## See also

- [README present](/gates/readme-present)
