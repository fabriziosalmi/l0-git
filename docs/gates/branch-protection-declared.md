---
title: "Branch protection declared"
description: "Verifies the repo tracks branch-protection rules as code via .github/settings.yml (Probot Settings format). Cannot verify the actual GitHub…"
---

# Branch protection declared

Checks that branch protection is declared as code. It cannot check that branch protection is actually on — and it says so.

<GateMeta id="branch_protection_declared" severity="info" tags="governance,security" scope="Project root" />

## What it checks

Verifies the repo tracks branch-protection rules as code via .github/settings.yml (Probot Settings format). Cannot verify the actual GitHub server-side state — that's reachable only via the REST API with auth. Opt-in (gate_options.branch_protection_declared.enabled = true) so users who manage protection via the UI don't get a false signal.

::: warning What this gate can and cannot see
Real branch-protection state lives on GitHub's servers and is reachable only
through the authenticated REST API. From the filesystem, the only thing that
can be verified deterministically is whether the repository *tracks* protection
as code.
:::

Detection scope is one canonical convention, by design:
`.github/settings.yml` in the [Probot Settings](https://github.com/repository-settings/app)
format, carrying at least one `branches:` entry with a `protection:` block.

Terraform and Pulumi providers, GitHub native rulesets, and ad-hoc `gh api`
scripts are **not** recognised. That is why the gate is opt-in and reports at
info: projects managing protection another way would otherwise get a signal
that is simply wrong.

## What a finding says

```text
no .github/settings.yml tracked. l0-git can only verify protection-as-code (Probot Settings format) — actual branch-protection rules live server-side on GitHub and aren't readable from the filesystem.
```

## Options

```json
{
  "gate_options": {
    "branch_protection_declared": { "enabled": true }
  }
}
```

## Turning it off

Silence the gate for the whole project in `.l0git.json`:

```json
{
  "ignore": ["branch_protection_declared"]
}
```

Or keep it running at a lower severity:

```json
{
  "severity": { "branch_protection_declared": "info" }
}
```

## See also

- [CODEOWNERS present](/gates/codeowners-present)
