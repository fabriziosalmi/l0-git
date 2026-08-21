---
title: "Secrets scan (history)"
description: "Walks every blob reachable from any ref and scans its content for the same patterns as secrets_scan. Catches secrets that were committed and later…"
---

# Secrets scan (history)

Deleting a secret from the working tree does not delete it from `.git`. This gate reads what is still in there.

<GateMeta id="secrets_scan_history" severity="warning" tags="security,history" scope="Every blob reachable from any ref" />

## What it checks

Walks every blob reachable from any ref and scans its content for the same patterns as secrets_scan. Catches secrets that were committed and later removed from the working tree but still live in .git/objects. Opt-in (set gate_options.secrets_scan_history.enabled = true) because the walk is slow on big repos.

Walks every blob reachable from any ref and applies the same pattern
set as [secrets_scan](/gates/secrets-scan). A hit means the credential is
recoverable by anyone who has ever cloned the repository.

**Opt-in**, because the walk is slow on large repositories.

If the scan hits its blob cap it says so explicitly, in a finding of its own:
the oldest commits were *not* scanned, and a clean result under a cap is not a
clean result.

### Remediating

Rotate the credential first. Purging history is secondary and cannot un-leak
anything that was already cloned:

```sh
git filter-repo --invert-paths --path <path>
```

## What a finding says

```text
Possible AWS access key ID in blob 8c14ef0 (path config/deploy.sh, line 12). The secret is in repo history even if removed from the working tree — rotate the credential, then run `git filter-repo --invert-paths --path config/deploy.sh` (or BFG).
```

## Options

```json
{
  "gate_options": {
    "secrets_scan_history": {
      "enabled": true,
      "max_blobs": 5000,
      "max_blob_size_mb": 2
    }
  }
}
```

| Option | Default | Meaning |
|---|---|---|
| `enabled` | `false` | The gate does nothing until this is `true` |
| `max_blobs` | `5000` | Stop after this many unique blobs |
| `max_blob_size_mb` | `2` | Skip blobs larger than this |

## Turning it off

Silence the gate for the whole project in `.l0git.json`:

```json
{
  "ignore": ["secrets_scan_history"]
}
```

Or keep it running at a lower severity:

```json
{
  "severity": { "secrets_scan_history": "info" }
}
```

## See also

- [Secrets scan](/gates/secrets-scan)
- [Large blob in history](/gates/large-blob-in-history)
