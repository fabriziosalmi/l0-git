---
name: Feature request
about: Suggest an idea
title: "feat: "
labels: enhancement
---

## What problem does this solve?
Describe the user-facing problem, not the proposed solution.

## Proposed change
What you would like the project to do.

## Alternatives considered
Other approaches you thought about, and why this one is preferable.

## Notes
- Keep in mind the project's "intentionally small" scope — stdlib +
  `modernc.org/sqlite` on the Go side, no bundler on the TS side.
- New gates should be auditable in <50 lines and rely only on the filesystem
  (or `git`) — no language-specific parsers, no network calls.
