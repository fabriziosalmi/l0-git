import * as path from "path";

// Stub describes a single file the extension is willing to generate when the
// user invokes "Generate stub" on a finding from one of the presence gates.
// The relative path is resolved against the project root.
export interface Stub {
  relPath: string;
  content: string;
}

// stubFor returns a Stub for the given gate ID, or null if we don't ship a
// generator for it. License/CI cases that need an extra prompt return null
// here and are handled by the extension flow directly.
export function stubFor(gateId: string, projectAbsPath: string): Stub | null {
  const projectName = path.basename(projectAbsPath);
  switch (gateId) {
    case "readme_present":
      return { relPath: "README.md", content: readmeStub(projectName) };
    case "contributing_present":
      return { relPath: "CONTRIBUTING.md", content: contributingStub(projectName) };
    case "security_present":
      return { relPath: "SECURITY.md", content: securityStub() };
    case "changelog_present":
      return { relPath: "CHANGELOG.md", content: changelogStub() };
    case "gitignore_present":
      return { relPath: ".gitignore", content: gitignoreStub() };
    case "pr_template_present":
      return { relPath: ".github/PULL_REQUEST_TEMPLATE.md", content: prTemplateStub() };
    case "issue_template_present":
      return { relPath: ".github/ISSUE_TEMPLATE/bug_report.md", content: bugTemplateStub() };
    case "ci_workflow_present":
      return { relPath: ".github/workflows/ci.yml", content: ciStub() };
    case "branch_protection_declared":
      return { relPath: ".github/settings.yml", content: probotSettingsStub() };
    default:
      return null;
  }
}

// licenseChoices is the curated set we offer when the user picks "Generate
// stub LICENSE". Each value lists a permissive top tier first.
export const licenseChoices: Array<{ label: string; description: string; spdx: string }> = [
  { label: "MIT", description: "Permissive — most common for libraries.", spdx: "MIT" },
  { label: "Apache-2.0", description: "Permissive with an explicit patent grant.", spdx: "Apache-2.0" },
  { label: "BSD-3-Clause", description: "Permissive, no patent grant.", spdx: "BSD-3-Clause" },
  { label: "GPL-3.0-or-later", description: "Strong copyleft.", spdx: "GPL-3.0-or-later" },
  { label: "MPL-2.0", description: "Weak copyleft (file-level).", spdx: "MPL-2.0" },
  { label: "Unlicense", description: "Public-domain dedication.", spdx: "Unlicense" },
];

export function licenseStub(spdx: string, holder: string): Stub {
  const year = new Date().getFullYear();
  switch (spdx) {
    case "MIT":
      return { relPath: "LICENSE", content: mitLicense(year, holder) };
    case "Apache-2.0":
      return { relPath: "LICENSE", content: apache2Header(year, holder) };
    case "BSD-3-Clause":
      return { relPath: "LICENSE", content: bsd3(year, holder) };
    case "GPL-3.0-or-later":
      return { relPath: "LICENSE", content: gplHeader(year, holder) };
    case "MPL-2.0":
      return { relPath: "LICENSE", content: mplHeader() };
    case "Unlicense":
      return { relPath: "LICENSE", content: unlicense() };
    default:
      // Fall back to MIT — every supported SPDX is enumerated above, so this
      // path is unreachable in practice; we keep it defensive for forward
      // compatibility if a caller adds a value to licenseChoices but forgets
      // the switch arm.
      return { relPath: "LICENSE", content: mitLicense(year, holder) };
  }
}

function readmeStub(name: string): string {
  return `# ${name}

> One-line description of what ${name} is.

## Install

\`\`\`sh
# install instructions here
\`\`\`

## Usage

\`\`\`sh
# minimal example
\`\`\`

## Development

\`\`\`sh
# build / test commands
\`\`\`

## License

See [LICENSE](LICENSE).
`;
}

function contributingStub(name: string): string {
  return `# Contributing to ${name}

Thanks for your interest in contributing.

## Getting started

1. Fork and clone the repo.
2. Create a feature branch.
3. Make your change with a focused commit history.
4. Run the test suite locally.
5. Open a pull request.

## Pull requests

- Keep PRs focused: one logical change per PR.
- Include test coverage for new behaviour.
- Follow the existing code style.
- Update the CHANGELOG under \`[Unreleased]\` for user-visible changes.

## Reporting bugs

Use GitHub Issues. Include:
- OS / runtime versions
- Exact steps to reproduce
- What you expected vs. what happened
`;
}

function securityStub(): string {
  return `# Security Policy

## Supported Versions

Only the latest release receives security fixes.

## Reporting a Vulnerability

Please do **not** open a public GitHub issue for security problems.

Email the maintainers privately with:
- A clear description of the issue
- Steps to reproduce
- Affected version / commit hash
- Any proposed mitigation

You can expect an acknowledgement within 7 days. Coordinated disclosure is appreciated.
`;
}

function changelogStub(): string {
  return `# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project follows
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

### Changed

### Fixed
`;
}

