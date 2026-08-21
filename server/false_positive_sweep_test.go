package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// The cases in this file were all harvested from a sweep of 220 real
// repositories. Each one is a finding the gates used to emit that no
// maintainer could ever act on. They are locked in here so a future rule
// change cannot quietly reintroduce them.

// -----------------------------------------------------------------------
// network_scan
// -----------------------------------------------------------------------

// Inline SVG path data was, by itself, 54% of every finding produced across
// the corpus (15,402 of 28,531). SVG packs decimals — `1.08.58 1.23.82.72`
// is five numbers — so a GitHub icon embedded in a page reports as five
// hardcoded public IPv4 addresses, once per page.
func TestNetworkScan_SvgPathDataIsNotAnAddress(t *testing.T) {
	const githubIcon = `<a href="/x"><svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16">` +
		`<path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95.29.25.54.73.54 1.48z"/></svg></a>`
	root := initRepoWithFiles(t, map[string]string{"index.html": githubIcon + "\n"})
	fs, err := checkNetworkScan(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 0 {
		t.Fatalf("SVG path data must produce no address findings; got: %+v", fs)
	}
}

// A standalone .svg is a drawing; nothing in it is ever infrastructure.
func TestNetworkScan_SvgFileSkipped(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		"icon.svg": `<svg viewBox="0 0 16 16"><path d="M2.2.82.64 1.23.82.72"/></svg>` + "\n",
	})
	fs, err := checkNetworkScan(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 0 {
		t.Fatalf(".svg files must not be address-scanned; got: %+v", fs)
	}
}

// A four-component version is byte-identical to a dotted quad. Without
// context these all reported as hardcoded public addresses — the User-Agent
// case (`Chrome/120.0.0.0`) appears in almost every scraper ever written.
func TestNetworkScan_VersionLiteralsAreNotAddresses(t *testing.T) {
	cases := map[string]string{
		"user_agent": `headers = {'User-Agent': 'Mozilla/5.0 AppleWebKit/537.36 Chrome/120.0.0.0 Safari/537.36'}`,
		"pin":        `dependencies = ["brotlicffi==1.2.0.1", "aiohttp==3.13.3"]`,
		"attribute":  `<assemblyIdentity version="0.0.1.0" name="setup"/>`,
		"assignment": `version: 4.18.2.1`,
		"npm_spec":   `"pkg": "@1.2.3.4"`,
		"oid_prefix": `oid = 1.3.6.1.4.1.311`,
		"tag":        `git checkout v9.8.7.6`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			root := initRepoWithFiles(t, map[string]string{"conf.yaml": content + "\n"})
			fs, err := checkNetworkScan(context.Background(), root, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range fs {
				if strings.Contains(f.FilePath, "ipv4") {
					t.Errorf("version literal reported as address: %+v", f)
				}
			}
		})
	}
}

// A real hardcoded address in the same shapes must still fire, so the
// version suppression cannot be papering over the rule.
func TestNetworkScan_RealPublicAddressStillFires(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		"conf.yaml": "upstream: 51.222.140.163\nfallback: 93.184.216.34\n",
	})
	fs, err := checkNetworkScan(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := 0
	for _, f := range fs {
		if strings.HasSuffix(f.FilePath, ":ipv4_public") && f.Severity == SeverityWarning {
			got++
		}
	}
	if got != 2 {
		t.Fatalf("expected 2 public-address warnings, got %d: %+v", got, fs)
	}
}

