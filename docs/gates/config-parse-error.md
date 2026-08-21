---
title: "Config parse error"
description: "Parses every tracked JSON and YAML config file and flags any that don't parse — a broken package.json, CI workflow, or k8s manifest is a…"
---

# Config parse error

A `package.json` or CI workflow that does not parse is a defect you can prove without running anything. This gate proves it.

<GateMeta id="config_parse_error" severity="warning" tags="quality,build" scope="Tracked files (`git ls-files`)" />

## What it checks

::: v-pre
Parses every tracked JSON and YAML config file and flags any that don't parse — a broken package.json, CI workflow, or k8s manifest is a deterministic defect. JSONC (tsconfig, .vscode/*.json, *.jsonc) and template files (Helm/Jinja {{ }}, ERB <% %>) are skipped; custom YAML tags (!Ref, …) are accepted. TOML/INI are out of scope.
:::

Every tracked `.json`, `.yaml` and `.yml` file is parsed. A file that
fails to decode produces one finding.

Deliberately **out of scope**, because a parse failure there would be a false
positive rather than a defect:

- **JSONC** — `tsconfig.json`, `.vscode/*.json`, `*.jsonc` all permit comments.
- **Template files** — Helm / Jinja <code v-pre>{{ … }}</code>, ERB `<% … %>`. These are not
  valid YAML until rendered.
- **TOML and INI** — not parsed at all.

Custom YAML tags (`!Ref`, `!GetAtt`, …) are accepted rather than rejected.

## What a finding says

```text
.github/workflows/ci.yml failed to parse.
```

## Turning it off

Silence the gate for the whole project in `.l0git.json`:

```json
{
  "ignore": ["config_parse_error"]
}
```

Or keep it running at a lower severity:

```json
{
  "severity": { "config_parse_error": "info" }
}
```

## See also

- [Compose lint](/gates/compose-lint)
