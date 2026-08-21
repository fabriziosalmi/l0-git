---
title: "Missing .nvmrc / .node-version"
description: "Fires when package.json exists but no .nvmrc / .node-version pins the Node runtime. nvm/asdf/Volta users (and CI runners) need this for reproducible…"
---

# Missing .nvmrc / .node-version

A `package.json` with no pinned Node version means nvm, asdf, Volta and your CI runner each pick whatever Node they happen to have.

<GateMeta id="nvmrc_missing" severity="info" tags="release-hygiene,quality" scope="Project root" />

## What it checks

Fires when package.json exists but no .nvmrc / .node-version pins the Node runtime. nvm/asdf/Volta users (and CI runners) need this for reproducible toolchains.

Fires only when `package.json` exists and neither `.nvmrc` nor
`.node-version` is present. The two files are interchangeable for this check.

## What a finding says

```text
package.json exists but no .nvmrc / .node-version pins the runtime. nvm/asdf/Volta users (and CI runners) will silently pick whatever Node is on PATH. Add a one-line .nvmrc with the target version.
```

## Turning it off

Silence the gate for the whole project in `.l0git.json`:

```json
{
  "ignore": ["nvmrc_missing"]
}
```

Or keep it running at a lower severity:

```json
{
  "severity": { "nvmrc_missing": "info" }
}
```
