---
title: "Network scan"
description: "Scans tracked files for IPv4 literals, CIDRs, and ASN references. Public addresses get warning severity, private/loopback/doc ranges get info."
---

# Network scan

Surfaces hardcoded IPv4 literals, CIDR blocks and ASN references. Almost always informational — the point is to know where they are.

<GateMeta id="network_scan" severity="info" tags="security,network" scope="Tracked files (`git ls-files`)" />

## What it checks

Scans tracked files for IPv4 literals, CIDRs, and ASN references. Public addresses get warning severity, private/loopback/doc ranges get info.

### Classification

Every parsed address is classified, and the category decides the severity:

| Category | Severity | Example |
|---|---|---|
| `public` | warning | a routable, allocated address |
| `private` | info | `10.0.0.0/8`, `192.168.0.0/16` |
| `loopback` | *(not reported by default)* | `127.0.0.1`, `::1` |
| `unspecified` | *(not reported by default)* | `0.0.0.0`, `::` |
| `link-local` | info | `169.254.0.0/16` |
| `doc-range` | *(dropped)* | `192.0.2.0/24` and the other RFC 5737 ranges |
| `doc-placeholder` | info | `1.2.3.4`, `4.3.2.1` — sequential octets |
| `public-resolver` | info | `8.8.8.8`, `1.1.1.1`, `9.9.9.9` |
| `broadcast` / `multicast` / `reserved` | info | |

### Why loopback and 0.0.0.0 are off by default

Both were measured as the two largest sources of non-actionable findings on a
220-repository sweep. A loopback literal is a local-dev default, and inside a
container `--host 0.0.0.0` is the *only* correct bind — the gate cannot see
enough to distinguish that from a service exposed on a public host. Turn them
on for projects that run services directly on the host.

## What a finding says

```text
IPv4 203.0.113.24 found in src/config.ts:18 (public). Hardcoded public addresses drift; move it to configuration.
```

## Options

```json
{
  "gate_options": {
    "network_scan": {
      "report_loopback": true,
      "report_unspecified": true
    }
  }
}
```

## Turning it off

Silence the gate for the whole project in `.l0git.json`:

```json
{
  "ignore": ["network_scan"]
}
```

Or keep it running at a lower severity:

```json
{
  "severity": { "network_scan": "info" }
}
```

## See also

- [Connection strings](/gates/connection-strings)
