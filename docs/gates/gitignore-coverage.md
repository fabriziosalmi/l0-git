---
title: ".gitignore coverage"
description: "Cross-checks .gitignore against a hardcoded `if-stack-then-must-ignore` table: package.json → node_modules, Cargo.toml → target…"
---

# .gitignore coverage

Having a `.gitignore` is not the same as having the right one. This gate cross-checks it against the stack it can actually see.

<GateMeta id="gitignore_coverage" severity="warning" tags="git-hygiene" scope="Project root" />

## What it checks

Cross-checks .gitignore against a hardcoded `if-stack-then-must-ignore` table: package.json → node_modules, Cargo.toml → target, pyproject.toml/setup.py → __pycache__/.venv, Gemfile → .bundle/vendor/bundle, plus the universal .DS_Store. Silent on repos with no recognised stack markers.

The rule table is fixed and small — *if this marker exists, then
these entries must be covered*:

| Marker | Required entries |
|---|---|
| `package.json` | `node_modules` |
| `Cargo.toml` | `target` |
| `pyproject.toml` | `__pycache__`, `.venv` |
| `setup.py` | `__pycache__`, `.venv` |
| `Gemfile` | `.bundle`, `vendor/bundle` |
| *(any recognised stack)* | `.DS_Store` |

`go.mod` deliberately requires nothing — vendored Go modules are an opt-in
choice, not an accident.

The gate is silent on repositories with no recognised stack marker, and it
understands existing entries well enough not to propose a redundant one (a
`**/node_modules/` line already counts as covering `node_modules`).

## What a finding says

```text
.gitignore does not cover `node_modules`. Node projects rebuild node_modules from package.json + lockfile; committing it bloats the repo and breaks installs. Add `node_modules` to your .gitignore.
```

## Options

```json
{
  "gate_options": {
    "gitignore_coverage": { "disabled_patterns": [".DS_Store"] }
  }
}
```

## Turning it off

Silence the gate for the whole project in `.l0git.json`:

```json
{
  "ignore": ["gitignore_coverage"]
}
```

Or keep it running at a lower severity:

```json
{
  "severity": { "gitignore_coverage": "info" }
}
```

## See also

- [.gitignore present](/gates/gitignore-present)
- [Vendored directory tracked](/gates/vendored-dir-tracked)