// Public resolver constants and 1.2.3.4-style stand-ins are deliberate, not
// accidental infrastructure coupling: information, never a warning.
func TestNetworkScan_ResolversAndPlaceholdersAreInfo(t *testing.T) {
	cases := map[string]string{
		"google":     "8.8.8.8",
		"cloudflare": "1.1.1.1",
		"quad9":      "9.9.9.9",
		"sequential": "1.2.3.4",
		"descending": "4.3.2.1",
		"broadcast":  "255.255.255.255",
		"multicast":  "224.0.0.251",
	}
	for name, addr := range cases {
		t.Run(name, func(t *testing.T) {
			root := initRepoWithFiles(t, map[string]string{"conf.yaml": "dns: " + addr + "\n"})
			fs, err := checkNetworkScan(context.Background(), root, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range fs {
				if f.Severity == SeverityWarning {
					t.Errorf("%s must not be a warning: %+v", addr, f)
				}
			}
		})
	}
}

// An ASN in a documentation table is describing routing, not wiring it.
func TestNetworkScan_AsnInProseSkipped(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		"docs/providers.md": "| Cloudflare | ~19% | AS13335 |\n| Hetzner | AS24940 |\n",
		"routes.conf":       "peer AS13335\n",
	})
	fs, err := checkNetworkScan(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	prose, code := 0, 0
	for _, f := range fs {
		switch {
		case strings.HasPrefix(f.FilePath, "docs/providers.md"):
			prose++
		case strings.HasPrefix(f.FilePath, "routes.conf"):
			code++
		}
	}
	if prose != 0 {
		t.Errorf("ASN in prose must not be reported; got %d findings", prose)
	}
	if code != 1 {
		t.Errorf("ASN in config must still be reported; got %d findings: %+v", code, fs)
	}
}

// -----------------------------------------------------------------------
// connection_strings
// -----------------------------------------------------------------------

// Every one of these fired at ERROR severity across the corpus. None is a
// committed credential.
func TestConnectionStrings_PlaceholderCredentialsSuppressed(t *testing.T) {
	cases := map[string]string{
		"compose_interpolation": `DATABASE_URL=postgresql://${POSTGRES_USER:-secdata}:${POSTGRES_PASSWORD:?err}@postgres:5432/db`,
		"doc_userpass":          "Check format: `postgresql+asyncpg://user:pass@host:port/db`",
		"screaming_snake":       `DATABASE_URL=postgresql+asyncpg://fleet:CHANGE_DATABASE_PASSWORD@postgres:5432/fleet`,
		"your_prefix":           `DATABASE_URL=postgresql+asyncpg://postgres:YOUR_DB_PASSWORD@postgres:5432/identity`,
		"scaffold_default":      `sqlalchemy.url = postgresql+asyncpg://portal:portal@localhost:5432/portal`,
		"doc_shorthand":         `/// Redact user:pass from a URL: scheme://u:p@host -> scheme://***@host`,
		"regex_rule":            "r(\"DLP-007\", `(?i)(mongodb(\\+srv)?://|postgres(ql)?://)(\\S+:)?\\S+@`, 7, 3),",
		"token_suffix":          `Format: https://user:TOKEN@gitlab.example.net`,
	}
	for name, line := range cases {
		t.Run(name, func(t *testing.T) {
			fs := scanConnectionLine("conf.txt", 1, []byte(line+"\n"))
			for _, f := range fs {
				if strings.HasSuffix(f.FilePath, ":creds_in_url") {
					t.Errorf("placeholder must not fire creds_in_url: %+v", f)
				}
			}
		})
	}
}

// A database URI with no credentials pointing at a container-internal or
// local host says only "this project uses a database".
func TestConnectionStrings_LocalDbUriSuppressed(t *testing.T) {
	cases := []string{
		"redis://redis:6379/0",
		"redis://localhost:6379/0",
		"postgres://internet_admin@localhost:5432/internet_core",
		"amqp://mq_admin@localhost:5672/",
		"jdbc:postgresql://db/app",
	}
	for _, uri := range cases {
		t.Run(uri, func(t *testing.T) {
			fs := scanConnectionLine("conf.txt", 1, []byte("URL = "+uri+"\n"))
			if len(fs) != 0 {
				t.Errorf("credential-free local DB URI must be silent; got: %+v", fs)
			}
		})
	}
}

// …but a remote database host is still worth surfacing.
func TestConnectionStrings_RemoteDbUriStillFires(t *testing.T) {
	fs := scanConnectionLine("conf.txt", 1, []byte("URL = mongodb://cluster0.acme.io:27017/app\n"))
	if len(fs) == 0 {
		t.Fatalf("remote DB URI must still be reported")
	}
}

