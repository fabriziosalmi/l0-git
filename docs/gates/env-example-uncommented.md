---
title: "Uncommented .env.example key"
description: "For every .env.example / .env.sample / .env.template / .env.dist, every KEY= line must have a `# …` comment either inline or on the line above. A…"
---

# Uncommented .env.example key

A list of bare `KEY=` lines tells a new contributor nothing about what to put in them.

<GateMeta id="env_example_uncommented" severity="info" tags="documentation" scope="Tracked files (`git ls-files`)" />

## What it checks

For every .env.example / .env.sample / .env.template / .env.dist, every KEY= line must have a `# …` comment either inline or on the line above. A list of bare keys with no context is a broken contract for new contributors.

Applies to `.env.example`, `.env.sample`, `.env.template` and
`.env.dist`. Every `KEY=` line must carry a `# …` comment either inline or on
the line directly above.

This gate ships one of the eight deterministic remediation recipes — `lgit fix`
will tell you exactly which line to annotate.

## What a finding says

```text
.env.example:4 DATABASE_URL has no preceding or inline `# …` comment. Without context, contributors don't know what to fill in.
```

## Turning it off

Silence the gate for the whole project in `.l0git.json`:

```json
{
  "ignore": ["env_example_uncommented"]
}
```

Or keep it running at a lower severity:

```json
{
  "severity": { "env_example_uncommented": "info" }
}
```

## See also

- [Secrets scan](/gates/secrets-scan)
