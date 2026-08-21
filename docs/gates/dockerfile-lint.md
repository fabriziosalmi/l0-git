---
title: "Dockerfile lint"
description: "Deterministic AST-based lint of tracked Dockerfiles. Fires for: untagged FROM, FROM :latest, ADD instruction, missing USER, USER root. Inline…"
---

# Dockerfile lint

An AST-based lint over tracked Dockerfiles — reproducibility and least privilege, nothing stylistic.

<GateMeta id="dockerfile_lint" severity="warning" tags="containers,security,build" scope="Tracked files (`git ls-files`)" />

## What it checks

Deterministic AST-based lint of tracked Dockerfiles. Fires for: untagged FROM, FROM :latest, ADD instruction, missing USER, USER root. Inline override via `# l0git: ignore <rule_id> reason: …`. Silent on repos without Dockerfile (set gate_options.dockerfile_lint.suggest_when_missing to opt in).

### Rules

| Rule | Severity | Fires when |
|---|---|---|
| `from_untagged` | warning | `FROM` has no tag at all |
| `from_latest` | warning | `FROM` pins `:latest` |
| `add_instruction` | info | `ADD` used where `COPY` would do |
| `missing_user` | warning | `ENTRYPOINT`/`CMD` with no `USER` earlier in the stage |
| `user_root` | warning | `USER` is explicitly `root` |

`missing_user` is evaluated **per build stage**, so a multi-stage file is not
punished for its builder stage.

### Silent by default on repos without a Dockerfile

If nothing containerised is tracked, the gate emits nothing. Set
`suggest_when_missing` to get a single info finding instead.

## What a finding says

```text
Dockerfile:1 FROM node:latest pins :latest. `:latest` is a moving target. Pin to a real version (or a digest with @sha256:…) so the image you ship today still rebuilds tomorrow.
```

## Options

```json
{
  "gate_options": {
    "dockerfile_lint": {
      "disabled_rules": ["add_instruction"],
      "suggest_when_missing": false
    }
  }
}
```

Use `disabled_rules` for a repo-wide policy decision. For a single line, prefer
the inline directive — it records *why*:

```dockerfile
# l0git: ignore from_latest reason: dev base image, never released
FROM node:latest
```

## Turning it off

Silence the gate for the whole project in `.l0git.json`:

```json
{
  "ignore": ["dockerfile_lint"]
}
```

Or keep it running at a lower severity:

```json
{
  "severity": { "dockerfile_lint": "info" }
}
```

For a single occurrence, prefer the inline directive — it records the reason next to the code:

```text
# l0git: ignore <rule_id> reason: …
```

## See also

- [Compose lint](/gates/compose-lint)
