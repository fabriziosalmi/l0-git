---
title: "CSS lint"
description: "Hand-rolled scan of tracked .css/.scss/.less/.sass/.styl files (skipping .min.css). Fires for: hidden scrollbar (display:none on…"
---

# CSS lint

Three things that are wrong in any stylesheet, regardless of taste.

<GateMeta id="css_lint" severity="warning" tags="frontend,quality" scope="Tracked files (`git ls-files`)" />

## What it checks

Hand-rolled scan of tracked .css/.scss/.less/.sass/.styl files (skipping .min.css). Fires for: hidden scrollbar (display:none on ::-webkit-scrollbar), thin font-weight (100/200) on body-text selectors, text-align: justify. Inline override via `/* l0git: ignore <rule_id> reason: … */`.

Scans tracked `.css`, `.scss`, `.less`, `.sass` and `.styl` files.
`.min.css` is skipped.

| Rule | Severity | Fires when |
|---|---|---|
| `thin_font_weight` | warning | `font-weight: 100` or `200` on body-text selectors |
| `justified_text` | warning | `text-align: justify` |
| `hidden_scrollbar` | info | `display: none` on `::-webkit-scrollbar` |

`hidden_scrollbar` reports at info because it is legitimate when the element
has `overflow: hidden` — there is no scrollbar to hide — or when a custom
scrollbar replaces it.

Inline override: `/* l0git: ignore <rule_id> reason: … */`

## What a finding says

```text
site.css:212 text-align: justify. Justified text on the web (without sophisticated hyphenation) creates rivers of whitespace and hurts readability. Use text-align: left.
```

## Options

```json
{
  "gate_options": {
    "css_lint": { "disabled_rules": ["hidden_scrollbar"] }
  }
}
```

## Turning it off

Silence the gate for the whole project in `.l0git.json`:

```json
{
  "ignore": ["css_lint"]
}
```

Or keep it running at a lower severity:

```json
{
  "severity": { "css_lint": "info" }
}
```

For a single occurrence, prefer the inline directive — it records the reason next to the code:

```text
/* l0git: ignore <rule_id> reason: … */
```

## See also

- [HTML lint](/gates/html-lint)
