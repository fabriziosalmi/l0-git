---
title: "Connection strings"
description: "Scans tracked files for connection URIs (legacy schemes like FTP/Telnet/SMB/NFS/rsync, database schemes like MongoDB/Postgres/MySQL/Redis, JDBC…"
---

# Connection strings

Finds connection URIs in tracked source — legacy plaintext protocols, database URIs, and anything carrying inline credentials.

<GateMeta id="connection_strings" severity="info" tags="security,network" scope="Tracked files (`git ls-files`)" />

## What it checks

Scans tracked files for connection URIs (legacy schemes like FTP/Telnet/SMB/NFS/rsync, database schemes like MongoDB/Postgres/MySQL/Redis, JDBC, plain HTTP, plain LDAP). URIs with inline credentials are reported as errors.

Each category carries its own severity:

| Category | Severity | Fires on |
|---|---|---|
| `creds_in_url` | error | `scheme://user:password@host` with a literal password |
| `ftp` | warning | `ftp://` |
| `telnet` | warning | `telnet://` |
| `smb` | warning | `smb://` |
| `nfs` | warning | `nfs://` |
| `rsync` | warning | `rsync://` |
| `ldap_unencrypted` | info | `ldap://` — not `ldaps://` |
| `jdbc` | info | `jdbc:<driver>:…` |
| `db_uri` | info | MongoDB, Postgres, MySQL, MariaDB, Redis, AMQP, Kafka, MSSQL, CouchDB, Cassandra |
| `http_remote` | info | plain `http://` to a remote host |

Three tiers, three reasons. **Credentials** are a leaked secret, whatever the
protocol. The **legacy cleartext protocols** are a transport choice that moves
data unauthenticated and unencrypted, and is worth changing. **Database URIs,
JDBC, LDAP and plain HTTP** are worth seeing but are mostly configuration, docs
and links — reporting them above info would bury the two tiers above.

### Not reported

- A password that is a template, not a value: `${DB_PASS}`, `$DB_PASS`, `%s`,
  `<pass>`, `{{ pass }}`, Python `{passwd}` / `{settings.DB_PASS}`, Ruby
  `#{pass}`. The field has to be the whole password — `pa{ss}word` still fires.
- A scheme named in prose or inside a pattern, with nothing after `://` to
  connect to: `` `ftp://` ``, `(?:https?://|ftp://)`.
- Well-known quickstart defaults where **both** user and password come from the
  default set (`postgres:postgres`, `guest:guest`).
- `http://` to spec and namespace identifiers (`http://www.w3.org/2000/svg`) and
  to local or container-internal hosts.

## What a finding says

```text
Credentials in URL in src/db.py:9. A URI carrying user:password leaks the credential to anyone who reads the file.
```

## Turning it off

Silence the gate for the whole project in `.l0git.json`:

```json
{
  "ignore": ["connection_strings"]
}
```

Or keep it running at a lower severity:

```json
{
  "severity": { "connection_strings": "info" }
}
```

## See also

- [Secrets scan](/gates/secrets-scan)
- [Network scan](/gates/network-scan)