// Licence texts are verbatim boilerplate carrying http:// URLs nobody may edit.
func TestConnectionStrings_LicenseBoilerplateSkipped(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		"LICENSE": "Apache License\nhttp://www.apache.org/licenses/\n",
	})
	fs, err := checkConnectionStrings(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 0 {
		t.Fatalf("licence boilerplate must be skipped; got: %+v", fs)
	}
}

// -----------------------------------------------------------------------
// dependency trees / generated output / binary payloads
// -----------------------------------------------------------------------

// Nothing under a package-manager tree was authored here. vendored_dir_tracked
// makes the one actionable statement ("don't commit this"); itemising every
// TODO and http:// inside it buries that statement.
func TestContentGates_SkipDependencyTrees(t *testing.T) {
	body := "// TODO: fix upstream\nconst u = 'http://json5.org/'\nconst ip = '51.222.140.163'\n"
	files := map[string]string{
		"node_modules/left-pad/index.js":                     body,
		"webapp/node_modules/react/cjs/react.development.js": body,
		".venv/lib/python3.9/site-packages/pydantic/net.py":  body,
		"backend/vendor/github.com/jackc/pgx/pool.go":        body,
		"ios/Pods/Alamofire/Source/Request.swift":            body,
		"src/app.js": body, // control: first-party code must still be scanned
	}
	root := initRepoWithFiles(t, files)
	ctx := context.Background()
	for _, run := range []struct {
		name string
		fn   func(context.Context, string, json.RawMessage) ([]Finding, error)
	}{
		{"network_scan", checkNetworkScan},
		{"connection_strings", checkConnectionStrings},
		{"dead_placeholders", checkDeadPlaceholders},
	} {
		t.Run(run.name, func(t *testing.T) {
			fs, err := run.fn(ctx, root, nil)
			if err != nil {
				t.Fatal(err)
			}
			sawFirstParty := false
			for _, f := range fs {
				switch {
				case strings.HasPrefix(f.FilePath, "src/app.js"):
					sawFirstParty = true
				default:
					t.Errorf("dependency-tree file reported: %+v", f)
				}
			}
			if !sawFirstParty {
				t.Errorf("first-party file must still be scanned; got: %+v", fs)
			}
		})
	}
}

// A per-file metadata finding inside a vendored tree restates what
// vendored_dir_tracked already says once about the directory.
func TestMetadataGates_SkipVendoredTrees(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		"webapp/node_modules/typescript/lib/tsc.js": "x",
		"target/debug/build/script.json":            "{}",
		"src/main.go":                               "package main",
	})
	fs, err := checkUnexpectedExecutableBit(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fs {
		if strings.Contains(f.FilePath, "node_modules") || strings.Contains(f.FilePath, "target/debug") {
			t.Errorf("vendored-tree file reported by a metadata gate: %+v", f)
		}
	}
}

// A PDF's first 8 KiB is ASCII header and object table, so the NUL-byte
// heuristic passes it through and the byte-scan lifts "URLs" out of
// compressed streams and font tables.
func TestContentGates_SkipBinaryExtensions(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		"docs/report.pdf": "%PDF-1.4\nstream http://leaked.example.com 51.222.140.163\nendstream\n",
	})
	ctx := context.Background()
	for _, fn := range []func(context.Context, string, json.RawMessage) ([]Finding, error){
		checkNetworkScan, checkConnectionStrings,
	} {
		fs, err := fn(ctx, root, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(fs) != 0 {
			t.Fatalf("binary payload must not be content-scanned; got: %+v", fs)
		}
	}
}

// A .vitepress/dist page is regenerated on the next build; a finding there
// cannot be fixed where it is reported.
func TestContentGates_SkipGeneratedDirs(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		"docs/.vitepress/dist/guide/index.html": "<a href='http://api.acme.io'>x</a>\n",
		"docs/guide/index.md":                   "# Guide\n",
	})
	fs, err := checkConnectionStrings(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 0 {
		t.Fatalf("generated output must not be scanned; got: %+v", fs)
	}
}

// -----------------------------------------------------------------------
// secrets_scan
// -----------------------------------------------------------------------

