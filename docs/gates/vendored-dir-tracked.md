---
title: "Vendored directory tracked"
description: "Flags tracked files under well-known vendored directories (node_modules, vendor, target, dist, build, …). One finding per offending top-level…"
---

# Vendored directory tracked

Dependency directories are meant to be rebuilt from a manifest. Committing them bloats the repository and turns every upgrade into a merge conflict.

<GateMeta id="vendored_dir_tracked" severity="warning" tags="git-hygiene" scope="Tracked files (`git ls-files`)" />

## What it checks

Flags tracked files under well-known vendored directories (node_modules, vendor, target, dist, build, …). One finding per offending top-level directory.

Recognised prefixes: `node_modules/`, `vendor/`, `target/`, `dist/`,
`build/`, `.venv/`, `venv/`, `site-packages/`, `.next/`, `.nuxt/`, `.cache/`,
`__pycache__/`, `.pytest_cache/`, `.mypy_cache/`, `.tox/`, `bower_components/`.

One finding per offending top-level directory, not per file — a committed
`node_modules/` produces one finding, not forty thousand.

Names that double as ordinary hand-authored directories (`build/`, `dist/`,
`target/`) are only flagged when a stack marker makes the vendored reading
unambiguous. A `docs/build/` written by hand does not trip the gate.

## What a finding says

```text
node_modules/ is tracked. node_modules is meant to rebuild from a manifest — committing it bloats the repo and produces merge conflicts. Add node_modules to .gitignore and remove with `git rm -r --cached node_modules`.
```

## Turning it off

Silence the gate for the whole project in `.l0git.json`:

```json
{
  "ignore": ["vendored_dir_tracked"]
}
```

Or keep it running at a lower severity:

```json
{
  "severity": { "vendored_dir_tracked": "info" }
}
```

## See also

- [.gitignore coverage](/gates/gitignore-coverage)
- [Large file tracked](/gates/large-file-tracked)
