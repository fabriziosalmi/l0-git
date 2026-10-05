---
title: "Network scan"
description: "Scans tracked files for IPv4 literals, CIDRs, and ASN references. Public addresses get warning severity, private/loopback/doc ranges get info."
---

# Network scan

Surfaces hardcoded IPv4 literals, CIDR blocks and ASN references. Almost always informational: the point is to know where they are.

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
| `doc-placeholder` | info | `1.2.3.4`, `4.3.2.1`: sequential octets; and invented test addresses: `100.1.2.3`, `100.4.5.6`, `2.2.2.2`, `100.1.1.1` |
| `public-resolver` | info | `8.8.8.8`, `1.1.1.1`, `9.9.9.9` |
| `broadcast` / `multicast` / `reserved` | info | |

### What is not an address

A dotted quad is byte-for-byte the same as a version number or a section number,
so only the text around it can tell them apart. These are not reported:

- **Versions**: `Chrome/120.0.0.0`, `brotlicffi==1.2.0.1`, `version="0.0.1.0"`.
- **Section numbers** quoted from a standard: `RFC 6749 §4.1.2.1`,
  `Req 4.2.1.1`, `section 4.2.1.1`, `clause 3.1.2.1`. Both conditions are
  required: the keyword directly before it, **and** every component at most 30,
  so `req 45.33.32.156 GET /` and `section 51.222.140.163` are still reported.
- **The version pins of package-manager lockfiles**: `uv.lock`, `pdm.lock`,
  `pixi.lock`, `bun.lock`, `deno.lock`, `mix.lock`, `pubspec.lock`, `Podfile.lock`,
  `Package.resolved`, `.terraform.lock.hcl` and the like, on top of the older
  `package-lock.json`, `yarn.lock`, `Cargo.lock`, `go.sum`, `poetry.lock`. Only
  `network_scan` skips the newer ones: a lockfile can record a git URL verbatim,
  credentials included, so `secrets_scan` and `connection_strings` still read
  them. Exact names; a file that merely ends in `.lock` is still scanned.

Invented test addresses: every octet equal, the last three equal, or the last
three in a run of step 1 (`100.1.2.3`, `100.4.5.6`, `2.2.2.2`): are reported as
`doc-placeholder` at info rather than as a warning. They are still listed: the
range may really be allocated. (A run of step 10, `100.10.20.30`, was a placeholder
rule for a while and was removed: `52.20.30.40` is a real AWS address.)

The same rules apply to a network written as `a.b.c.0/24` or narrower, which
`isSyntheticOctets` cannot see (it needs a non-zero last octet): `1.2.3.0/24`,
`3.3.3.0/24` and `3.2.1.0/24` are `doc-placeholder`, and a resolver provider's own
prefix (`1.1.1.0/24`, `8.8.8.0/24`, `9.9.9.0/24`) is `public-resolver`, both at
info. A wide prefix is never softened (`1.0.0.0/8` contains `1.1.1.1` and is still a
public range), and neither is any other public network.

### Why loopback and 0.0.0.0 are off by default

Both were measured as the two largest sources of non-actionable findings on a
220-repository sweep. A loopback literal is a local-dev default, and inside a
container `--host 0.0.0.0` is the *only* correct bind: the gate cannot see
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
