---
title: Gate reference
description: All 35 built-in gates, grouped by theme.
---

# Gate reference

l0-git ships **35 built-in gates**. Every one of them fires only when the
violation can be stated as a binary condition over the file system, the git
index, or a parse tree — so a finding is reproducible on any machine, and two
runs over the same tree always agree.

Three gates are **opt-in** and do nothing until you enable them in
`.l0git.json`: [Secrets scan (history)](/gates/secrets-scan-history),
[Large blob in history](/gates/large-blob-in-history) and
[Branch protection declared](/gates/branch-protection-declared).

```sh
lgit gates          # the same list, straight from the binary
```

## Project hygiene

| Gate | Severity | What it catches |
|---|---|---|
| [README present](/gates/readme-present) | Warning | A repository with no README is a repository nobody can adopt. |
| [LICENSE present](/gates/license-present) | Warning | Without a license file, default copyright applies and nobody may legally reuse the code — however open the repository looks. |
| [CONTRIBUTING present](/gates/contributing-present) | Info | Tells an outside contributor how to build, test and submit a change before they burn an afternoon guessing. |
| [SECURITY policy present](/gates/security-present) | Info | A SECURITY.md is the difference between a researcher mailing you privately and a researcher opening a public issue with a working exploit. |
| [CHANGELOG present](/gates/changelog-present) | Info | One place users can look to see what changed between releases, instead of reading the commit log. |
| [CODE_OF_CONDUCT present](/gates/code-of-conduct-present) | Info | States the rules of engagement before you need them, not during the incident. |
| [Pull request template present](/gates/pr-template-present) | Info | Standardises what a PR description contains, so reviewers stop asking the same three questions. |
| [Issue templates present](/gates/issue-template-present) | Info | Turns "it doesn't work" into a report you can actually act on. |
| [CI workflow present](/gates/ci-workflow-present) | Warning | If nothing runs the tests on push, the tests are decoration. |

## Governance

| Gate | Severity | What it catches |
|---|---|---|
| [CODEOWNERS present](/gates/codeowners-present) | Info | Routes pull requests to the people who actually own the touched paths. |
| [Branch protection declared](/gates/branch-protection-declared) | Info | Checks that branch protection is declared as code. |

## Git hygiene

| Gate | Severity | What it catches |
|---|---|---|
| [.gitignore present](/gates/gitignore-present) | Warning | The first line of defence against committing build artefacts and secrets by accident. |
| [.gitignore coverage](/gates/gitignore-coverage) | Warning | Having a `.gitignore` is not the same as having the right one. |
| [Merge conflict markers](/gates/merge-conflict-markers) | Error | An unresolved conflict marker on a shipping branch is never intentional. |
| [Large file tracked](/gates/large-file-tracked) | Warning | Large binaries in git are permanent: every clone pays for them forever, even after you delete them. |
| [Vendored directory tracked](/gates/vendored-dir-tracked) | Warning | Dependency directories are meant to be rebuilt from a manifest. |
| [Editor/IDE artefact tracked](/gates/ide-artifact-tracked) | Warning | User-local editor state does not belong in shared history. |
| [Unexpected executable bit](/gates/unexpected-executable-bit) | Warning | A `README.md` tracked as mode 100755 is a mistake that survives every clone and confuses every packaging script. |
| [File name quality](/gates/filename-quality) | Info | Filenames containing spaces or invisible characters break every shell pipeline that forgot to quote `$f`. |

## Security

| Gate | Severity | What it catches |
|---|---|---|
| [Secrets scan](/gates/secrets-scan) | Error | Scans tracked files for credential shapes that are unambiguous enough to act on. |
| [Connection strings](/gates/connection-strings) | Info | Finds connection URIs in tracked source — legacy plaintext protocols, database URIs, and anything carrying inline credentials. |
| [Network scan](/gates/network-scan) | Info | Surfaces hardcoded IPv4 literals, CIDR blocks and ASN references. |

## Git history (opt-in)

| Gate | Severity | What it catches |
|---|---|---|
| [Secrets scan (history)](/gates/secrets-scan-history) | Warning | Deleting a secret from the working tree does not delete it from `.git`. This gate reads what is still in there. |
| [Large blob in history](/gates/large-blob-in-history) | Warning | Finds the files that are still making your clone slow long after you deleted them. |

## Containers

| Gate | Severity | What it catches |
|---|---|---|
| [Dockerfile lint](/gates/dockerfile-lint) | Warning | An AST-based lint over tracked Dockerfiles — reproducibility and least privilege, nothing stylistic. |
| [Compose lint](/gates/compose-lint) | Warning | A YAML-AST lint over tracked Compose files, aimed at the settings that hand a container the host. |

## Frontend & accessibility

| Gate | Severity | What it catches |
|---|---|---|
| [HTML lint](/gates/html-lint) | Warning | Accessibility violations in tracked HTML that can be decided from the parse tree alone — no rendering, no heuristics. |
| [CSS lint](/gates/css-lint) | Warning | Three things that are wrong in any stylesheet, regardless of taste. |

## Documentation

| Gate | Severity | What it catches |
|---|---|---|
| [Markdown lint](/gates/markdown-lint) | Warning | AST lint over tracked Markdown, via goldmark. |
| [Dead placeholders](/gates/dead-placeholders) | Info | Finds the unfinished-work markers that were meant to be temporary. |
| [Uncommented .env.example key](/gates/env-example-uncommented) | Info | A list of bare `KEY=` lines tells a new contributor nothing about what to put in them. |

## Quality & release

| Gate | Severity | What it catches |
|---|---|---|
| [Tests present](/gates/tests-present) | Warning | Detects whether the project has any tests at all. |
| [Config parse error](/gates/config-parse-error) | Warning | A `package.json` or CI workflow that does not parse is a defect you can prove without running anything. |
| [Version drift](/gates/version-drift) | Warning | When two manifests in the same repository claim different versions, at least one of them is lying to whoever reads it. |
| [Missing .nvmrc / .node-version](/gates/nvmrc-missing) | Info | A `package.json` with no pinned Node version means nvm, asdf, Volta and your CI runner each pick whatever Node they happen to have. |

