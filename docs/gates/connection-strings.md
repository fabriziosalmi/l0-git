---
title: "Connection strings"
description: "Scans tracked files for connection URIs (legacy schemes like FTP/Telnet/SMB/NFS/rsync, database schemes like MongoDB/Postgres/MySQL/Redis, JDBC…"
---

# Connection strings

Finds connection URIs in tracked source — legacy plaintext protocols, database URIs, and anything carrying inline credentials.

<GateMeta id="connection_strings" severity="info" tags="security,network" scope="Tracked files (`git ls-files`)" />

## What it checks

Scans tracked files for connection URIs (legacy schemes like FTP/Telnet/SMB/NFS/rsync, database schemes like MongoDB/Postgres/MySQL/Redis, JDBC, plain HTTP, plain LDAP). URIs with inline credentials are reported as errors.

Reported categories: `creds_in_url`, `ftp`, `telnet`, `smb`, `nfs`,
`rsync`, `ldap_unencrypted`, `jdbc`, `db_uri`, `http_remote`.

A URI that carries **inline credentials** (`scheme://user:password@host`) is
raised to error severity — that is a leaked secret, not a protocol preference.
Everything else reports at info: a `http://` link in a comment is worth seeing,
not worth blocking a release over.

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
