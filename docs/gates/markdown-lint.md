---
title: "Markdown lint"
description: "Deterministic AST lint of tracked .md/.markdown files via goldmark. Fires for: image with empty alt, broken local-file link, broken in-document…"
---

# Markdown lint

AST lint over tracked Markdown, via goldmark. Catches the documentation defects that are checkable without a network.

<GateMeta id="markdown_lint" severity="warning" tags="documentation,accessibility" scope="Tracked files (`git ls-files`)" />

## What it checks

Deterministic AST lint of tracked .md/.markdown files via goldmark. Fires for: image with empty alt, broken local-file link, broken in-document anchor, fenced code block without language tag, and `json`/`yaml` blocks whose payload doesn't parse. Inline override via `<!-- l0git: ignore <rule_id> reason: … -->`. HTTP link liveness is intentionally NOT checked (would require network).

### Rules

| Rule | Severity | Fires when |
|---|---|---|
| `image_no_alt` | warning | `![](…)` — empty alt text |
| `link_local_broken` | warning | A relative link whose target is not in the repo |
| `link_anchor_broken` | warning | A `#anchor` matching no heading in the same file |
| `codeblock_invalid_payload` | warning | A block tagged `json`/`yaml` whose contents do not parse |
| `codeblock_no_language` | info, **off by default** | A fenced block with no language tag |

### Why codeblock_no_language is opt-in

An untagged fence is a style preference, not a verifiable defect — an
output or plain-text block legitimately has no language. It stayed the single
largest finding category in the corpus while being the least actionable, so it
is now opt-in via `enabled_rules`.

::: tip Not checked, on purpose
HTTP link liveness. Resolving external URLs would need a network call, and a
gate whose result depends on someone else's uptime is not deterministic.
:::

Inline override: `<!-- l0git: ignore <rule_id> reason: … -->`

## What a finding says

```text
README.md:37 Image without alt text. Empty alt makes the image invisible to screen readers and unusable when images fail to load. Describe what the image shows.
```

## Options

```json
{
  "gate_options": {
    "markdown_lint": {
      "enabled_rules": ["codeblock_no_language"],
      "disabled_rules": ["link_anchor_broken"]
    }
  }
}
```

## Turning it off

Silence the gate for the whole project in `.l0git.json`:

```json
{
  "ignore": ["markdown_lint"]
}
```

Or keep it running at a lower severity:

```json
{
  "severity": { "markdown_lint": "info" }
}
```

For a single occurrence, prefer the inline directive — it records the reason next to the code:

```text
<!-- l0git: ignore <rule_id> reason: … -->
```

## See also

- [Dead placeholders](/gates/dead-placeholders)
- [HTML lint](/gates/html-lint)