// The PEM header inside an error message or a test's input string is code
// that mentions a key, not a key.
func TestSecretsScan_PemHeaderInsideStringLiteral(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		"src/keys.ts": "  throw new Error(\n    \"Private key must be PEM with -----BEGIN PRIVATE KEY----- (PKCS#8).\"\n  );\n",
		"src/red.rs":  "        let pem = \"head -----BEGIN RSA PRIVATE KEY----- tail\";\n",
	})
	fs, err := checkSecretsScan(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 0 {
		t.Fatalf("PEM header inside a string literal must not fire; got: %+v", fs)
	}
}

// A real key file must still fire — the literal check must not swallow it.
func TestSecretsScan_RealPemFileStillFires(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		"ssl/server.key": "-----BEGIN PRIVATE KEY-----\nMIIEowIBAAKCAQEA\n-----END PRIVATE KEY-----\n",
	})
	fs, err := checkSecretsScan(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !findingsContainPattern(fs, "private_key_header") {
		t.Fatalf("a committed .key file must still fire; got: %+v", fs)
	}
}

// A token spelled out as the alphabet scores MAXIMUM Shannon entropy (every
// character distinct) and sails past the entropy floor, while being the most
// obviously synthetic string a developer can type.
func TestSecretsScan_SequentialFillerSuppressed(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		"src/lib.rs": "assert_eq!(redact(\"export API_KEY=ghp_abcdefghijklmnopqrstuvwxyz0123456789\"), \"[REDACTED]\");\n",
	})
	fs, err := checkSecretsScan(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if findingsContainPattern(fs, "github_pat_classic") {
		t.Fatalf("sequential filler must not fire; got: %+v", fs)
	}
}

// Documentation placeholders no longer satisfy the Slack token shape.
func TestSecretsScan_SlackPlaceholdersSuppressed(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		"docs/slack.md":  "SLACK_BOT_TOKEN=xoxb-workspace1-token,xoxb-workspace2-token\n",
		"src/example.md": "token: \"xoxb-your-bot-token-here\"\n",
	})
	fs, err := checkSecretsScan(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if findingsContainPattern(fs, "slack_token") {
		t.Fatalf("slack placeholders must not fire; got: %+v", fs)
	}
}

// Xcode / .NET name a test target `<Product>Tests`, which the fixture-path
// convention did not recognise, so a regex-assertion test file was scanned.
func TestFixturePath_CamelCaseTestDirectories(t *testing.T) {
	yes := []string{
		"proxymateTests/ExfiltrationTests.swift",
		"AppKitTests/Helper.swift",
		"Acme.Web.Tests/Fixture.cs",
		"tests/conftest.py",
	}
	no := []string{
		"src/contests/leaderboard.go",
		"internal/latest/version.go",
		"src/main.go",
	}
	for _, p := range yes {
		if !isDefaultFixturePath(p) {
			t.Errorf("%s should be a fixture path", p)
		}
	}
	for _, p := range no {
		if isDefaultFixturePath(p) {
			t.Errorf("%s should NOT be a fixture path", p)
		}
	}
}

// -----------------------------------------------------------------------
// ide_artifact_tracked
// -----------------------------------------------------------------------

// The canonical VisualStudioCode.gitignore ignores `.vscode/*` and then
// explicitly un-ignores these — committing them is the documented convention.
func TestIdeArtifact_SharedVscodeConfigAllowed(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		".vscode/settings.json":    "{}",
		".vscode/launch.json":      "{}",
		".vscode/extensions.json":  "{}",
		".vscode/tasks.json":       "{}",
		".vscode/mcp.json":         "{}",
		".vscode/go.code-snippets": "{}",
		".vscode/ipch/cache.bin":   "x",
	})
	fs, err := checkIdeArtifactTracked(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 || fs[0].FilePath != ".vscode/ipch/cache.bin" {
		t.Fatalf("only user-local .vscode files may be flagged; got: %+v", fs)
	}
}

// -----------------------------------------------------------------------
// round 2: classes still visible after the first pass
// -----------------------------------------------------------------------