function gitignoreStub(): string {
  return `# OS / IDE
.DS_Store
.idea/
.vscode/
*.log

# Env
.env
.env.*

# Dependencies & build artefacts
node_modules/
dist/
build/
target/
*.test
*.out

# Local DBs
*.db
*.db-wal
*.db-shm
`;
}

function prTemplateStub(): string {
  return `## Summary
<!-- 1–3 bullets on what changes and why. -->

## Test plan
- [ ] Unit tests
- [ ] Manual smoke test

## Notes for reviewers
<!-- Anything risky, surprising, or worth flagging. -->
`;
}

function bugTemplateStub(): string {
  return `---
name: Bug report
about: Report something that does not work as expected
title: "bug: "
labels: bug
---

## Summary
A clear, one-sentence description of the problem.

## Steps to reproduce
1.
2.
3.

## Expected vs actual
- **Expected:**
- **Actual:**

## Environment
- OS / arch:
- Version:

## Logs
\`\`\`
<logs here>
\`\`\`
`;
}

function probotSettingsStub(): string {
  return `# Probot Settings (https://github.com/apps/settings)
# Install the "Settings" GitHub App on this repo, then any change to
# this file becomes the live branch-protection state. l0-git uses the
# presence of a \`branches:\` entry with \`protection:\` to verify
# that protection is declared as code; the actual server-side rules
# remain managed by GitHub once the app applies this file.

repository:
  # Optional: lock the repo's default settings as code. Uncomment to
  # take ownership of these — otherwise the live values stay untouched.
  # name: my-repo
  # description: Short description.
  # has_issues: true
  # has_projects: false
  # has_wiki: false
  # default_branch: main
  # allow_squash_merge: true
  # allow_merge_commit: false
  # allow_rebase_merge: true
  # delete_branch_on_merge: true

branches:
  - name: main
    protection:
      required_pull_request_reviews:
        required_approving_review_count: 1
        dismiss_stale_reviews: true
        require_code_owner_reviews: false
      required_status_checks:
        strict: true
        # List required CI job names (must match \`jobs.<name>\` in your workflows).
        contexts: []
      enforce_admins: false
      required_linear_history: false
      allow_force_pushes: false
      allow_deletions: false
      restrictions: null
`;
}

function ciStub(): string {
  return `name: ci

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

permissions:
  contents: read

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      # Set up your toolchain below — the commented examples are for Go.
      # - uses: actions/setup-go@v5
      #   with: { go-version: '1.23.x' }
      # - run: go test ./...
`;
}

function mitLicense(year: number, holder: string): string {
  return `MIT License

Copyright (c) ${year} ${holder}

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
`;
}

// The full Apache-2.0 / GPL / MPL texts are >5 KB each. Shipping a header
// with a download URL keeps the stub light while still being a valid
// pointer; the user is expected to drop in the full text.
function apache2Header(year: number, holder: string): string {
  return `Copyright ${year} ${holder}

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

# === The full Apache-2.0 license text should replace this block. ===
# Source: the Apache Software Foundation publishes the canonical text
# at apache.org/licenses/LICENSE-2.0.txt
`;
}

function bsd3(year: number, holder: string): string {
  return `BSD 3-Clause License

Copyright (c) ${year}, ${holder}
All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

1. Redistributions of source code must retain the above copyright notice,
   this list of conditions and the following disclaimer.

2. Redistributions in binary form must reproduce the above copyright notice,
   this list of conditions and the following disclaimer in the documentation
   and/or other materials provided with the distribution.

3. Neither the name of the copyright holder nor the names of its contributors
   may be used to endorse or promote products derived from this software
   without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE
LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR
CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF
SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS
INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN
CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE)
ARISING IN ANY WAY OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE
POSSIBILITY OF SUCH DAMAGE.
`;
}

function gplHeader(year: number, holder: string): string {
  return `Copyright (C) ${year} ${holder}

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

# === Replace this block with the full GPL-3.0 license text. ===
# Source: https://www.gnu.org/licenses/gpl-3.0.txt
`;
}

function mplHeader(): string {
  return `This Source Code Form is subject to the terms of the Mozilla Public
License, v. 2.0. If a copy of the MPL was not distributed with this
file, You can obtain one at https://mozilla.org/MPL/2.0/.

# === Replace this block with the full MPL-2.0 license text. ===
`;
}

function unlicense(): string {
  return `This is free and unencumbered software released into the public domain.

Anyone is free to copy, modify, publish, use, compile, sell, or
distribute this software, either in source code form or as a compiled
binary, for any purpose, commercial or non-commercial, and by any
means.

In jurisdictions that recognize copyright laws, the author or authors
of this software dedicate any and all copyright interest in the
software to the public domain. We make this dedication for the benefit
of the public at large and to the detriment of our heirs and
successors. We intend this dedication to be an overt act of
relinquishment in perpetuity of all present and future rights to this
software under copyright law.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT.
IN NO EVENT SHALL THE AUTHORS BE LIABLE FOR ANY CLAIM, DAMAGES OR
OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE,
ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR
OTHER DEALINGS IN THE SOFTWARE.

For more information, please refer to <https://unlicense.org/>
`;
}
