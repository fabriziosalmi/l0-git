---
title: "HTML lint"
description: "Deterministic AST lint of tracked .html/.htm files via golang.org/x/net/html. Fires for: viewport blocking zoom, autoplay video without muted…"
---

# HTML lint

Accessibility violations in tracked HTML that can be decided from the parse tree alone — no rendering, no heuristics.

<GateMeta id="html_lint" severity="warning" tags="accessibility,frontend" scope="Tracked files (`git ls-files`)" />

## What it checks

Deterministic AST lint of tracked .html/.htm files via golang.org/x/net/html. Fires for: viewport blocking zoom, autoplay video without muted, target=_blank without rel=noopener, icon-only controls without an accessible name, placeholders used as labels, and form reset buttons. Inline override via `<!-- l0git: ignore <rule_id> reason: … -->`. (Note: findings currently pin to file:1 — line-precise pin is queued as Phase B-bis.)

### Rules

| Rule | Severity | Fires when |
|---|---|---|
| `viewport_no_zoom` | warning | `user-scalable=no` or `maximum-scale=1` — WCAG 1.4.4 |
| `autoplay_with_sound` | warning | `<video autoplay>` without `muted` |
| `mystery_meat_nav` | warning | Icon-only control with no accessible name |
| `placeholder_as_label` | warning | An input whose only label is its `placeholder` |
| `reset_button` | warning | `type="reset"` — one misclick wipes the form |
| `target_blank_no_rel` | info | `target="_blank"` without `rel="noopener"` |

`target_blank_no_rel` reports at **info**, not warning, and deliberately does
not claim a vulnerability: every evergreen browser has implied `rel="noopener"`
for `target="_blank"` since 2021, so reverse tabnabbing is not reachable. What
remains — withholding the `Referer` header, and supporting pre-2021 browsers —
is a preference.

::: tip Known limitation
Findings currently pin to line 1 of the file rather than the offending element.
:::

Parsed with `golang.org/x/net/html`. Inline override:
`<!-- l0git: ignore <rule_id> reason: … -->`

## What a finding says

```text
index.html:1 Viewport meta blocks user zoom. user-scalable=no / maximum-scale=1 violates WCAG 1.4.4 (Resize Text). Drop the restriction.
```

## Options

```json
{
  "gate_options": {
    "html_lint": { "disabled_rules": ["target_blank_no_rel"] }
  }
}
```

## Turning it off

Silence the gate for the whole project in `.l0git.json`:

```json
{
  "ignore": ["html_lint"]
}
```

Or keep it running at a lower severity:

```json
{
  "severity": { "html_lint": "info" }
}
```

For a single occurrence, prefer the inline directive — it records the reason next to the code:

```text
<!-- l0git: ignore <rule_id> reason: … -->
```

## See also

- [CSS lint](/gates/css-lint)
- [Markdown lint](/gates/markdown-lint)