// Tailscale hands out RFC 6598 shared address space (100.64.0.0/10). A
// service URL on the tailnet is unreachable from the public internet, so
// cleartext there carries the same exposure as loopback — which the gate
// already exempts.
func TestConnectionStrings_TailnetHostExempt(t *testing.T) {
	exempt := []string{
		"http://100.102.64.123:8000/api",
		"http://100.76.251.33:11434/health",
	}
	for _, u := range exempt {
		if !httpHostExempt(u) {
			t.Errorf("CGNAT/tailnet host must be exempt: %s", u)
		}
	}
	// 100.0.0.0/10 outside the shared range is ordinary public space.
	if httpHostExempt("http://100.1.2.3/api") {
		t.Errorf("100.1.2.3 is not shared address space and must not be exempt")
	}
}

// An XML namespace or DOCTYPE system identifier names a schema; nothing is
// fetched over it. Every .plist and .svg in existence carries one.
func TestConnectionStrings_MarkupIdentifiersExempt(t *testing.T) {
	lines := []string{
		`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">`,
		`<svg xmlns="http://www.w3.org/2000/svg" width="16">`,
		`<xsd:schema xmlns:xsd="http://example.org/2001/XMLSchema">`,
	}
	for _, l := range lines {
		fs := scanConnectionLine("doc.xml", 1, []byte(l+"\n"))
		for _, f := range fs {
			if strings.HasSuffix(f.FilePath, ":http_remote") {
				t.Errorf("markup identifier must not fire http_remote: %+v", f)
			}
		}
	}
	// A real cleartext endpoint on the same kind of line still fires.
	fs := scanConnectionLine("conf.xml", 1, []byte(`<url>http://api.acme.io/v1</url>`+"\n"))
	if len(fs) == 0 {
		t.Errorf("a real http:// endpoint must still fire")
	}
}

// A tool's cache directory is scratch space regenerated on the next run.
func TestContentGates_SkipCacheDirs(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		".stargazer_cache/profiles.json": `{"blog":"http://example.org/x"}` + "\n",
		"src/app.py":                     `URL = "http://api.acme.io"` + "\n",
	})
	fs, err := checkConnectionStrings(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fs {
		if strings.HasPrefix(f.FilePath, ".stargazer_cache/") {
			t.Errorf("cache directory must not be scanned: %+v", f)
		}
	}
	if len(fs) != 1 {
		t.Fatalf("first-party source must still be scanned; got: %+v", fs)
	}
}

// Documentation quotes an EXCERPT of a JSON document — the members without
// their braces. Wrapping and reparsing is exact, not a heuristic.
func TestMarkdown_JsonFragmentBlockAccepted(t *testing.T) {
	fragments := []string{
		"\"meta\": {\n  \"objective\": \"O1\",\n  \"split\": \"test\"\n}",
		"\"scripts\": {\n  \"build\": \"tsc\"\n}",
		"{\"a\": 1},\n{\"a\": 2}",
	}
	for _, body := range fragments {
		if msg := validatePayload("json", body); msg != "" {
			t.Errorf("JSON excerpt must be accepted; got: %s\n%s", msg, body)
		}
	}
	// Genuinely broken JSON must still be reported.
	if msg := validatePayload("json", `{"a": 1,,}`); msg == "" {
		t.Errorf("malformed JSON must still be reported")
	}
}

// A block showing two alternative spellings of the same key repeats it on
// purpose. YAML forbids that; documentation does it constantly.
func TestMarkdown_YamlDuplicateKeyBlockAccepted(t *testing.T) {
	body := "# String format\ndiscord:\n  channels: \"1,2\"\n\n# List format\ndiscord:\n  channels:\n    - 1\n"
	if msg := validatePayload("yaml", body); msg != "" {
		t.Errorf("alternative-form YAML block must be accepted; got: %s", msg)
	}
	// Structurally broken YAML must still be reported.
	if msg := validatePayload("yaml", "name: Smoke: engine syntax\n"); msg == "" {
		t.Errorf("malformed YAML must still be reported")
	}
}

