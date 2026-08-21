---
title: "SECURITY policy present"
description: "A SECURITY.md tells users how to responsibly disclose vulnerabilities."
---

# SECURITY policy present

A SECURITY.md is the difference between a researcher mailing you privately and a researcher opening a public issue with a working exploit.

<GateMeta id="security_present" severity="info" tags="project-hygiene,security" scope="Project root" />

## What it checks

A SECURITY.md tells users how to responsibly disclose vulnerabilities.

Accepted: `SECURITY`, `SECURITY.*`. GitHub also surfaces this file in the repo's Security tab.

## What a finding says

```text
No SECURITY file at the project root. Add SECURITY.md with a contact and disclosure process.
```

## Turning it off

Silence the gate for the whole project in `.l0git.json`:

```json
{
  "ignore": ["security_present"]
}
```

Or keep it running at a lower severity:

```json
{
  "severity": { "security_present": "info" }
}
```

## See also

- [Secrets scan](/gates/secrets-scan)
