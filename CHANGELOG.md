# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project follows
[Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.1.29] - 2026-08-21

### Fixed

- **`findings_remediate` returned a `summary` that restated `title`.** v0.1.28 fixed this for `lgit fix`'s text output; the `Remediation` struct was untouched, so MCP clients and `--json` still received both fields carrying the same sentence — which cost an agent tokens and told it nothing it did not already have in the same payload. Seven call sites were assembling `Remediation{Summary: f.Title, …}` by hand; they now share one helper, and the sentence lives in a single exported constant both channels read. Deterministic summaries are unchanged: *"Untrack .idea/workspace.xml and ignore it going forward."* is real guidance and stays.
- **A test flake that only appeared on git 2.55.** `git commit` can detach a `gc --auto`, and git does not wait for it, so it kept writing into `.git/objects/pack` after a test returned and raced `t.TempDir()`'s cleanup — `unlinkat …/.git/objects/pack: directory not empty`. Fixture repos now disable background maintenance at init, written straight into `.git/config` so it costs no extra process and covers every git invocation against the repo.

### Changed

- **Every GitHub Action moved to its current major** — `checkout` v7, `setup-go` v7, `setup-node` v7, `upload-artifact` v7, `download-artifact` v8, `upload-pages-artifact` v5, `deploy-pages` v5, `action-gh-release` v3. The runner logs had been warning that Node 20 is deprecated, which is a countdown rather than a warning: when it is removed, the release workflow stops working. The release notes were read rather than the version numbers trusted — `upload-pages-artifact` v4 stops including dotfiles in the artifact, which was the one change here that could have broken the published site silently, and was checked against a real build (the VitePress `dist` has no dotfiles and no underscore directories).
- **The docs build uses `npm ci` instead of `npm install`.** The lockfile is committed and `install` is free to resolve something else, so the published site need not have been the one built and reviewed locally.

## [0.1.28] - 2026-08-21

### Security