// Minified bundles carry the addresses of whatever library was bundled.
// secrets_scan still reads them — a build-injected key lives nowhere else.
func TestContentGates_SkipMinifiedBundlesButNotSecrets(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		"static/js/jspdf.umd.min.js": "var u='http://json5.org/';var k='AKIA1A2B3C4D5E6F7G8H';\n",
	})
	ctx := context.Background()
	fs, err := checkConnectionStrings(ctx, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 0 {
		t.Errorf("minified bundle must not be URL-scanned; got: %+v", fs)
	}
	sec, err := checkSecretsScan(ctx, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !findingsContainPattern(sec, "aws_access_key") {
		t.Errorf("secrets_scan must still read minified bundles; got: %+v", sec)
	}
}

// A real credential whose password merely CONTAINS a password-ish word must
// still fire. This is the false negative an earlier suffix rule introduced:
// `readonly_dev_pass` ends in `_pass` but is an issued credential.
func TestConnectionStrings_PasswordWordSuffixStillFires(t *testing.T) {
	cases := []string{
		"mysql+pymysql://minerva_ro:readonly_dev_pass@127.0.0.1:3306/revenue",
		"postgres://svc:prod_db_password@db.acme.io:5432/app",
		"redis://cache:s3cret_key@redis.acme.io:6379/0",
	}
	for _, url := range cases {
		t.Run(url, func(t *testing.T) {
			fs := scanConnectionLine("db.py", 1, []byte(`    "`+url+`",`+"\n"))
			found := false
			for _, f := range fs {
				if strings.HasSuffix(f.FilePath, ":creds_in_url") {
					found = true
				}
			}
			if !found {
				t.Errorf("real credential must still fire: %s :: %+v", url, fs)
			}
		})
	}
}

// The username is not the secret: a placeholder-looking USER with a real
// password must still fire, while doc shorthand (one-character segments)
// and regex patterns stay suppressed on either side.
func TestConnectionStrings_UserSideRulesAreStructuralOnly(t *testing.T) {
	fires := "https://user:hunter2xyz@api.acme.com"
	fs := scanConnectionLine("conf.txt", 1, []byte(fires+"\n"))
	found := false
	for _, f := range fs {
		if strings.HasSuffix(f.FilePath, ":creds_in_url") {
			found = true
		}
	}
	if !found {
		t.Errorf("placeholder-looking username with a real password must fire: %+v", fs)
	}
}

// The version-context rules must not swallow a real hardcoded address. Each
// of these was a false NEGATIVE an earlier, looser version of the rule
// introduced, caught by diffing the corpus sweep before and after.
func TestNetworkScan_VersionContextDoesNotHideRealAddresses(t *testing.T) {
	cases := map[string]string{
		"ssh_user_at_host": `ssh root@192.168.0.136 'systemctl restart app'`,
		"makefile_host":    `DEV_HOST  ?= root@192.168.0.135`,
		"env_host":         `ARENA_HOST=root@192.168.122.228 tools/deploy.sh`,
		"js_strict_equal":  `  return h === '169.254.169.254'  // IMDS`,
		"py_equality":      `if host == "169.254.169.254":`,
		"url_path_segment": `resp = get("https://api.acme.com/whois/93.184.216.34")`,
		"nip_io_wrapper":   `"http://169.254.169.254.nip.io",`,
	}
	for name, line := range cases {
		t.Run(name, func(t *testing.T) {
			fs := scanNetworkLine("conf.txt", 1, []byte(line), false, false, false)
			if len(fs) == 0 {
				t.Errorf("real address must still be reported: %s", line)
			}
		})
	}
}

// …while the genuine version shapes stay suppressed.
func TestNetworkScan_VersionContextStillSuppresses(t *testing.T) {
	cases := map[string]string{
		"user_agent": `'User-Agent': 'Mozilla/5.0 AppleWebKit/537.36 Chrome/120.0.0.0 Safari/537.36'`,
		"pip_pin":    `dependencies = ["brotlicffi==1.2.0.1"]`,
		"attribute":  `<assemblyIdentity version="0.0.1.0"/>`,
		"oid":        `OID_PREFIX = "1.3.6.1.4.1.311"`,
		"tag":        `git checkout v9.8.7.6`,
	}
	for name, line := range cases {
		t.Run(name, func(t *testing.T) {
			fs := scanNetworkLine("conf.txt", 1, []byte(line), false, false, false)
			for _, f := range fs {
				if strings.Contains(f.FilePath, "ipv4") {
					t.Errorf("version literal reported as address: %s :: %+v", line, f)
				}
			}
		})
	}
}

// A PEM header with no key material after it is a MENTION of the format:
// an error message, a README explaining what to paste, a changelog entry
// describing this very rule. The tool tripped on its own CHANGELOG.
func TestSecretsScan_PemHeaderWithoutBodyIsAMention(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		"CHANGELOG.md": "- Recognises a PEM header quoted inside a literal, so\n" +
			"  `\"must be PEM with -----BEGIN PRIVATE KEY----- (PKCS#8)\"` no longer fires.\n",
		"docs/setup.md": "Paste the contents starting at -----BEGIN PRIVATE KEY----- into the field.\n",
	})
	fs, err := checkSecretsScan(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 0 {
		t.Fatalf("a PEM header with no body must not fire; got: %+v", fs)
	}
}

