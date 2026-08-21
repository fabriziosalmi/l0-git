---
title: What is l0-git?
description: Deterministic project-hygiene quality gates — one Go binary, a CLI, an MCP server and a VS Code extension over one findings store.
---

# What is l0-git?

l0-git checks a repository for the things that are true or false about it —
a missing license, a tracked `.env`, a `FROM node:latest`, an unresolved merge
marker — and keeps the results in one place that your editor, your shell and
your coding agent all read from.

It is a single Go binary. No CGO, no Python, no rule engine, no YAML DSL.

## The rule that decides what gets in

> A gate fires if and only if the violation can be expressed as a binary,
> mathematically unambiguous condition over the file system, the git index, or
> a parse tree.

That constraint is what the whole project is organised around, and it is worth
being precise about what it does and does not buy you.

**What it buys you.** Results are reproducible: the same tree produces the same
findings on any machine, in any order, with no model, no network and no
threshold to tune. `secrets_scan` finding a `-----BEGIN PRIVATE KEY-----` header
is not an opinion. Neither is `merge_conflict_markers`. You can wire these into
CI and trust that a red build means something changed.

**What it does not buy you.** Determinism is not the same as being right about
intent. A gate can be perfectly deterministic about the bytes and still wrong
about what they mean — a mock AWS key in a test fixture, a `0.0.0.0` bind that
is the only correct choice inside a container, a `docker.sock` mount in a
service whose entire job is to talk to Docker.

Those are false positives, and l0-git has had plenty. The current defaults come
out of an adversarial sweep against 220 real repositories, and a good number of
gates carry carve-outs that exist only because a rule that was technically
correct was practically noise. Several categories are off by default for
exactly this reason — see [network scan](/gates/network-scan) and
[scan options](/guide/configuration#scan-options).

Anything needing genuine context understanding is deliberately out of scope for
a gate. It goes through the [remediation path](/guide/mcp), where an LLM can
weigh it — and where being wrong costs a suggestion rather than a red build.

## How the pieces fit

![Architecture: the VS Code extension, MCP clients and the shell all drive one lgit binary, which reads the working tree and the git index and persists findings to a shared SQLite store.](/architecture.svg)

- **The engine** — `lgit`, a single binary holding all 35 gates and the
  findings store.
- **The CLI** — `lgit check`, `list`, `fix`, … Everything prints JSON, so it
  pipes into `jq` and into CI.
- **The MCP server** — `lgit mcp` over stdio, exposing eight tools so an agent
  can run gates and read remediations.
- **The VS Code extension** — sidebar tree, Problems pane, status bar, and an
  overview dashboard.

They are not three products sharing a name. They are three front ends over one
binary and one SQLite file, which is why a finding you ignore in the editor
stays ignored when CI runs.

## Findings have a life cycle

A finding is keyed by `(project, gate_id, file_path)`. Re-running a gate does
not append duplicates — it updates what is already there, and a violation that
went away flips to `resolved` rather than vanishing. `ignored` is sticky: mark
something once and later runs will not resurface it.

## What next

- [Getting started](./getting-started) — install and first scan.
- [Gate reference](/gates/) — all 35 gates.
- [Configuration](./configuration) — `.l0git.json`, severities, scan options.
