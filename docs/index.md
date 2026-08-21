---
layout: home

hero:
  name: "l0-git"
  text: "Quality gates that mean the same thing everywhere"
  tagline: "35 deterministic checks over your repository — one Go binary, shared by your shell, your editor and your coding agent."
  image:
    src: /logo.svg
    alt: l0-git
  actions:
    - theme: brand
      text: Get started
      link: /guide/getting-started
    - theme: alt
      text: Browse the gates
      link: /gates/
    - theme: alt
      text: GitHub
      link: https://github.com/fabriziosalmi/l0-git

features:
  - title: Deterministic by construction
    details: A gate fires only when the violation is a binary condition over the file system, the git index or a parse tree. Same tree, same findings, on any machine — no model, no network, no threshold to tune.
  - title: 35 gates, tuned against real repositories
    details: Secrets, git hygiene, containers, accessibility, governance, docs. Current defaults come out of an adversarial sweep across 220 real repositories, so the noisy categories are already off.
  - title: One binary, one store
    details: Pure Go, no CGO. The CLI, the MCP server and the VS Code extension are three front ends over the same SQLite file — ignore a finding in the editor and CI agrees.
  - title: Built for agents, not just humans
    details: An MCP server with eight tools, and a remediation path that says out loud whether a fix is deterministic or needs judgement.
  - title: Fixes you can read before you run them
    details: lgit fix prints exact commands, file edits and a verification step. It never executes anything — applying is always your call.
  - title: Configured per project, overridden per line
    details: .l0git.json to ignore a gate or change its severity, and inline directives that record why this one line is different.
---

## See it work

Three commands against a small broken project — real output, nothing staged:

![Terminal session: lgit check reports 35 gates and 19 findings, lgit list filters to the two errors, and lgit fix prints a deterministic recipe with the exact git commands to run.](/demo-cli.svg)

`lgit check` runs the gates and persists findings. `lgit list` filters them.
`lgit fix` explains one — and for the eight gates with deterministic recipes,
tells you the exact commands rather than a paragraph of advice.

## Install

```sh
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m); [ "$ARCH" = x86_64 ] && ARCH=amd64; [ "$ARCH" = aarch64 ] && ARCH=arm64

curl -fsSL "https://github.com/fabriziosalmi/l0-git/releases/latest/download/lgit-$OS-$ARCH.tar.gz" | tar xz
sudo mv "lgit-$OS-$ARCH" /usr/local/bin/lgit

lgit check .
```

Then [wire it into Claude Code](/guide/mcp) or install the
[VS Code extension](/guide/vscode).