// A header WITH key material after it still fires anywhere, and a file whose
// name declares it holds a key fires on the header alone.
func TestSecretsScan_PemBodyAndKeyFilesStillFire(t *testing.T) {
	const body = "MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQC7VJTUt9Us8cKj"
	cases := map[string]string{
		"docs/leak.md":      "```\n-----BEGIN PRIVATE KEY-----\n" + body + "\n```\n",
		"ssl/acme.key":      "-----BEGIN PRIVATE KEY-----\n",
		"deploy/id_ed25519": "-----BEGIN OPENSSH PRIVATE KEY-----\n",
	}
	for path, content := range cases {
		t.Run(path, func(t *testing.T) {
			root := initRepoWithFiles(t, map[string]string{path: content})
			fs, err := checkSecretsScan(context.Background(), root, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !findingsContainPattern(fs, "private_key_header") {
				t.Errorf("%s must still fire; got: %+v", path, fs)
			}
		})
	}
}

// -----------------------------------------------------------------------
// round 2: gates that assert a file is missing, and rules whose stated
// harm does not follow from the condition they detect
// -----------------------------------------------------------------------

// GitHub documents the root, .github/, and docs/ as valid homes for community
// health files. codeowners_present already searched all three; the rest read
// only the root and told authors to add a file they had already written.
func TestPresenceGates_SearchCommunityHealthDirs(t *testing.T) {
	cases := map[string]string{
		".github/CONTRIBUTING.md":    "contributing_present",
		"docs/SECURITY.md":           "security_present",
		"docs/CHANGELOG.md":          "changelog_present",
		".github/CODE_OF_CONDUCT.md": "code_of_conduct_present",
		"docs/README.md":             "readme_present",
	}
	for path, gate := range cases {
		t.Run(path, func(t *testing.T) {
			root := initRepoWithFiles(t, map[string]string{path: "# x", "main.go": "package main"})
			for _, g := range gateRegistry() {
				if g.ID != gate {
					continue
				}
				fs, err := g.Check(context.Background(), root, nil)
				if err != nil {
					t.Fatal(err)
				}
				if len(fs) != 0 {
					t.Errorf("%s exists, %s must stay silent; got: %+v", path, gate, fs)
				}
			}
		})
	}
}

// LICENSE is deliberately root-only: GitHub's licensee reads only the root,
// so a licence filed elsewhere still leaves the repo showing no license.
func TestLicensePresent_StaysRootOnly(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{"docs/LICENSE": "MIT"})
	for _, g := range gateRegistry() {
		if g.ID != "license_present" {
			continue
		}
		fs, err := g.Check(context.Background(), root, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(fs) != 1 {
			t.Errorf("a licence outside the root must still be reported; got: %+v", fs)
		}
	}
}

// A project with a working .gitlab-ci.yml has CI. Telling it to "add a CI
// workflow" was 23% of this gate's findings across the sweep, at warning.
func TestCIWorkflow_RecognisesOtherProviders(t *testing.T) {
	silent := []string{
		".gitlab-ci.yml", ".travis.yml", "Jenkinsfile", "azure-pipelines.yml",
		".drone.yml", "bitbucket-pipelines.yml", ".cirrus.yml",
		".circleci/config.yml", ".gitea/workflows/ci.yml", ".forgejo/workflows/ci.yml",
		".github/workflows/ci.yml",
	}
	for _, f := range silent {
		t.Run(f, func(t *testing.T) {
			root := initRepoWithFiles(t, map[string]string{f: "steps: []\n"})
			fs, err := checkCIWorkflow(context.Background(), root, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(fs) != 0 {
				t.Errorf("%s is a CI pipeline; gate must stay silent, got: %+v", f, fs)
			}
		})
	}
	// A repo with no pipeline at all still fires.
	root := initRepoWithFiles(t, map[string]string{"main.go": "package main"})
	fs, err := checkCIWorkflow(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 {
		t.Errorf("a repo with no CI must still fire; got: %+v", fs)
	}
}

// GitHub accepts the PR template in three directories, with three extensions,
// and as a directory of named templates.
func TestPRTemplate_AllDocumentedForms(t *testing.T) {
	for _, path := range []string{
		".github/PULL_REQUEST_TEMPLATE.md",
		"PULL_REQUEST_TEMPLATE.md",
		"docs/pull_request_template.md",
		".github/PULL_REQUEST_TEMPLATE/feature.md",
	} {
		t.Run(path, func(t *testing.T) {
			root := initRepoWithFiles(t, map[string]string{path: "## What"})
			fs, err := checkPRTemplate(context.Background(), root, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(fs) != 0 {
				t.Errorf("%s is a PR template; got: %+v", path, fs)
			}
		})
	}
}

// The legacy single-file issue template is still supported by GitHub.
func TestIssueTemplate_LegacySingleFile(t *testing.T) {
	for _, path := range []string{".github/ISSUE_TEMPLATE.md", "ISSUE_TEMPLATE.md", "docs/ISSUE_TEMPLATE.md"} {
		t.Run(path, func(t *testing.T) {
			root := initRepoWithFiles(t, map[string]string{path: "## Bug"})
			fs, err := checkIssueTemplates(context.Background(), root, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(fs) != 0 {
				t.Errorf("%s is an issue template; got: %+v", path, fs)
			}
		})
	}
}

// A detection-rule file carries the marker as its payload: the pattern IS
// what the file is about.
func TestDeadPlaceholders_DetectionRuleFilesSkipped(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		"rules/VBC-056.yaml":  "id: VBC-056\nregex: 'TODO:'\n",
		"docs/rules/VBC-1.md": "Flags a leftover TODO: marker in production code.\n",
		"config/rules.yaml":   "- name: Lorem Ipsum in Production\n",
		"src/app.py":          "# TODO: implement\n",
	})
	fs, err := checkDeadPlaceholders(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	sawFirstParty := false
	for _, f := range fs {
		if strings.HasPrefix(f.FilePath, "src/app.py") {
			sawFirstParty = true
			continue
		}
		t.Errorf("detection-rule file reported: %+v", f)
	}
	if !sawFirstParty {
		t.Errorf("a real TODO in first-party code must still fire; got: %+v", fs)
	}
}

// A line-oriented dump is payload whether its items are bare addresses or
// bare URLs. connection_strings already skipped the URL form; network_scan
// reported one finding per line of the same file.
func TestNetworkScan_UrlListFileSkipped(t *testing.T) {
	lines := ""
	for i := 0; i < 20; i++ {
		lines += fmt.Sprintf("http://93.184.216.%d:3000\n", i+10)
	}
	root := initRepoWithFiles(t, map[string]string{
		"lista.txt":  lines,
		"config.yml": "upstream: 93.184.216.34\n",
	})
	fs, err := checkNetworkScan(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fs {
		if strings.HasPrefix(f.FilePath, "lista.txt") {
			t.Errorf("URL-list payload must be skipped: %+v", f)
		}
	}
	if len(fs) != 1 {
		t.Fatalf("the real config literal must still fire; got: %+v", fs)
	}
}
