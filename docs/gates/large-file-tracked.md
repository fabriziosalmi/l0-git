---
title: "Large file tracked"
description: "Flags files in `git ls-files` larger than the configured threshold (default 5 MiB). Tune via gate_options.large_file_tracked.threshold_mb in…"
---

# Large file tracked

Large binaries in git are permanent: every clone pays for them forever, even after you delete them.

<GateMeta id="large_file_tracked" severity="warning" tags="git-hygiene" scope="Tracked files (`git ls-files`)" />

## What it checks

Flags files in `git ls-files` larger than the configured threshold (default 5 MiB). Tune via gate_options.large_file_tracked.threshold_mb in .l0git.json.

## What a finding says

```text
assets/demo.mp4 is 41.2 MiB tracked in git (threshold: 5 MiB). Move it to Git LFS, releases, or external storage to keep clones lean.
```

## Options

```json
{
  "gate_options": {
    "large_file_tracked": { "threshold_mb": 20 }
  }
}
```

`threshold_mb` defaults to **5**. Values below `1` are ignored and the default
applies.

## Turning it off

Silence the gate for the whole project in `.l0git.json`:

```json
{
  "ignore": ["large_file_tracked"]
}
```

Or keep it running at a lower severity:

```json
{
  "severity": { "large_file_tracked": "info" }
}
```

## See also

- [Large blob in history](/gates/large-blob-in-history)
