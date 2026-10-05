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
protocol — including a password-only one like `redis://:password@host`. The **legacy cleartext protocols** are a transport choice that moves
data unauthenticated and unencrypted, and is worth changing. **Database URIs,
JDBC, LDAP and plain HTTP** are worth seeing but are mostly configuration, docs
and links — reporting them above info would bury the two tiers above.

### Credentials to a host nobody else can reach

A `creds_in_url` finding drops from error to **warning** when the host is
`localhost`, a loopback address, or a single-label name such as a
docker-compose service (`db`, `postgres`, `redis`):

```text
postgresql://app:app_dev_password@db:5432/app          → warning
postgresql://app:app_dev_password@db-prod.internal/app → error
```

It is still reported. The password is in the repository and passwords get
reused; what changes is how far the leak reaches. Private addresses,
`.internal` and `.local` stay at error — anyone on that network can use the
credential — and so does a templated host like `${DB_HOST}`, which can resolve
to production.

### Not reported

- A password that is a template, not a value: `${DB_PASS}`, `$DB_PASS`, `%s`,
  `<pass>`, `{{ pass }}`, Python `{passwd}` / `{settings.DB_PASS}`, Ruby
  `#{pass}`. The field has to be the whole password — `pa{ss}word` still fires.
- A scheme named in prose or inside a pattern, with nothing after `://` to
  connect to: `` `ftp://` ``, `(?:https?://|ftp://)`.
  Also a mention with nothing connectable after it: `ftp://,` in a list of
  schemes, or a regular expression (`ftp://127\.0\.0\.1!`).
- A legacy scheme (`ftp`, `telnet`, `smb`, `nfs`, `rsync`, `ldap`) that names **this
  machine**: `localhost`, `*.localhost`, `0.0.0.0` or a loopback address. Nothing
  leaves the host. A private address, `.lan`, a bare service name and
  `127.0.0.1.evil.com` are other machines and are still reported; so is a
  credential in the URL, whatever the host.
- Well-known quickstart defaults where **both** user and password come from the
  default set (`postgres:postgres`, `guest:guest`).
- `http://` to spec and namespace identifiers (`http://www.w3.org/2000/svg`) and
  to local or container-internal hosts.
- `http://` that is cleartext **by design**:
  - certificate-chain and revocation fetches — a path ending `.crt`, `.cer`,
    `.crl`, `.p7b` or `.p7c`, or an `ocsp.` host **at its root path** (RFC 5280).
    `.pem`, `.p12`, `.pfx` and `.der` are *not* exempt, they can be private keys,
    and `http://ocsp.acme-cdn.io/install.sh` is a download that borrowed the label;
  - license and schema identifiers quoted in source headers — the host **and the
    first path segment, matched whole, after the path is cleaned**:
    `http://www.apache.org/licenses/…`, `http://www.gnu.org/licenses/…`,
    `http://scripts.sil.org/OFL`, `http://json-schema.org/draft-07/schema#`. So
    `http://www.apache.org/dist/…zip`, `…/licenses/../dist/…` and
    `http://www.mozilla.org/mplayer-setup.exe` are still reported;
  - the stock fake adversary of a security test: `evil.com`, `sub.attacker.com`,
    `malicious.io`, `yourserver.com`. A host whose first label is a number
    (`192.168.evil.net`, `10.evil.com`, `0177.0.0.1.evil.com`, `0x7f.evil.com`) is
    never an example.
- A pattern that greps for credentials, such as a pre-commit hook containing
  `postgresql://[^:]+:[^@]+@|sk_(test|live)_` — regex syntax where a URL should be.
- A one- or two-**character** password — `scheme://u:p@host`, `user:…@…` in prose.
  It is the password that counts, not the user: `sa` (SQL Server's default login)
  or `x` in front of a real password or token is reported, and so is a token in the
  **user** slot with a one-character password (`https://<token>:x@github.com`).
  (A literal `<password>` or `<token>` is a placeholder and is not reported.)

### Where the password stops

The userinfo ends at the **last** `@` before the path, so `user:p@ss@host` has the
password `p@ss` and `P@ssw0rd#2024` is read whole. A `?` or `#` makes that
ambiguous — it may start a query, or be part of the password — and is settled by
what lies between it and the last `@`: a `key=value` query ends the userinfo before
it (`https://user:pw@host?x=a@b`), anything else does not. Where the evidence is
silent the reading that keeps the password in view wins, because hiding a
credential is the failure that matters.

## What a finding says

```text
postgres://admin:***@db.prod.acme.io:5432/app in src/db.py:9. Remove the inline user:password from the URL — read it from a vault, env var, or secret manager instead. Also rotate, since the URL has been committed.
```

The finding says **where** the credential is, never **what** it is. The password
is always shown as `***` — in the CLI output, over MCP, in the editor's Problems
pane and hover, and in the store. So is the password of every URL in a list, an
Oracle JDBC `user/password@host`, a token used as the whole user
(`https://<token>@host`), and the value of a credential-looking parameter
(`?password=`, `;pwd=`, `#access_token=`, `bindpw=`, …). `secrets_scan` works the
same way.

Two things are deliberately left alone: a password that contains a `/` (a URL
cannot carry one), and a short or letters-only user (`ssh://git@host`).

This matters because a finding outlives the thing it reports: a copy of the
password in the findings store would still be there after you removed it from
the repository. Rotate the credential; the finding tells you where to look.

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
