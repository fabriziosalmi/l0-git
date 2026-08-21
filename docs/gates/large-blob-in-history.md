---
title: "Large blob in history"
description: "Walks every blob reachable from any ref and reports those above the configured threshold (default 5 MiB). Catches files that bloat .git even after…"
---

# Large blob in history

Finds the files that are still making your clone slow long after you deleted them.

<GateMeta id="large_blob_in_history" severity="warning" tags="git-hygiene,history" scope="Every blob reachable from any ref" />

## What it checks

Walks every blob reachable from any ref and reports those above the configured threshold (default 5 MiB). Catches files that bloat .git even after deletion from the working tree. Opt-in (gate_options.large_blob_in_history.enabled = true).

Walks every blob reachable from any ref and reports those above the
threshold, whether or not the path still exists in the working tree.
**Opt-in.**

```sh
git filter-repo --strip-blobs-bigger-than 5M
```

## What a finding says

```text
Blob 3f9a2c1 (path assets/old-demo.mov, 118 MiB, threshold 5 MiB) lives in .git even if it's no longer in the working tree. Big blobs in history bloat clones — purge with `git filter-repo --strip-blobs-bigger-than 5M`.
```

## Options

```json
{
  "gate_options": {
    "large_blob_in_history": { "enabled": true, "threshold_mb": 5 }
  }
}
```

`threshold_mb` defaults to **5**.

## Turning it off

Silence the gate for the whole project in `.l0git.json`:

```json
{
  "ignore": ["large_blob_in_history"]
}
```

Or keep it running at a lower severity:

```json
{
  "severity": { "large_blob_in_history": "info" }
}
```

## See also

- [Large file tracked](/gates/large-file-tracked)
- [Secrets scan (history)](/gates/secrets-scan-history)
