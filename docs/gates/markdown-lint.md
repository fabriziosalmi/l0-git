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

### Which links count as broken

A relative link is checked against the working tree, with the allowances a
renderer makes:

- **Extensionless pages**: `./configuration` finds `configuration.md` or
  `configuration/index.md`. A dotted version number is not an extension
  (`./remediation-v11.2`, `./release-1.0`), but that reading is only taken under a
  site generator (VitePress, MkDocs, Docusaurus, Jekyll, Hugo, …); on GitHub such
  a link is broken. A real file type (`./diagram.png`) never falls back to a page.
- **GitHub's own pages**: a README is rendered at `/owner/repo/blob/<branch>/`, so
  `../../issues` from the repository root is `github.com/owner/repo/issues`. The
  number of `..` must be the file's directory depth **plus two** (four from
  `docs/guide/`) and the next segment one of `issues`, `pulls`, `actions`, `wiki`,
  `releases`, `security`, `discussions`, `projects`, `tags`, `labels`,
  `milestones`, `pulse`, `graphs`, `stargazers`, `watchers`, `forks`, `compare`,
  `commits`, `branches`. `../../docs/guide.md` and a wrong depth stay broken.

An anchor matches a heading the way GitHub numbers them: the second `## Usage` is
`#usage-1`, the third `#usage-2`. `#` alone is the top of the page, and a
percent-encoded anchor (`#caf%C3%A9`) is decoded before it is compared.

### When a block is not a defect

`codeblock_invalid_payload` answers "does the snippet parse?", and documentation
has four honest reasons for a snippet that does not. None is reported:

- **An excerpt** — the members of an object without its braces.
- **A stream** of JSON values one after another (`{"detail": "a"}` then
  `{"detail": "b"}`): how a troubleshooting page lists alternative responses.
- **Comments and type placeholders** in JSON: `"features": {…}   // optional` and
  `"tenants": <integer>`. They are removed *outside string literals only*, a
  placeholder only where a JSON **value** belongs (after `:`, `,` or `[` — an
  `<html>` tag on its own is not one), and the block is accepted only if what is
  left parses strictly — one with a real syntax error as well is still reported.
- **A block labelled as the wrong way to do it**: its **first line** is a
  `# Bad: …` / `# Wrong: …` / `# Incorrect: …` / `# Invalid: …` comment, or the line
  directly above it (one blank line allowed) is only a label — `**Bad**:`,
  `### Wrong`, `❌ Bad example`, `Don't:`. A comment in the *middle* of a block, such as
  `# Don't: expose 5432 publicly`, is advice about the configuration and does not
  excuse the block.

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
