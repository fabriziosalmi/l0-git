# Security Policy

## Supported Versions

l0-git is pre-1.0. Only the latest commit on `main` receives security fixes.

## Reporting a Vulnerability

Please do **not** open a public GitHub issue for security problems.

Email: **fabrizio.salmi@gmail.com** with:
- A clear description of the issue
- Steps to reproduce
- Affected version / commit hash
- Any proposed mitigation

You can expect an acknowledgement within 7 days. Coordinated disclosure is appreciated; once a fix is released, credit will be given in the changelog unless you prefer to remain anonymous.

## Threat model

- The `lgit` binary stores findings in a local SQLite file at `~/.l0-git/findings.db` (or `$LGIT_DB`).
- The MCP server reads/writes via stdio — no network listener, no authentication beyond OS file permissions.
- Gates only **read** project files; they never modify them.
- The VSCode extension shells out to `lgit` and is bound to the user's local environment.
- Findings (titles, messages, file paths) are stored in plaintext. If a future gate scans for secrets, the *fact* that a secret was found is recorded along with file path and line number — but never the secret value itself.