- **`golang.org/x/net` 0.27.0 → 0.58.0.** Three advisories were open on this dependency. The one that mattered here is [GHSA-5cv4-jp36-h3mw](https://github.com/advisories/GHSA-5cv4-jp36-h3mw) (CVSS 6.5), first patched in **0.55.0**: *parsing arbitrary HTML can consume excessive CPU time, possibly leading to denial of service*. `golang.org/x/net/html` is the only package of x/net this project imports, and parsing arbitrary HTML out of third-party repositories is exactly what `html_lint` does — so this was reachable on the one code path where it counts. Dependabot's open PR proposed 0.38.0, which would have closed the proxy-bypass and XSS advisories and left this one. The other two are closed as well.

### Changed

- **Minimum Go is now 1.25** (`go.mod`), because `golang.org/x/net` 0.55.0+ requires it. The CI matrix moves to 1.25.x / 1.26.x on Linux, macOS and Windows. This is not a narrowing: 1.22 and 1.23 are both EOL, and 1.25/1.26 are the pair Go itself supports. Prebuilt release binaries are unaffected — only building from source needs the newer toolchain. `release.yml` moved with it; it had pinned Go 1.23 in two places, which would have either failed the release build or had `GOTOOLCHAIN` quietly substitute a different toolchain than the pin claimed.
- **The `macos-15` CI pin is gone.** It existed because macOS 26's dyld rejects Go 1.22's `-race` test binaries, and its own comment said to drop it once 1.22 left the matrix.

### Fixed

- **`lgit stats -project <path>` returned totals for every project in the store.** Only the inline `-project=<path>` spelling was ever honoured: with the space-separated form the value came back empty, and an empty project means "all of them" to the store. The result was a wrong answer in the shape of the right one — correct JSON, exit 0, no warning. Both spellings work now, and unknown flags, missing values and stray positionals are errors, matching `lgit list`.
- **`lgit fix <id> --jsno` silently printed human-readable text.** A mistyped flag fell through as "no `--json`", so a caller about to parse JSON got prose instead. Unknown arguments are now rejected.
- **`lgit fix` printed the finding's title three times.** Gates without a deterministic recipe set the remediation summary to the finding's own title, so the same sentence appeared as the header, in substance under `Detected`, and again under a heading called `Fix`. A heading that says *Fix* and restates the problem reads as guidance while carrying none; it now says what to do next. Gates with a real summary — `merge_conflict_markers` — keep it. Rendering only: the `Remediation` struct and the MCP contract are unchanged.
- **The Windows CI job timed out intermittently.** Not deterministically: the same commit passed on one Go version and timed out on the other, then the reverse on the next run. Windows runs without `-race` and timed out anyway, so the cost was process spawning, which Windows does an order of magnitude slower than Linux. The test helpers spawned **1,467 git processes**, of which **478 were `git config`** setting a committer identity that `-c` carries inline; `git init` copied 14 sample hook files into each of ~240 fixture repos; and `initRepoWithFiles` committed, when every working-tree gate enumerates through `git ls-files`, which reads the index. Now **767 processes**, and the suite runs in roughly half the time. History gates see nothing in a repo built without a commit, so `initRepoWithCommit` exists for them — a test expecting findings would otherwise have passed vacuously. `-timeout` goes to 300s on top of that: it is a deadlock detector, not a performance budget.

### Documentation

- **The published site was broken from the day it went up.** VitePress resolves its public directory as `<srcDir>/public`; `logo.svg` lived in `docs/.vitepress/public`, which is never copied into the build, so the navbar logo and the favicon returned **404 in production**. Separately, the sidebar declared roughly 35 gate pages of which **three existed** — every other entry was a 404 for anyone who clicked it.
- **All 35 gates are now documented**, written from the source: rule tables with severity and advice for the five linters, the secret patterns with their entropy floor, the `network_scan` categories with the reasoning behind the ones that are off by default, the compose orchestrator carve-out, and the five `.vscode` files that are deliberately not flagged. Plus the VS Code and MCP guides the sidebar had always promised. 44 pages, no dead internal links.
- **The sidebar is generated from the gate registry** rather than maintained by hand, which is what let it drift — it also still claimed "34 gates" while the registry held 35, so `config_parse_error` had shipped undocumented. Four tests now pin the documentation to the registry, including that each page's `<GateMeta>` block matches on id, severity and tags.
- **Artwork**, all vector and first-party: a logo (the previous one was Lucide's shield icon as-is), an architecture diagram of the actual mechanism, and a terminal demo that is a transcript of real `lgit` output. The VS Code extension had no Marketplace icon at all and shipped with the grey placeholder.
- **The README drops from 475 lines to 190**, with install near the top instead of at line 435 — and the command is the one that works, verified against the real release asset names. Corrected along the way: the gate count, two contradictory test counts, and the claim that a firing gate means "no false positives", which three rounds of false-positive suppression do not support. Determinism buys reproducibility, not being right about intent.

## [0.1.27] - 2026-08-21

### Fixed

Adversarial false-positive sweep against 220 real repositories (28,531 findings
before / 7,929 after — **-72% total, -89% of all warnings, -71% of all errors**,
across two adversarial passes and a review round that put 248 real findings
back).
Every item below is a class the sweep proved unactionable, and every one is
locked in by a regression test in `false_positive_sweep_test.go`.

- **`network_scan` no longer reads inline SVG path data as IPv4 addresses.** This single class was **54% of every finding the tool produced across the corpus** (15,402 of 28,531). SVG packs decimals — `1.08.58 1.23.82.72` is five numbers — so one embedded GitHub icon reports as five hardcoded public addresses, once per page that carries it. Geometry attribute values (`d=`, `viewBox=`, `points=`, `transform=`, …) are blanked before any regex runs, and `.svg` files are skipped outright. The attribute form requires the HTML/JSX spelling with no spaces around `=`, so a Python `d = "10.0.0.1"` is untouched.
- **`network_scan` distinguishes a four-component version from a dotted quad.** `Chrome/120.0.0.0` in a User-Agent, `brotlicffi==1.2.0.1` in a dependency pin, `version="0.0.1.0"` in a manifest, and the OID prefix `1.3.6.1.4.1.311` all reported as hardcoded public addresses. Suppression is by surrounding context and was narrowed against real false *negatives* found by diffing the sweep: `ssh root@192.168.0.136`, `h === '169.254.169.254'`, `https://api.acme.com/whois/93.184.216.34`, and `169.254.169.254.nip.io` all still fire.
- **`network_scan` classifies constants that are not infrastructure coupling.** Public DNS resolvers (`8.8.8.8`, `1.1.1.1`, `9.9.9.9`, Quad9/OpenDNS/AdGuard/…), addresses whose octets run consecutively (`1.2.3.4`, `5.6.7.8`, `4.3.2.1` — no allocation is laid out that way), the limited-broadcast address, multicast groups, and reserved `240.0.0.0/4` are now **info** with their own category instead of `public` **warning**.
- **`network_scan`: `0.0.0.0` findings are now opt-in** (`"report_unspecified": true`), for the same reason loopback became opt-in in v0.1.25 — inside a container `--host 0.0.0.0` is the *only* correct bind, so the literal is a deployment fact the gate cannot distinguish from a real exposure. 1,146 findings on the corpus, none actionable.
- **`network_scan` skips ASN references in prose.** `| Cloudflare | ~19% | AS13335 |` in a documentation table describes routing; only a config or source file can pin a project to an ASN.
- **`connection_strings` `creds_in_url` (error severity) no longer fires on placeholders.** The userinfo splitter now skips over `${…}`/`{{…}}` groups, so a compose URL like `postgresql://${POSTGRES_USER:-x}:${POSTGRES_PASSWORD:?err}@postgres:5432/db` is no longer parsed into a nonsense password. Added suppression for regex metacharacters (a WAF detection rule is not a URL), one- and two-character segments (`scheme://u:p@host` in a doc comment), placeholder vocabulary (`user:pass`, `changeme`, `TOKEN`, `xxx`), `your…`/`change…`/`replace…` prefixes, SCREAMING_SNAKE_CASE env-var slots, and a user repeated as its own password (`portal:portal`). Vocabulary rules apply to the **password only** — a suffix rule was tried and reverted because it swallowed `readonly_dev_pass`, a real credential.
- **`connection_strings` exempts credential-free local database URIs, licence boilerplate, tailnet hosts, and markup identifiers.** `redis://redis:6379/0` states only that the project uses a database; `http://www.apache.org/licenses/` in a LICENSE is text nobody may edit; `http://100.102.64.123:8000` is RFC 6598 shared address space (Tailscale), unreachable off-tailnet; and `xmlns="http://…"` / a DOCTYPE system identifier names a schema rather than an endpoint — nothing connects over either.
- **Every content gate skips third-party dependency trees** (`node_modules/`, `vendor/`, `site-packages/`, `.venv/`, `Pods/`, `.yarn/`, `third_party/`, …). Nothing there was authored in the repository, and the one actionable statement about the tree — "this shouldn't be committed" — is already made once by `vendored_dir_tracked`; itemising 801 upstream TODOs and 15 vendored `user:pass@` docstrings buries it. Metadata gates (`large_file_tracked`, `unexpected_executable_bit`) skip the same trees plus `target/debug`, `target/release`. Opt out with `"skip_default_dependency_paths": false`.
- **Every content gate skips unambiguous tool output** (`.next/`, `.vitepress/`, `_site/`, `__pycache__/`, `htmlcov/`, any `*_cache/`) — a finding there cannot be fixed where it is reported. Ambiguous names (`dist/`, `build/`, `out/`, `target/`) are deliberately **not** included, matching the v0.1.23 reasoning for `vendored_dir_tracked`. Opt out with `"skip_default_generated_dirs": false`.
- **Binary payloads are never content-scanned.** `isBinary` looks for a NUL byte in the first 8 KiB; a PDF's first 8 KiB is an ASCII header and object table, so every PDF in the corpus was byte-scanned and its compressed streams and font tables mined for "URLs" and "IPv4 addresses". Extensions are now checked first (`.pdf`, images, audio/video, archives, fonts, `.safetensors`, …).
- **The noisy content gates skip minified bundles** (`*.min.js`, `*.min.css`, `*.bundle.js`) — every match came from the library that was bundled. `secrets_scan` still reads them: a build-time-injected key lives there and nowhere else.
- **`markdown_lint` reports the line the link is actually on.** goldmark gives inline nodes no position, so `nodeLine` walked up to the enclosing block and returned the line the *block* starts on. For a multi-line paragraph — or a GFM table, which goldmark parses as one paragraph — every link in it reported the same wrong line: one README table produced 76 findings all pointing at the header row. Findings now descend to the first positioned descendant.
- **`markdown_lint` strips a query string before checking a link target.** `[x](../../discussions/new?category=ideas)` and `[y](image.png?raw=true)` were resolved against a filename that included `?…`.
- **`markdown_lint codeblock_invalid_payload` accepts documentation conventions.** A ` ```json ` block quoting an *excerpt* — object members without their braces (`"meta": { … }`) — is now accepted by wrapping and reparsing, an exact test rather than a heuristic. A ` ```yaml ` block whose only violation is a duplicate key is accepted too: showing two alternative spellings of the same key ("# String format" … "# List format") is deliberate. Genuinely malformed payloads still fail.
- **`secrets_scan`: the Slack token pattern matches the real token format.** `xox[abprs]-[0-9A-Za-z-]{10,}` matched any hyphenated word salad, so a documented `SLACK_BOT_TOKEN=xoxb-workspace1-token` and a test's `"xoxb-1234567890-1234567890-"` literal both reported as leaked tokens at **error** severity.
- **`secrets_scan` recognises a PEM header quoted anywhere inside a string literal.** The check required the match to sit immediately after the opening quote, so `"Private key must be PEM with -----BEGIN PRIVATE KEY----- (PKCS#8)."` — an error message — reported as a committed key. A real `.key` file still fires.
- **`secrets_scan` treats a PEM header with no key material as a mention of the format.** A README explaining what to paste, an error message, or a changelog entry describing this very rule carries the header with prose after it, not base64 — and the tool tripped on its own CHANGELOG. A header now needs a following line of key material to fire, unless the file name itself declares it holds a key (`*.pem`, `*.key`, `id_ed25519`, …), where the header alone is proof enough.
- **`secrets_scan` rejects synthetic filler the entropy floor cannot see.** `ghp_abcdefghijklmnopqrstuvwxyz0123456789` uses 36 distinct characters and therefore scores the *maximum possible* Shannon entropy, sailing past the 3.5 floor while being the most obviously fake string a developer can type. A monotone run of 8+ consecutive code points now disqualifies a match.
- **Fixture detection recognises the `<Product>Tests/` convention** used by Xcode, .NET, and Java (`proxymateTests/`, `Acme.Web.Tests/`). Matching requires the capital `T`, so a lower-case `contests/` is untouched.
- **`ide_artifact_tracked` no longer flags the shared `.vscode/` project files.** The canonical `VisualStudioCode.gitignore` ignores `.vscode/*` and then explicitly un-ignores `settings.json`, `tasks.json`, `launch.json`, `extensions.json`, and `*.code-snippets` — committing them is the documented convention. The gate was telling users to delete files their editor expects the repository to carry. `.DS_Store` and genuinely user-local `.vscode/` content still fire.
Second adversarial pass, same corpus — this one went after the gates that
assert a file is **missing**, and after rules whose stated harm does not follow
from the condition they detect.

- **Community health files are found where GitHub documents them.** GitHub reads CONTRIBUTING / SECURITY / CODE_OF_CONDUCT / CHANGELOG / README from the root, `.github/`, **or** `docs/`; every one of those gates looked only at the root, so a project with `docs/SECURITY.md` was told to write the file it had already written. `codeowners_present` already searched all three — the rest now match it. LICENSE and `.gitignore` stay root-only on purpose: GitHub's licensee reads only the root, so a licence filed elsewhere really does leave the repository showing no license, and git only honours a `.gitignore` where it sits.
- **`ci_workflow_present` recognises CI that is not GitHub Actions.** A project with a working `.gitlab-ci.yml` was told "No workflow files found under `.github/workflows/`. Add a CI workflow" — at **warning** severity, on 28 repositories, 23% of the gate's output. Now also detects GitLab CI, CircleCI, Travis, Jenkins, Azure Pipelines, Drone, Bitbucket Pipelines, Woodpecker, AppVeyor, Cloud Build, Cirrus, and the Gitea / Forgejo workflow directories.
- **`pr_template_present` and `issue_template_present` accept every form GitHub documents.** The PR gate matched exactly `.github/pull_request_template.md`, missing the root and `docs/` locations, the `.txt` and extension-less spellings, and the `PULL_REQUEST_TEMPLATE/` directory of named templates. The issue gate missed the legacy single-file `ISSUE_TEMPLATE.md`.
- **`filename_quality` no longer reports a project for writing its own language.** The blanket "non-ASCII chars" rule was **82% of this gate's output** (120 of 146 findings), and every hit was a correctly-spelled word — `it_esperto_di_sostenibilità_….txt`. The gate's stated harm is shell pipelines that don't quote argv, and that follows from *whitespace*, not from an accent: `à` word-splits exactly as `a` does. The rule is replaced by the characters that genuinely misbehave — bidirectional overrides (a name that renders as something other than what it is, the Trojan Source class) and zero-width characters (two names that look identical) — alongside the existing space and control-character checks. The gate also stops itemising names inside vendored trees.
- **Detection-rule files are skipped by the content gates.** A YAML/JSON/Markdown file under `rules/`, `signatures/`, `detections/` — or named `rules.yaml`, `signatures.json` — carries the pattern as its payload: the placeholder marker, the cleartext-protocol probe, and the credential-shaped example are what the file is *about*. `secrets_scan` already skipped `.yar`/`.yara`; that skip now covers the directory and basename conventions too, and applies to `dead_placeholders`, `network_scan`, and `connection_strings`. `config_parse_error` deliberately still reads them — broken JSON is broken JSON.
- **`html_lint target_blank_no_rel` no longer asserts a vulnerability that does not exist.** Its advice said the new tab "can read `window.opener` and run reverse-tabnabbing attacks". Every evergreen browser has implied `rel="noopener"` for `target="_blank"` since 2021 (Chrome 88, Firefox 79, Safari 12.1, WHATWG HTML #4078), so that hazard is unreachable. What remains — withholding the Referer header, and supporting pre-2021 browsers — is a preference, so the rule drops from **warning** to **info** and says what is actually true.
- **`network_scan` skips URL-list files.** A 108-line `lista.txt` of bare `http://<ip>:<port>` lines is a scan-target dump; its addresses are the payload. `connection_strings` already recognised the URL-list form via the same exact per-line test — `network_scan` saw only the bare-address form and reported one finding per line.

Review round — eleven further defects, every one on the same axis: a
suppression that would have hidden something real.

- **A key assigned to a source constant is still a committed key.** The quoted-literal heuristic ran *before* the key-material check, so `const key = "-----BEGIN PRIVATE KEY-----\nMIIEow…"` was discarded as "a mention of the format". The checks are now ordered so evidence of key material always wins; the quoted-literal and comment heuristics are gone entirely, subsumed by it.
- **The dependency / generated / binary skips are kept out of the secrets path.** They were placed in the shared base helper — the one `secrets_scan` and `secrets_scan_history` use — on the reasoning that `vendored_dir_tracked` already makes the actionable statement about such a tree. That reasoning fails exactly where it matters: a legitimate Go `vendor/` (go.mod + `vendor/modules.txt`) is *exempt* from that gate, so a credential committed there would have produced no finding at all, silently breaking the secrets gate's contract over tracked files. The noise those skips remove is address / URL / TODO noise; only the gates that suffer it are protected now.
- **The equal-pair credential exemption is bounded by the host.** `user == password` is open-ended, unlike the closed `postgres:postgres` list beside it, so `svc:svc@db.acme.io` was discarded as a scaffold default. Every instance of the pattern in the corpus pointed at localhost or a container-internal service name; against a real host it is a weak credential, not an example, and fires again.
- **Key material is base64 of random bytes, not any long alphanumeric run.** Loosening the body check to "contains a 40-character run" made a docs sentence carrying `kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly` read as a key, turning a prose mention of the header into an error. A run now has to mix both cases *and* digits — which random base64 does with probability 1 - 4e-6, and a CamelCase identifier or a hex digest never does. Caught by re-running the corpus, not by the unit tests.
- **An encrypted PEM is no longer mistaken for prose.** The body check gave up after two non-empty lines, but an encrypted key puts `Proc-Type: 4,ENCRYPTED` and `DEK-Info: …` between the header and the base64. RFC 1421 metadata lines are now stepped over within a bounded PEM block.
- **Reserved-range exemptions parse the host as an address.** `strings.HasPrefix(host, "10.")` also accepts `10.acme.io`, and `"100."` accepts `100.64.123.evil` — public hostnames whose cleartext URL was silently exempt.
- **A placeholder prefix needs a token boundary.** Bare `my` classified `svc:mySecretValue@db` as a placeholder and dropped a real credential. A prefix now has to be followed by a separator (`my_password`, `your-token`) or by a placeholder noun with nothing after it (`changeme`, `yourpassword`).
- **The markup-identifier exemption belongs to the declaration that owns the URL.** Matching anywhere in the line prefix meant one `xmlns` hid every later endpoint on the same line — `<svg xmlns="…"><image href="http://api.acme.io/x"/>` reported nothing.
- **`.svg` files are scanned again.** Blanking the geometry attributes is what kills the false positive; skipping the whole file also threw away a real `<image href="http://…">`.
- **`1.2.3.4` is reported at info instead of vanishing.** `isSequentialOctets` returned the `doc-range` category, which `scanNetworkLine` drops outright — so the placeholders produced no finding at all, contradicting this changelog. They have their own category now. The test that should have caught this asserted only "not a warning", which a dropped finding satisfies; it now asserts the finding exists.
- **`.cargo/config.toml` and `.bundle/config` are authored files**, not dependency code — the first routinely holds registry URLs and can hold credentials. Only named cache subtrees (`.cargo/registry`, `.cargo/git`, `.yarn/cache`, …) count as third-party now.
- **Detection-rule basenames match case-insensitively**, extension included: `RULES.YAML` trimmed a lower-cased basename with an upper-cased extension and fell through.

Self-review of the sweep's own suppressions, held to the same standard as the
false positives it removed — a skip that silences real source is worse than the
noise it removes:

- **The regex-syntax check is structural, not per-character.** `creds_in_url` treats a URL as a detection rule when it contains regex *syntax* — an escape sequence, a character class, a parenthesised group, an alternation. Matching bare `$ ^ + * ?` would have been simpler and wrong: a strong password contains those constantly (`Xk9$mQ2!vL`), and silencing one is exactly the leak this gate exists to catch.
- **`Pods/` is matched case-sensitively**, so a lower-case `k8s/manifests/pods/` of hand-written YAML is still scanned; **a bare `cache/` is no longer treated as tool output**, because `internal/cache/redis.go` is an ordinary source package; and the CamelCase fixture rule **drops `Spec`/`Specs`**, since an `OpenApiSpec/` is an API definition rather than a test target.

- **`vendored_dir_tracked` reports the outermost container.** `.venv/`, `venv/`, and `site-packages/` were missing from the prefix list, so a Python virtualenv was reported as a `build/` directory seven levels inside it.

## [0.1.26] - 2026-07-19

### Fixed

- **Remediation prompts now name a verification surface that actually exists in the session.** Every `claude_prompt` unconditionally told the agent to *both* run `lgit check …` *and* "use the l0-git MCP tools", regardless of which — if either — was present: `lgit` is never on `PATH` (it ships only as `server/lgit` and the extension-bundled binary), and the MCP tools exist only when the l0-git server is registered for that Claude Code session. An agent remediating in a repo where neither was wired up had no working way to confirm a fix. The prompt is now built per delivery channel: delivered via the `findings_remediate` MCP tool it verifies with the `gates_check` MCP tool (guaranteed present — the agent just called one) and never mentions `lgit check`; delivered via `lgit fix` on the CLI it verifies with `lgit check <project> <gate>` (guaranteed present — the user just ran `lgit`) and never mentions the MCP tools. `RemediationFor` takes an explicit `Channel`; each caller passes the one matching how it delivers the prompt.

## [0.1.25] - 2026-06-29

### Fixed

- **Content gates skip list/log payload files anywhere in the tree.** `.log` access logs, `.list`/`.lst` block/allow lists, and JSON-lines log dumps (`log.json`, `*.log.json`, `*.log.txt`) are payload, not authored source — a quoted-IP `dns/dns.list`, an `nginx-access.log`, or a 491-line `log.json` produced one finding per record. Extends the v0.1.24 dataset-directory skip beyond `data/`-style trees (these files are payload regardless of location) under the same `skip_default_data_dirs` knob. As with the dataset-dir skip, **`secrets_scan` / `secrets_scan_history` still read them** — logs are a real credential-leak vector.
- **`network_scan` loopback findings are now opt-in (off by default).** A loopback literal (`127.0.0.0/8`, `::1`) is a local-dev default, never a security concern, and was the single largest info-noise category. Re-enable per-project with `"report_loopback": true`. The security-relevant unspecified address (`0.0.0.0` — bound to all interfaces, worth a review) stays on.

## [0.1.24] - 2026-06-29

### Added

- **`skip_default_data_dirs` (default on) — content gates skip dataset directories.** A file under a recognised dataset directory (`data/`, `datasets/`, `corpus/`, `corpora/`, `samples/`, `payloads/`, `wordlists/`) **and** carrying an ambiguous data extension (`.json`, `.txt`, `.xml`, `.cm`, `.nl`, `.list`, `.lst`, `.dat`) is the dataset payload, not authored source — every IP / URL / token inside is a self-evident false positive. The extension gate is deliberately data-only, so a real source file in the same tree (`data/loader.go`, `internal/data/store.py`) is never silenced, and the same extensions at the repo root (`config.json`, `notes.txt`) are still scanned. **`secrets_scan` and `secrets_scan_history` deliberately do NOT honour this** (they use a new `shouldSkipContentExceptDataDirs`): a credential committed into a dataset file is still a leak that must surface. Opt out for the other gates with `"skip_default_data_dirs": false`.

### Fixed

- **`network_scan` detects address-list files by content, not just by extension.** A file whose lines are overwhelmingly bare IP/CIDR literals — a Tor exit-node blocklist, a resolver cache (`cache/3.txt`), an ASN dump — is an address list whose payload IS the addresses, so every line was a false positive. Detection is exact (each line must parse as a single IP/CIDR via `net.ParseIP`/`ParseCIDR` after stripping an inline `#`/`;` comment; multi-token lines like `server 1.2.3.4:80;` and invalid octets never count) and gated on `skip_default_data_files`. The previous extension list (`.csv`/`.jsonl`/…) missed the `.txt` line-lists that dominate the noise — a single 8.5k-line IP cache produced 8.5k findings.
- **`connection_strings` detects URL-list files by content** with the same heuristic shape — a file that is overwhelmingly bare `scheme://…` lines (a feed dump, seed list, crawl frontier) is skipped rather than flagged line-by-line.
- **`connection_strings` exempts `.internal` hosts and `169.254.0.0/16` link-local.** `service.internal` / `pushgateway.internal` use the ICANN-reserved (2024) private-use TLD and resolve only inside a cluster; `169.254.169.254` is the cloud metadata endpoint, which is http-only by design and unreachable off-host. Flagging either as "cleartext HTTP MITM exposure" is pure noise.
- **`markdown_lint` `codeblock_no_language` is now opt-in (off by default).** A fenced block without a language tag is a style preference, not a verifiable defect — an output/plain-text/ASCII-tree block legitimately has none — so it sat outside the project's "binary, unambiguous violation" charter while accounting for the bulk of markdown findings. Re-enable per-project with `"enabled_rules": ["codeblock_no_language"]`. The structural rules (broken link, broken anchor, unparseable payload, missing alt) are unchanged.
- **`markdown_lint link_local_broken` no longer flags non-repository paths.** A link to a home-relative (`~/.config/app/settings.md`), filesystem-absolute (`/etc/hosts`), or site-root-absolute (`/guide/intro`) target is not a repo-relative path; resolving it against the file's directory always misses, producing a false "broken link". These are left to the author.
- **`markdown_lint codeblock_invalid_payload` passes illustrative JSON.** A ` ```json ` block that uses an ellipsis (`...`, "more fields here") or line comments (`//`, `/* */`) is a documentation example, not a literal payload — strict-parsing it is a guaranteed false positive. Comment markers are matched only as a line lead so a `//` inside a string value (a URL) does not mask a real parse error.

## [0.1.23] - 2026-06-24

### Fixed

- **`secrets_scan` now actually honours the data-file / backup-path skips it documents.** The gate read file contents but called `shouldSkip` (exclude-paths + fixtures only) instead of `shouldSkipContent`, so the `.csv`/`.tsv`/`.jsonl` data-file skip and the `bak/`/`.bak`/`backup-YYYYMMDD` snapshot skip — announced in v0.1.16 — were never applied. A credential-shaped column in a `.csv` export or a shelved `.bak` produced an **error**-severity false positive. The lone content-scan gate calling the wrong helper; now aligned with the other seven. Opt back in with `"skip_default_data_files": false`.
- **`secrets_scan_history` now applies the full working-tree suppression chain.** The history scanner did a bare `regexp.Match` on every blob — no entropy floor, no known-non-secret allowlist, no detection-rule (`.yar`/`.yara`) skip, no source-literal private-key suppression. Canonical doc examples (`AKIAIOSFODNN7EXAMPLE`, the jwt.io token), low-entropy mock data, and YARA-rule payloads fired in history, and because history is immutable they re-surfaced on **every** scan with a destructive `git filter-repo` remedy. The per-match filter is now a single shared `secretMatchSuppressed` used by both the working-tree and history gates, so they can never drift again.
- **`vendored_dir_tracked` no longer flags `build/`, `dist/`, `target/`, `.cache/` on the name alone.** Unlike `node_modules/` or `__pycache__/`, these double as ordinary English words and hand-authored source/content directories, so flagging them unconditionally proposed a destructive `git rm -r --cached` against tracked source. They are now flagged only when a corroborating build-tool marker for the matching ecosystem is present at the repo root or alongside the directory (`Cargo.toml`/`pom.xml`/`build.gradle` for `target/`; `package.json`/`pyproject.toml`/`setup.py`/`CMakeLists.txt` for `build/`; `package.json`/`pyproject.toml` for `dist/`; `package.json` for `.cache/`). The served-web-asset exemption (previously `vendor/`-only) now also covers `dist/`/`build/`, so a committed CDN or GitHub-Pages bundle (`docs/.vitepress/dist`) is left tracked. A false negative here is far safer than the destructive false positive.
- **`large_file_tracked` recognises `**/*.ext` Git-LFS patterns.** `.gitattributes` matching split each pattern on `**` and substring-checked the literal segments, so the most common LFS form — `**/*.psd`, the shape `git lfs track` emits — never matched a real path (the literal `*` is not in `design/logo.psd`). A file already in LFS was told to "move it to Git LFS". Patterns are now translated to anchored regexps with faithful `*`/`**`/`?` gitattributes glob semantics.
- **`creds_in_url` no longer flags canonical default-credential pairs.** A connection URL whose user **and** password are both service defaults (`postgres:postgres`, `guest:guest`, `root:example`) is a docker-compose / quickstart example, not a committed secret. Discrimination is by credential shape, not host: real-looking credentials still fire even on `localhost` or an RFC1918 address (committing real creds is a leak regardless of reachability).
- **`config_parse_error` skips Jinja/Salt/Ansible YAML that uses only `{% … %}`.** The template detector recognised `{{ … }}` and `<% … %>` but not bare `{% if %}`/`{% for %}` control blocks, so a Salt state or Ansible vars file with no `{{ }}` expression was reported as broken YAML. `{%` is now a template marker.
- **`dockerfile_lint` understands multi-stage stage aliases and BuildKit heredocs.** `from_untagged` no longer fires on `FROM <stage>` where `<stage>` is an earlier `AS <stage>` (an internal alias cannot be tagged — the most common multi-stage idiom). The parser now consumes heredoc bodies (`COPY <<EOF … EOF`, `RUN <<EOF … EOF`), so a `USER root`/`FROM …` *inside* a heredoc is treated as data, not misclassified as an instruction.
- **`markdown_lint link_anchor_broken` now reproduces GitHub's anchor algorithm faithfully.** The slugger was an ASCII approximation that the exact-match rule treated as ground truth, producing false positives on valid anchors. Three divergences fixed: Unicode letters are preserved (`Café Menu` → `café-menu`), runs of hyphens are no longer collapsed and leading/trailing hyphens no longer trimmed (`Node.js & npm` → `nodejs--npm`, `🚀 Getting Started` → `-getting-started`), matching the Ruby `html-pipeline` rules GitHub renders READMEs with. Inline code in a heading no longer double-counts (`## The \`cfg\` file` slugged correctly), and explicit anchors written as raw HTML (`<a name="x">`, `<h2 id="x">`) are now collected as valid link targets.
- **Content-scan gates skip machine-generated artefacts by default.** Source maps (`.map`), dependency lockfiles (`package-lock.json`, `yarn.lock`, `go.sum`, `Cargo.lock`, `composer.lock`, …), and generated Go protobuf (`.pb.go`) are skipped by `secrets_scan`, `connection_strings`, `network_scan`, and the other content gates — any pattern match inside is a build artefact or a value already present in the scanned source. Minified bundles (`.min.js`) are deliberately **not** skipped: build-time-injected frontend secrets live there and nowhere else. Opt out with `"skip_default_generated_files": false`.
- **`dead_placeholders` skips changelog/release-note files.** A line like "Removed the `FIXME:` markers" in `CHANGELOG.md` is historical prose, not a live placeholder — the same basename policy the `connection_strings` and `markdown_lint` gates already apply.
- **`css_lint justified_text` is exempt inside Sass `@mixin`/`@include`/`@function`/`%placeholder` definitions.** A reusable fragment's final applied selector is unknown at definition, so flagging `text-align: justify` there is a context-free guess the zero-FP bar forbids.
- **`gitignore_coverage` recognises `**/`-anchored patterns as covering.** A `.gitignore` already containing `**/__pycache__/` (the canonical recursive form) was still told to add `__pycache__`, because Go's `filepath.Match` has no recursive `**`. Coverage now also tests the pattern with a leading `**/` stripped.

## [0.1.22] - 2026-06-16

### Fixed

- **`vendored_dir_tracked` no longer flags a `vendor/` of hand-committed web assets.** Self-hosted fonts/CSS/JS vendored same-origin to remove third-party egress (e.g. `vendor/font-awesome/*.woff2`, `docs/vendor/chart.umd.min.js`) are *served* by the site and rebuilt by nothing — `git rm -r --cached` + `.gitignore` would 404 the deployed page, the inverse of package-manager vendoring. The gate already exempted package-manager `vendor/` (go.mod+modules.txt, composer, bundler) and dirs under a served static root (`public`/`static`/…), but missed a top-level `vendor/` and a GitHub-Pages `docs/vendor/` of hand-committed assets. Now a `vendor/`-prefixed dir containing browser-served files (`woff2`/`woff`/`ttf`/`eot`/`otf`/`css`/`js`/`svg`/`map`) is left tracked; `node_modules`/`bower_components` stay flagged (the exemption is scoped to the `vendor/` prefix).

## [0.1.21] - 2026-06-12

### Fixed

- **Untrack recipes no longer append a redundant `.gitignore` line.** The `ide_artifact_tracked` and `vendored_dir_tracked` remediations always added the ignore pattern to `.gitignore`, even when it was already covered — the common case, since git keeps tracking files added *before* an ignore rule, so a tracked-but-ignored artefact is exactly what the gate flags. The redundant edit is harmless to a human, but makes automated consumers that reject redundant `.gitignore` changes drop the *whole* remediation — so the artefact is never untracked and gets re-flagged every scan. The append is now emitted only when the pattern isn't already covered (reusing the gate's `readGitignorePatterns`/`coveredBy`); otherwise the recipe is the untrack command alone and the summary notes "(already covered by .gitignore)". Fails open (keeps the append) when `.gitignore` can't be read. Mirrors the v0.1.19 glob-aware fix on the detection side.

## [0.1.20] - 2026-06-10

### Added

- **New gate `config_parse_error`** — parses every tracked JSON and YAML config file and flags any that fail to parse. A broken `package.json`, CI workflow, or k8s manifest is a deterministic defect that breaks downstream tooling the moment it lands. Built to the zero-FP bar: JSONC files (`tsconfig.json`, `.vscode/*.json`, `*.jsonc`) are skipped by path, and any other `.json` that merely uses comments or trailing commas is rescued by a tolerant re-parse before it could be flagged; YAML is decoded into a `yaml.Node` so custom tags (`!Ref`, `!GetAtt`, …) and multi-document files are accepted, and template files (Helm/Jinja `{{ }}`, ERB `<% %>`) are skipped. A UTF-8 BOM and empty files are tolerated. TOML and INI are intentionally out of scope (no parser without a new dependency; INI has no single grammar). Honours the shared `exclude_paths` / `skip_default_*` options. Verified zero false positives across a real-world repo corpus.

## [0.1.19] - 2026-06-06

### Fixed

- **`gitignore_coverage` is now glob-aware — no more redundant suggestions.** The gate matched recommended entries (`.DS_Store`, `.venv`, …) against `.gitignore` lines by exact/normalised string only, so a project already ignoring `*.DS_Store` was still told to add `.DS_Store`. Coverage now also honours globs via `filepath.Match` (`*.DS_Store` covers `.DS_Store`), while preserving the literal distinction that matters (`venv` does NOT cover `.venv`).

## [0.1.18] - 2026-06-06

### Fixed

- **`vendored_dir_tracked` no longer flags vendored dirs under a served static web root.** A `vendor/`, `node_modules/`, … directory under `public/`, `static/`, `assets/`, `www/`, `htdocs/`, or `wwwroot/` holds hand-committed third-party assets (fonts, chart libs, polyfills) that nothing rebuilds — `git rm -r --cached`-ing them would delete files the site serves and break the build. Those paths are now skipped. Package-manager vendoring and build outputs (`dist/`, `build/`, `target/`, root `vendor/` without a manifest) are still flagged.

## [0.1.17] - 2026-05-17

### Changed

- **Content-scan gates skip tabular data files by default**. `network_scan`, `connection_strings`, `secrets_scan` (via the same option struct), `markdown_lint`, `html_lint`, `css_lint`, `dockerfile_lint`, `compose_lint`, and `dead_placeholders` now skip `.csv`, `.tsv`, `.jsonl`, `.ndjson`, `.parquet`, `.arrow`, `.feather` — the addresses, URLs, and hashes inside are the file's payload, not embedded literals. Opt out per-gate with `"skip_default_data_files": false` in `gate_options`. Metadata-only gates (`large_file_tracked`, `vendored_dir_tracked`, …) still see these files.
- **Content-scan gates skip local backup snapshots by default**. Files under `bak/`, `backup/`, `backups/`, `archive/`, `archived/` directories, with `.bak` / `.backup` / `.old` / `.orig` extensions, or whose basename / directory matches `backup[-_]YYYYMMDD([-_]HHMMSS)?` are skipped — they're stale echoes of the live tree. Opt out per-gate with `"skip_default_backup_paths": false`. The leading-anchor in the timestamp regex avoids matching `check_backup_*.py` and similar domain-word filenames.
- **`connection_strings` skips changelog-style files** (`CHANGELOG.md`, `HISTORY.md`, `RELEASES.md`, `CHANGES.md`, `NEWS.md`, …) by basename, matching the existing `network_scan` policy. Those files quote past behaviour, not current configuration.
- **`markdown_lint codeblock_no_language` skipped in changelog-style files**. Same rationale: changelogs paste raw output / log excerpts where retagging is churn nobody re-reads. Structural rules (`link_local_broken`, `link_anchor_broken`, `codeblock_invalid_payload`) still run.

### Fixed

- **`creds_in_url` no longer fires on template URLs**. `postgresql://nodeapp:${PG_DB_PASS}@host`, `https://$GITEA_USER:$GITEA_TOKEN@…`, `mongodb://%s:%s@host`, `https://{{ user }}:{{ token }}@…`, `redis://<user>:<pass>@…` are recognised as runtime templates: when the password segment is entirely one placeholder (`${VAR}`, `$VAR`, `%[sdvqxX]`, `<name>`, `{{ var }}`), the URL is not a committed secret. The username is treated as non-sensitive — `admin:hunter2@host` (literal password) still fires.
- **`secrets_scan private_key_header` no longer fires on key-parsing code**. Two new signatures suppress the false positive without weakening detection on real PEM blobs:
  - **Detection-rule files** (`.yar`, `.yara`) are skipped outright — the header IS the rule's payload.
  - **Literal-context match in any file**: a header immediately preceded by `"`, `'`, or `` ` `` is treated as a string literal in source / YAML rule lists / Astro / Vue / Svelte / HTML attributes / Markdown inline code.
  - **Comment-line match in source files**: header lines starting with `//`, `#`, `/*`, `*`, `--`, `;`, or `<!--` (across .go / .ts / .py / .rs / .swift / .sql / .html / …) are treated as documentation about the parser.
  Genuine PEM blobs (header at column 0 on its own line, or inside a block-comment opener) still fire — verified by test.
- **Nil-safe option defaults**: `skipEnabled(*bool)` treats `nil` as enabled, so the default-skip semantics now hold even when a gate's custom option parser decodes directly into the embedded `scanOptions` without going through `parseScanOptions`. Previously affected: `markdown_lint`, `html_lint`, `css_lint`, `dockerfile_lint`, `compose_lint`, `dead_placeholders`.

## [0.1.16] - 2026-05-11

### Changed

- Release 0.1.16 (no notes — fill me in)

## [0.1.15] - 2026-05-11

### Changed

- Release 0.1.15 (no notes — fill me in)

## [0.1.14] - 2026-05-11

### Changed

- Release 0.1.14 (no notes — fill me in)

## [0.1.13] - 2026-05-11

### Added

- **`scripts/update.sh` — lifecycle manager for the local install**. Pulls the latest `main`, rebuilds the `lgit` binary, re-registers the Claude Code MCP server, and prints a restart hint. Flags: `--no-pull` (build current tree), `--dry-run`, `--quiet`, `--force` (skip dirty-tree check), `--no-mcp`, `--no-restart-hint`. Runs from any subdirectory — `cd`s to repo root itself.
- **`make update` / `make update-local` / `make status`** — thin Makefile wrappers around `scripts/update.sh` plus a status target that prints the binary version, running `lgit` PIDs, and the Claude Code MCP registration state. No more guessing which `lgit` is wired up.
- **`scripts/release.sh` + `make release-patch|release-minor|release-major`** — one-command release flow: verifies clean tree on `main` up-to-date with `origin`, bumps `extension/package.json` and `extension/package-lock.json`, rotates the CHANGELOG `Unreleased` section to the new version with today's date, commits, creates an annotated tag, and pushes both — which triggers the `release.yml` workflow that publishes binaries and `.vsix`.

### Fixed

- **Extension version alignment**: `extension/package.json` and `extension/package-lock.json` were stuck at `0.1.11` and `0.1.6` respectively across every tag from `v0.1.7` onward. The published `.vsix` therefore reported a stale internal version regardless of the tag name. Both files are now in lock-step with the release tag, enforced by `scripts/release.sh`.

## [0.1.12] - 2026-05-10

### Added

- **Global `exclude_paths` in `.l0git.json`**: a top-level `exclude_paths` array now applies to every content-scanning gate without repeating the list under each `gate_options` entry. Gate-specific `exclude_paths` are still supported and are merged after the global ones. Example:

  ```json
  {
    "exclude_paths": ["**/generated/**", "vendor/**"],
    "gate_options": {
      "secrets_scan": { "exclude_paths": ["**/fixtures/**"] }
    }
  }
  ```

  The injection happens in `optionsFor` — zero changes to gate function signatures.

## [0.1.11] - 2026-05-10

### Fixed

- **`tests_present`**: added `.test.mjs` / `.test.cjs` / `.spec.mjs` / `.spec.cjs` (Vitest ESM/CJS), `*Test.cs` / `*Tests.cs` (C# / .NET), `*Test.php` / `*Tests.php` / `test*.php` (PHPUnit), `*Spec.kt` (Kotlin). Projects using these conventions no longer get a false-positive "No tests found" warning.
- **`connection_strings` / `db_uri`**: regex extended to cover `sqlserver://`, `mssql://` (SQL Server), `mariadb://`, `couchdb://`, `cassandra://`, `cql://`. Previously these connection string schemes passed through undetected.
- **`secrets_scan_history` cap transparency**: when history scanning stops at the `max_blobs` ceiling (default 5000) an `info` finding is now emitted — "History scan stopped after N blobs (M total) — oldest commits NOT scanned." Previously the truncation was completely silent, giving false confidence that the full history was clean.
- **VSCode extension / diagnostics truncation**: the hardcoded `-limit=500` in `syncDiagnostics` has been raised to 2000. If the cap is still reached a warning toast is shown ("diagnostics capped — run `lgit list` from the terminal for the full set"). Previously the Problems pane silently showed fewer findings than existed.

## [0.1.10] - 2026-05-10

### Fixed (VSCode extension UI/UX — draconian round)

- **Loading indicator**: status bar now shows `$(loading~spin) l0-git: checking…` while gates are running, so the user always knows a scan is in progress instead of seeing a stale count.
- **Go-to-line navigation**: clicking a finding in the tree now opens the file AND positions the cursor at the exact line the gate flagged. Scan-style gates encode `<file>:<line>:<rule_id>` in `file_path`; the line component was parsed but previously ignored for navigation.
- **Binary path validation**: changing `l0-git.binaryPath` in settings now immediately checks whether the path exists on disk and shows a warning with an "Open settings" button if it doesn't — no more silent failures at run-time.
- **Clear project — finding count**: the "Delete all l0-git findings for …?" confirmation dialog now includes the finding count (e.g., "12 findings") so the user knows what they're about to delete.
- **MCP spawn safety**: `startMCP` validates binary existence before spawning, attaches an `error` event handler to catch ENOENT at spawn-time (prevents unhandled-rejection leaks on activation failure), and guards the `exit` callback against a stale reference when `stopMCP` replaces the process.
- **File watchers for late-joined folders**: adding a workspace folder after activation now registers the full set of file-change watchers for that folder, so the sidebar responds to README/LICENSE/compose/… changes in folders opened after startup.

## [0.1.9] - 2026-05-10

### Added

- **`secrets_scan` known-non-secret filter** (`server/known_non_secrets.go`). A post-entropy layer eliminates false positives whose matched value is publicly known and carries zero information advantage for an attacker. Four tiers applied in order:
  - **Tier 1 — placeholder / template syntax**: `{{secret}}`, `${MY_KEY}`, `<TOKEN>`, `%SECRET%`, `[MY_TOKEN]`, `__VAR__`, `@VAR@`, `#{var}` and explicit instruction words (`changeme`, `replace_me`, `not_set`, `redacted`, `dummy`, `fake`, `mock`, …)
  - **Tier 2 — well-known service defaults** (~200 entries, each traceable to an official vendor page): PostgreSQL, MySQL/MariaDB, MongoDB, Redis, RabbitMQ (`guest`), Elasticsearch, InfluxDB, CouchDB, Cassandra, Neo4j, MinIO (`minioadmin`), Grafana, Keycloak, SonarQube, Harbor (`Harbor12345`), GitLab legacy (`5iveL!fe`), Vault dev-server (`root`, `dev-root-token`), LocalStack, Kafka, Airflow, Superset, Metabase, n8n, Jenkins, Drone, Woodpecker, Portainer, Gitea, Azurite, JWT tutorial secrets, and ~100 more
  - **Tier 3 — official test / sandbox key prefixes**: `sk_test_` / `pk_test_` / `rk_test_` / `whsec_test_` (Stripe), `sandbox-sq0isp-` / `sandbox-sq0atb-` (Square), `test_sk_` / `test_pk_` (Checkout.com), `sandbox_` (Braintree), `adyentest_` (Adyen)
  - **Tier 4 — canonical documentation examples**: AWS `AKIAIOSFODNN7EXAMPLE` + secret key, Azurite well-known storage account key, jwt.io debugger token, GCP quickstart key, GitHub PAT examples, Slack token examples, Twilio test SIDs, SendGrid / npm / OpenAI / Anthropic / Stripe placeholder examples

## [0.1.8] - 2026-05-10

### Fixed (false-positive reduction — round 2, 5 gates)

- **`connection_strings`**: single-label hostnames (no dot) are now exempt from the `http_remote` rule — `http://kafka`, `http://redis`, `http://db-primary` are Docker/k8s internal service names that are never reachable on the public internet.
- **`css_lint` / `thin_font_weight`**: `selectorIsBodyText` now handles element selectors with class/pseudo-class modifiers (`body.dark-theme`, `p:not(.lead)`) and comma-separated lists (`html, body { … }`); the `"html, body"` dead case in the switch was removed.
- **`css_lint` / `justified_text`**: `@media print { … }` blocks are exempt — justified text is standard typographic practice in print stylesheets where hyphenation is controlled by the renderer.
- **`compose_lint`**: orchestrator image list extended with `nginx`, `jwilder/nginx-proxy`, `nginxproxy/nginx-proxy`, `haproxy`, `envoyproxy/envoy`, `caddy`, `prom/prometheus`, `grafana/grafana`, `grafana/loki`, `grafana/promtail`, `prom/alertmanager`, `prom/node-exporter`, and more; new gate option `additional_orchestrator_images` lets projects add custom entries without an inline override per service.
- **`vendored_dir_tracked`**: `vendor/` is now exempt in Ruby projects that have `Gemfile` + `vendor/bundle/` (Bundler `--deployment` / `--path vendor/bundle` idiom for hermetic builds).

## [0.1.7] - 2026-05-10

### Fixed (false-positive reduction — 14 gates)

- **`nvmrc_missing`**: silent when `package.json` declares `engines.node` or `volta.node`; plain `.nvmrc` / `.node-version` files remain the canonical signal.
- **`vendored_dir_tracked`**: `vendor/` is no longer flagged in Go projects that have `vendor/modules.txt` (idiomatic `-mod=vendor`); same exemption for PHP Composer (`vendor/autoload.php`).
- **`secrets_scan` / `secrets_scan_history`**: added Shannon entropy floor (≥ 3.5 bits/char) on all variable-body patterns — mock data, placeholder strings, and doc examples that happen to match the regex are skipped. `skip_default_fixture_paths` now **defaults to `true`** (was `false`); set it to `false` explicitly to scan test fixtures.
- **`network_scan`**: `docNets` extended with RFC 2544 benchmarking range (`198.18.0.0/15`), IANA MCAST-TEST-NET (`233.252.0.0/24`), and RFC 6598 CGNAT (`100.64.0.0/10`) — these no longer produce a warning.
- **`connection_strings`**: `http://` URLs to standard-body hosts (`w3.org`, `ietf.org`, `xmlsoap.org`, `schemas.microsoft.com`, `purl.org`, `oasis-open.org`, …) are now exempt — XML namespaces and RFC references in source files no longer fire.
- **`compose_lint`**: `docker_socket_mount` is demoted to `info` for well-known orchestrator/proxy images (Traefik, Portainer, Watchtower, Dozzle, cAdvisor, …). An inline `# l0git: ignore docker_socket_mount` silences both the warning and the info variant.
- **`markdown_lint`**: `codeblock_invalid_payload` no longer fires for `jsonc`, `json5`, `hjson`, `json with comments` (pass-through — stdlib parser rejects their legal syntax). `ndjson` / `jsonl` are validated line-by-line.
- **`unexpected_executable_bit`**: files under `bin/`, `scripts/`, `script/`, `tools/`, `tool/`, `cmd/`, `hack/`, `.bin/` are exempted — intentional executable wrappers in conventional locations no longer flag.
- **`version_drift`**: root `package.json` is excluded from cross-manifest comparison when monorepo markers are present (`pnpm-workspace.yaml`, `lerna.json`, `nx.json`, `turbo.json`, or `workspaces` field).
- **`tests_present`**: added `cypress/`, `playwright/`, `e2e/`, `integration/`, `features/` (Cucumber) to recognized test directory names; added fallback that checks `package.json` devDependencies for well-known test runners (Jest, Vitest, Cypress, Playwright, Mocha, …).
- **`css_lint`**: `hidden_scrollbar` severity demoted from `warning` to `info` — the gate cannot determine cross-selector whether the element is actually scrollable, so a hard warning was disproportionate.
- **`dead_placeholders`**: files whose basename is a placeholder tracking register (`TODO.md`, `FIXME.md`, `TODO.txt`, `TODO`, …) are now skipped entirely.
- **`.l0git.json`**: removed now-redundant `skip_default_fixture_paths: true` entries (the default is `true`).

## [0.1.6] - 2026-05-07

### Added

- **Remediation recipes.** New `lgit fix <id>` CLI prints a structured
  fix for any finding: summary, exact shell commands, file edits with
  caveats, a verification step, and a Claude-Code-ready prompt block.
  `--json` emits the same payload as `Remediation { summary, confidence,
  recipe?, claude_prompt }` for tooling. Never executes — print only.
  Eight gates ship deterministic recipes today (`vendored_dir_tracked`,
  `ide_artifact_tracked`, `gitignore_coverage`,
  `unexpected_executable_bit`, `env_example_uncommented`,
  `merge_conflict_markers`, `large_blob_in_history`,
  `secrets_scan_history`); the rest fall back to `confidence: guided`
  with only the prompt populated.
- **`findings_remediate` MCP tool.** Same payload as `lgit fix --json`,
  callable from Claude Code. Pairs with the agent's own Bash/Edit tools
  so HITL is preserved at the apply step.
- **Sidebar inline actions.** Every finding row now has "Show fix
  recipe" (opens `lgit fix <id>` output in a doc) and "Ask Claude Code
  to fix" (copies the prompt to the clipboard) next to the existing
  ignore / delete buttons.

### Changed

- **Sidebar defaults rebalanced for signal-to-noise.** New installs hide
  `info`-severity findings by default — toggle via the severity filter to
  bring them back. `override_accepted` is now suppressed from the tree at
  every severity level (still persisted, still surfaced in the dashboard
  and `lgit list -gate=override_accepted`). Toasts fire for `error` only;
  warnings and info live in the sidebar / Problems pane. Existing users
  with customised filters keep them.
- Sidebar empty state now distinguishes "no actionable findings, N info
  hidden" from "no findings at all" so a clean tree no longer disguises
  pending audit work.

## [0.1.4] - 2026-05-07

### Fixed

- `TestUnexpectedExecutableBit_FlagsTextFiles` failed on the Windows
  CI runner because `os.Chmod(0o755)` is a no-op on Windows
  filesystems and git can't pick up an executable bit the filesystem
  doesn't carry. The test helper now drives the mode via
  `git update-index --chmod=+x`, which works portably across the
  Linux / macOS / Windows matrix.

## [0.1.3] - 2026-05-07

### Added

- `scanOptions.skip_default_fixture_paths` — opt-in flag (default
  `false`) on every content-scan gate. When enabled, files matching
  `*_test.go` / `test_*.py` / `*_test.py` / `*.test.{ts,tsx,js,jsx}` /
  `*.spec.{ts,tsx,js,jsx}` / `*_test.rs` / `*Test.{java,kt}` /
  `*_spec.rb` / `conftest.py`, plus paths traversing
  `test/`, `tests/`, `__tests__/`, `spec/`, `testdata/`, `fixtures/`,
  `__fixtures__/` are skipped. Removes the dogfood noise where test
  fixtures legitimately contain trigger material (mock secrets,
  fake URLs, fake IPs).

### Changed

- Overview dashboard: TAGS card explicitly explains that a finding
  contributes to every tag it carries (counts can sum to more than
  the open total).
- Sparkline shows a "trend will fill in over the next 7 days" hint
  when ≤ 1 day has data — typical of fresh databases.
- `.l0git.json` of l0-git itself now enables
  `skip_default_fixture_paths` for the 8 content-scan gates, dropping
  self-flagged fixtures from 79 → 40 findings.

### Fixed

- "By severity (open)" percentages used `s.total` (across all
  statuses) as denominator, so values summed to less than 100% when
  resolved/ignored findings existed. Now relative to the open total
  (`sum(by_severity)`), so percentages always close to 100%.

## [0.1.2] - 2026-05-07

### Added

- `branch_protection_declared` gate (severity `info`, opt-in via
  `gate_options.branch_protection_declared.enabled = true`). Verifies
  the project tracks branch-protection rules as code via Probot Settings
  (`.github/settings.yml` with `branches: [{protection: …}]`). Cannot
  see the actual server-side state — that needs a network call with
  auth, which is out of scope. Companion CodeAction quick-fix scaffolds
  a `.github/settings.yml` with sensible defaults (PR review required,
  no force-push, no deletions).

## [0.1.1] - 2026-05-07

### Added — gates (33 total, was 9)

**Project hygiene** — `code_of_conduct_present`, `codeowners_present` (silent
on docs-only repos via language-extension heuristic).

**Quality / release hygiene** — `tests_present` (multi-language test
detection: Go / Python / TS-JSX / Rust / Java / Kotlin / Ruby), `version_drift`
(cross-checks `package.json`, `Cargo.toml`, `pyproject.toml`, `mix.exs`,
`pom.xml`, `VERSION`), `nvmrc_missing`.

**Git hygiene** — `merge_conflict_markers` (line-precise, byte-pattern),
`large_file_tracked` (LFS-aware via `.gitattributes`), `unexpected_executable_bit`
(filters by extension whitelist of "definitely-not-script"), `vendored_dir_tracked`
(one finding per top-level dir), `ide_artifact_tracked` (`.vscode`/`.idea`/
`.DS_Store`/swap files), `filename_quality` (spaces / control / non-ASCII),
`gitignore_coverage` (stack-aware: `package.json` → `node_modules`, `Cargo.toml`
→ `target`, `pyproject.toml` → `__pycache__`+`.venv`, etc.).

**Security** — `secrets_scan` (10 high-precision patterns + `.env` tracked
detection, scoped to `git ls-files`), `connection_strings` (URI-style
detection: credentials inline → `error`; ftp/telnet/smb/nfs/rsync →
`warning`; cleartext `http://` non-local + `ldap://` + DB URIs → `info`),
`network_scan` (IPv4/CIDR/ASN classification: public → `warning`,
private/loopback/RFC-doc → `info`).

**Containers** — `dockerfile_lint` (hand-rolled AST: `from_untagged`,
`from_latest`, `add_instruction`, `missing_user`, `user_root`),
`compose_lint` (`yaml.v3` AST: `yaml_invalid`, `privileged_true`,
`network_mode_host`, `docker_socket_mount`, `missing_memory_limit`).

**Frontend / accessibility** — `html_lint` (`golang.org/x/net/html`
tokenizer with per-line tracking: viewport-blocks-zoom, autoplay-without-muted,
target-`_blank`-without-rel, mystery-meat-nav, placeholder-as-label,
reset-button), `css_lint` (hand-rolled: hidden-scrollbar, thin-font-weight
on body-text, justified-text).

**Documentation** — `markdown_lint` (goldmark AST: `image_no_alt`,
`link_local_broken`, `link_anchor_broken` with GitHub-style slug,
`codeblock_no_language`, `codeblock_invalid_payload` for ` ```json ` /
` ```yaml ` blocks), `dead_placeholders` (TODO/FIXME/XXX/HACK/Lorem ipsum
across tracked text files), `env_example_uncommented` (each `KEY=` line
must have an inline or preceding `#` comment).

**History scanning (opt-in)** — `secrets_scan_history` and
`large_blob_in_history` walk every blob reachable from any ref via
`git rev-list --all --objects` + `git cat-file --batch-check`. Both opt-in
via `gate_options.<gate>.enabled = true` because the walk is slow on big
repos. Findings carry `history:<sha>:…` paths and remediation messages
point at `git filter-repo`.

### Added — extension UI

**Tier 1 (in-tree controls)** — group findings by severity / gate / file /
tag / status / none; sort by updated / created / severity / gate / file;
status filter (open / ignored / resolved / all); per-severity multi-select
toggle; substring search across title/message/file/gate. State persists in
`globalState` across sessions; active state surfaces in `treeView.description`
("12 findings · group: severity · sort: severity · status: open").

**Tier 2 (Overview dashboard)** — webview with severity bars (open-only),
top gates, top files, tag chips (click to filter the tree), 7-day trend
sparkline. Backed by a new `findings_stats` MCP tool / `lgit stats` CLI
that returns aggregations in one round trip. Auto-refreshes after every
`runChecksAndRefresh`.

**CodeAction quick-fixes** — lightbulb action on every finding for a
fixable presence-style gate. LICENSE picker offers MIT / Apache-2.0 /
BSD-3-Clause / GPL-3.0 / MPL-2.0 / Unlicense; other gates write a
canonical scaffold and re-run gates so the diagnostic clears.

**Inline override directive** — `# l0git: ignore <rule_id> reason: …`
(plus YAML / Markdown / HTML / CSS comment variants) silences a single
rule on a single line. Override emits an `override_accepted` info
finding for audit. Missing `reason: …` bumps it to `warning`.

**Status bar item** — bottom-left, severity-aware: `$(check) l0-git: clean`
or `$(error|warning|info) l0-git: N` with tooltip breakdown. Clicks focus
the tree view.

**Diagnostics integration** — every open finding becomes a
`vscode.Diagnostic` keyed by `(file, line)` with `source = l0-git` and
`code = gate_id`. Showed in the Problems pane and on file-gutter icons.

**File watchers extended** — ~30 patterns covering README/LICENSE/CHANGELOG,
`.gitignore`, `.gitattributes`, `.nvmrc`, `CODEOWNERS`, `CODE_OF_CONDUCT*`,
`.env.example*`, `Dockerfile*`, `docker-compose*.yml`, manifests
(`package.json`, `Cargo.toml`, `pyproject.toml`, `mix.exs`, `pom.xml`),
`.github/**`. Adding/editing any input file re-runs gates without manual
refresh.

**Blame annotation** — opt-in via `l0-git.showBlame: true`. After each
fetch, runs `git blame --line-porcelain` per affected file (in parallel)
and appends `<short-sha> · <author> · <relative-time>` to each row's
description and tooltip.

### Added — backend API

**Rich `findings_list`** — both CLI (`lgit list -project=…
-severity=…  -gate=… -tag=… -query=… -sort=… -limit=N -offset=N`) and
MCP (`findings_list { … }`) accept the same filter set. Tag matching is
CSV-aware: `git` does NOT match `git-hygiene`. Sort whitelist:
`updated` / `created` / `severity` (worst-first) / `gate` / `file`.

**`findings_stats`** — new MCP tool / `lgit stats` CLI returning the
aggregate the Overview webview needs in one trip. `by_severity` is
open-only for consistency with the rest of the dashboard; `by_status`
spans every row.

**`gateRegistryMarshallable`** — fixes a long-standing bug where
`lgit gates` and the `gates_list` MCP tool tried to JSON-encode `Gate`
values directly, failing because `Check` is a `func`. The endpoints
now serialise descriptive metadata only (id, title, description,
severity, tags).

**`Tags` field on findings** — gates declare a comma-separated tag set
(`security`, `git-hygiene`, `accessibility`, …) propagated to every
finding they emit. Stored in a new `findings.tags` column with auto-
migration via `PRAGMA table_info` + `ALTER TABLE`.

**Severity precedence rework** — finding severity is now
`config_override > gate_set > gate_default`, so tiered scanners
(`secrets_scan`, `connection_strings`, `network_scan`) keep their per-
match severities unless the user explicitly overrides at the gate
level via `.l0git.json`.

**`gate_options` map in `.l0git.json`** — typed, gate-specific JSON
sub-tree passed to each gate's `Check` function. Schemas include
`disabled_rules`, `disabled_patterns`, `exclude_paths`, `threshold_mb`,
`suggest_when_missing`, `enabled` (history gates).

### Changed

- SQLite store: `busy_timeout` raised from 5 s to 15 s to absorb
  cross-process WAL contention (extension + Claude-Code MCP server).
  Migration runs on every open via `PRAGMA table_info` to add the new
  `tags` column on legacy DBs without rewriting the schema.
- `lgit list` switched from positional args to flag-based for the rich
  filter set. The extension and tests are the only consumers.
- `lgit gates` and MCP `gates_list` now return JSON-safe metadata
  (`gateRegistryMarshallable`) instead of the runtime `Gate` struct.

### Fixed

- Watcher serialisation: `lgitQueue` Promise chain ensures the
  extension never spawns two `lgit` processes against the same SQLite
  DB at once. `runChecksAndRefresh` debounces watcher bursts so a
  multi-file save collapses to a single check pass.
- Overview `By severity` panel previously mixed open + resolved
  counts, contradicting the (open-only) tree below it. Now both are
  open-only; the dashboard explicitly labels the "Total (all statuses)"
  card.
- Webview button label rendered literally as `$(play) Run all checks`
  (codicon syntax isn't expanded inside webview HTML). Replaced with
  a Unicode play arrow.
- `mustWrite` test helper now `os.MkdirAll`s parent dirs so subpath
  fixtures like `.github/CODE_OF_CONDUCT.md` no longer fail before
  the assertion runs.

### Documentation

- README rewritten to document all 33 gates (grouped by theme), the
  `.l0git.json` schema with `gate_options`, the inline override
  directive, the Tier 1 view controls, the Overview dashboard, the
  history-aware gates, the blame annotation setting, the 7 MCP tools,
  and the flag-based CLI.

## [0.1.0] - 2026-05-07

- Initial public commit: Go MCP stdio server + SQLite findings store +
  VSCode TreeView UI + first gate (`readme_present`).
