---
title: "Version drift"
description: "Cross-checks declared versions across package.json, Cargo.toml, pyproject.toml, mix.exs, pom.xml, and a top-level VERSION file. Disagreement is a…"
---

# Version drift

When two manifests in the same repository claim different versions, at least one of them is lying to whoever reads it.

<GateMeta id="version_drift" severity="warning" tags="release-hygiene" scope="Project root" />

## What it checks

Cross-checks declared versions across package.json, Cargo.toml, pyproject.toml, mix.exs, pom.xml, and a top-level VERSION file. Disagreement is a release-hygiene smell.

Cross-checks the version declared in `package.json`, `Cargo.toml`,
`pyproject.toml`, `mix.exs`, `pom.xml`, and a top-level `VERSION` file. Any
disagreement between two manifests produces a finding naming both.

## What a finding says

```text
package.json declares version 1.4.0 but Cargo.toml declares 1.3.2. Pick a single source of truth or wire a build-time sync.
```

## Turning it off

Silence the gate for the whole project in `.l0git.json`:

```json
{
  "ignore": ["version_drift"]
}
```

Or keep it running at a lower severity:

```json
{
  "severity": { "version_drift": "info" }
}
```

## See also

- [CHANGELOG present](/gates/changelog-present)
