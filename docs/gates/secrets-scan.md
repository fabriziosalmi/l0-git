---
title: "Secrets scan"
description: "Scans every git-tracked file for AWS/GitHub/OpenAI/Anthropic/Google/Slack/Stripe API keys, JWTs, private-key headers, and tracked .env files. Honours…"
---

# Secrets scan

Scans tracked files for credential shapes that are unambiguous enough to act on. One of only two gates that report at **error** severity.

<GateMeta id="secrets_scan" severity="error" tags="security,git-hygiene" scope="Tracked files (`git ls-files`)" />

## What it checks

Scans every git-tracked file for AWS/GitHub/OpenAI/Anthropic/Google/Slack/Stripe API keys, JWTs, private-key headers, and tracked .env files. Honours .gitignore via git ls-files.

### Detected patterns

| Pattern | Shape |
|---|---|
| AWS access key ID | `AKIA` + 16 upper-case alphanumerics |
| GitHub PAT (classic) | `ghp_` / `ghs_` / `gho_` / `ghr_` / `ghu_` + 36 chars |
| GitHub PAT (fine-grained) | `github_pat_` + 82 chars |
| OpenAI API key | `sk-` + 48 alphanumerics |
| Anthropic API key | `sk-ant-` + 40 or more chars |
| Google API key | `AIza` + 35 chars |
| Slack token | `xox[abprs]-` + the full numeric-segment shape |
| Stripe live secret key | `sk_live_` + 24 or more chars |
| JWT | three `base64url` segments, the first two starting `eyJ` |
| Private key | a `-----BEGIN … PRIVATE KEY-----` header |

Every pattern except the private-key header also requires a **Shannon entropy of
at least 3.5** over the match. That is what separates a real key from
`AKIAIOSFODNN7EXAMPLE`-style filler in documentation.

A tracked `.env` file is reported on its own, regardless of contents.

### Scope

The gate enumerates files with `git ls-files`, so anything already ignored is
never read. Files above 2 MiB and detected binaries are skipped. Test fixtures
and data files are skipped by default — see [Configuration](/guide/configuration#scan-options).

::: warning A finding is not proof
The entropy floor cuts most filler, but the only thing the gate can prove is
that a string *has the shape* of a credential. Verify before you rotate — and
if it is real, remember that removing it from the working tree does not remove
it from history. See [secrets_scan_history](/gates/secrets-scan-history).
:::

## What a finding says

```text
.env is tracked in git. .env files typically hold secrets and shouldn't be committed. Move secrets to a vault and add .env to .gitignore.
```

## Turning it off

Silence the gate for the whole project in `.l0git.json`:

```json
{
  "ignore": ["secrets_scan"]
}
```

Or keep it running at a lower severity:

```json
{
  "severity": { "secrets_scan": "info" }
}
```

## See also

- [Secrets scan (history)](/gates/secrets-scan-history)
- [Connection strings](/gates/connection-strings)
