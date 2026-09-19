---
title: "Tracked file matches .gitignore"
description: "Reports files that are in the git index even though the repository's own .gitignore excludes them — committed before the rule existed, or…"
---

# Tracked file matches .gitignore

Your `.gitignore` says a file should not be in the repository, and the index
says it is. One of the two is wrong, and nothing else will tell you which.

<GateMeta id="ignored_file_tracked" severity="warning" tags="git-hygiene" scope="Tracked files matched against committed `.gitignore` files" />

## What it checks

Reports files that are in the git index even though the repository's own .gitignore excludes them — committed before the rule existed, or force-added. Grouped by directory. Only committed .gitignore files are consulted, never .git/info/exclude or the user's global excludes, so the result is the same on every machine. .gitkeep files, env templates and vendored trees are left to the gates that own them.

A file ends up both tracked and ignored in one of two ways:

- **Committed before the rule existed.** Adding a pattern to `.gitignore`
  untracks nothing: the file stays in every future commit.
- **Force-added.** `git add -f` goes past the ignore rules without a trace in
  `.gitignore`.

Either way the repository says two contradictory things. The gate reports the
contradiction; it cannot know which side is right, so it does not guess.

### Why it exists

It was built after a sweep of the author's public repositories found node
identity keys tracked under a `data/` directory the `.gitignore` excluded, a
committed `.env`, `__pycache__/`, a coverage report, and 260 cache and report
files in a single repository. In every case the author had already decided the
files should not be committed — the `.gitignore` said so.

### Grouping

One finding per top-level directory, keyed on the deepest directory that holds
every file in the group, with a count and a few examples. 260 files in `cache/`
are one statement, not 260.

Files at the repository root form one group too, keyed on `.gitignore` — only
the root `.gitignore` can have excluded them. A single root file keeps its own
path, which is what an editor needs to jump to.

### What it leaves alone

| File | Why |
|---|---|
| `.gitkeep`, `.keep` | They exist only to be force-added into an ignored directory. |
| `.env.example`, `.env.sample`, `.env.template`, `.env.dist` | Routinely caught by a broad `.env*` rule on purpose. `secrets_scan` still reads them. |
| Vendored trees (`node_modules/`, `vendor/`, …) | [Vendored directory tracked](/gates/vendored-dir-tracked) already says the one actionable thing about them. |

### Deterministic on purpose

Only `.gitignore` files **committed to the repository** are consulted.
`.git/info/exclude` and your global excludes file (`core.excludesFile`) are
ignored: they differ between machines, and a gate whose result depends on who
runs it is not a gate.

## What a finding says

```text
2 tracked files under data/ match the repository's .gitignore: data/node-1/ed25519_private_key.pem, data/node-2/ed25519_private_key.pem. The index and the ignore rules disagree. If the files should not be in the repository, untrack them with `git rm -r --cached data/`. If they belong there, add a `!` negation to .gitignore so the exception is written down instead of implied.
```

## Fixing it

If the files should not be there:

```sh
git rm -r --cached data/
git commit -m "untrack files the .gitignore already excludes"
```

This removes them from the index and leaves them on disk. It does **not**
remove them from history — if they are secrets, rotate them first, then see
[Secrets scan (history)](/gates/secrets-scan-history).

If they belong in the repository, write the exception down:

```text
data/*
!data/schema.sql
```

A negated file is no longer ignored, so the finding goes away and the next
reader of `.gitignore` sees the intent.

## Options

The shared scan options apply — `exclude_paths` in particular:

```json
{
  "gate_options": {
    "ignored_file_tracked": { "exclude_paths": ["fixtures/*"] }
  }
}
```

## Turning it off

Silence the gate for the whole project in `.l0git.json`:

```json
{
  "ignore": ["ignored_file_tracked"]
}
```

Or keep it running at a lower severity:

```json
{
  "severity": { "ignored_file_tracked": "info" }
}
```

## See also

- [.gitignore coverage](/gates/gitignore-coverage)
- [Vendored directory tracked](/gates/vendored-dir-tracked)
- [Editor/IDE artefact tracked](/gates/ide-artifact-tracked)
