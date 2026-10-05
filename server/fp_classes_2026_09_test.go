package main

import (
	"context"
	"strings"
	"testing"
)

// False positives (and one false negative) found by re-running the gates over
// every repository the findings store knew about, 2026-09-30. Each test names
// the real file that exposed it and pairs the suppression with the case it must
// NOT silence.

func firesCredsInURL(line string) bool {
	for _, f := range scanConnectionLine("conf.txt", 1, []byte(line+"\n")) {
		if strings.HasSuffix(f.FilePath, ":creds_in_url") {
			return true
		}
	}
	return false
}

// minerva/integration-api/README.md wrote `mysql+pymysql://minerva_ro:…@…/db`,
// where `…` (U+2026) abbreviates the password and the host. The "one or two
// characters is prose" rule counted BYTES, and `…` is three of them.
func TestConnectionStrings_EllipsisIsProseShorthand(t *testing.T) {
	for _, line := range []string{
		"mysql+pymysql://minerva_ro:…@…/minerva_revenue",
		"postgres://app:…@db.prod.acme.io/app",
		"see scheme://u:p@host and scheme://user:…@host",
	} {
		if firesCredsInURL(line) {
			t.Errorf("prose abbreviation must not be reported: %s", line)
		}
	}
}

// The rule applied to the USERNAME as well, so a short login silenced the whole
// URL however strong the password behind it. `sa` is SQL Server's default
// login and `x:<token>` is the standard way to put a token in an https URL,
// both were invisible. The username is not the secret; the password's shape is.
func TestConnectionStrings_ShortUsernameDoesNotHideARealPassword(t *testing.T) {
	for _, line := range []string{
		"sqlserver://sa:Xk9mQ2vLw8Rt@prod-db.acme.io:1433/app",
		"DATABASE_URL=postgres://pg:Zq8nRt4vLw2x@db.prod.acme.io/app",
		"git clone https://x:Tk5sB1mW7eH4cVn@git.acme.io/team/repo.git",
		"redis://r:Mx7kP3dQ9yBs@cache.acme.io:6379/0",
	} {
		if !firesCredsInURL(line) {
			t.Errorf("a short username must not hide a real password: %s", line)
		}
	}
}

// wildbox's .githooks/pre-commit greps for credentials with
// `postgresql://[^:]+:[^@]+@|sk_(test|live)_`. It was suppressed only because
// its user, `[^`, happened to be two characters long; once that rule stopped
// applying to usernames the corpus diff showed these as NEW errors. The regex is
// recognised by what it is: a host made of alternation and groups.
func TestConnectionStrings_CredentialGrepPatternsAreNotCredentials(t *testing.T) {
	for _, line := range []string{
		`if grep -qE 'postgresql://[^:]+:[^@]+@|sk_(test|live)_' "$f"; then`,
		`PATTERN='mysql://[^:]+:[^@]+@|mysql'`,
		`r"(?i)(mongodb(\+srv)?://|postgres(ql)?://)(\S+:)?\S+@"`,
	} {
		if firesCredsInURL(line) {
			t.Errorf("a detection pattern must not be reported as a credential: %s", line)
		}
	}
	// The other axis: an IPv6 literal's brackets are not regex syntax in the
	// host, so a real credential in front of one is still reported.
	for _, line := range []string{
		"postgres://svc:Xk9mQ2vLw8Rt@[::1]:5432/app",
		"postgres://svc:Xk9mQ2vLw8Rt@[2001:db8::1]:5432/app",
	} {
		if !firesCredsInURL(line) {
			t.Errorf("a real credential must still be reported: %s", line)
		}
	}
}

// …and the suppressions that protect the doc shorthand still hold: the
// password side of the rule is unchanged, only its reach over the username went.
func TestConnectionStrings_ShortPasswordStaysShorthand(t *testing.T) {
	for _, line := range []string{
		"Redact user:pass from a URL: scheme://u:p@host -> scheme://***@host",
		"postgres://admin:x@db.acme.io/app",
		"https://user:pw@host.io/x",
	} {
		if firesCredsInURL(line) {
			t.Errorf("one- and two-character passwords are shorthand: %s", line)
		}
	}
	// A three-character password is not shorthand.
	if !firesCredsInURL("postgres://svc:Xy9@db.prod.acme.io/app") {
		t.Error("a three-character password must still be reported")
	}
	// Nor is a multi-byte password that is long in characters.
	if !firesCredsInURL("postgres://svc:пароль9x@db.prod.acme.io/app") {
		t.Error("a long non-ASCII password must still be reported")
	}
	// Nor is `…` plus real characters.
	if !firesCredsInURL("postgres://svc:…x9q@db.prod.acme.io/app") {
		t.Error("an ellipsis in front of real characters is not an abbreviation")
	}
}

// code-metrics/uv.lock has `version = "1.2.0.2"` for a package; eleven hits in
// one repository. The lockfile list named Poetry and Pipenv but not uv.
func TestNetworkScan_SkipsEveryPackageManagersLockfile(t *testing.T) {
	names := []string{
		"uv.lock", "pdm.lock", "pixi.lock", "bun.lock", "deno.lock", "mix.lock",
		"pubspec.lock", "Podfile.lock", "Package.resolved", "packages.lock.json",
		".terraform.lock.hcl", "gradle.lockfile", "conan.lock", "rebar.lock",
		"renv.lock", "stack.yaml.lock", "cabal.project.freeze", "Gopkg.lock",
		"glide.lock",
		// Already skipped before: must stay so.
		"Cargo.lock", "poetry.lock", "yarn.lock", "go.sum",
	}
	old := map[string]bool{"Cargo.lock": true, "poetry.lock": true, "yarn.lock": true, "go.sum": true}
	for _, n := range names {
		t.Run(n, func(t *testing.T) {
			if old[n] {
				if !isDefaultGeneratedFile("some/dir/" + n) {
					t.Errorf("%s was skipped before and must stay so", n)
				}
				return
			}
			if !isVersionPinLockfile("some/dir/" + n) {
				t.Errorf("%s is machine-generated and network_scan must skip its version pins", n)
			}
			if isDefaultGeneratedFile("some/dir/" + n) {
				t.Errorf("%s must NOT be in the list every content gate consults: it can hold a git URL with credentials", n)
			}
		})
	}
	root := initRepoWithFiles(t, map[string]string{
		"uv.lock":     "[[package]]\nname = \"brotlicffi\"\nversion = \"1.2.0.2\"\n",
		"deploy.conf": "primary = 51.222.140.163\n",
	})
	fs, err := checkNetworkScan(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fs {
		if strings.HasPrefix(f.FilePath, "uv.lock") {
			t.Errorf("uv.lock must not be scanned: %+v", f)
		}
	}
	// The scan itself still works on an ordinary file next to it.
	seen := false
	for _, f := range fs {
		if strings.HasPrefix(f.FilePath, "deploy.conf") {
			seen = true
		}
	}
	if !seen {
		t.Error("the control file must still be reported, or this test proves nothing")
	}
}

// Narrowing over widening: only the exact generated names are skipped. A file
// that merely ends in `.lock`, or carries a lockfile's name as a prefix, is
// authored and stays in scope.
func TestNetworkScan_LockfileSkipIsExactNamesOnly(t *testing.T) {
	for _, n := range []string{"deploy.lock", "uv.lock.bak", "notes-uv.lock.txt", "lock.json", "my.terraform.lock.hcl.md", "server.lockfile"} {
		if isDefaultGeneratedFile("cfg/"+n) || isVersionPinLockfile("cfg/"+n) {
			t.Errorf("%s is not a generated lockfile and must still be scanned", n)
		}
	}
}

func networkCats(t *testing.T, line string) []string {
	t.Helper()
	var out []string
	for _, f := range scanNetworkLine("x.txt", 1, []byte(line), true, true, false) {
		out = append(out, f.Severity+"/"+f.FilePath[strings.LastIndex(f.FilePath, ":")+1:])
	}
	return out
}

// certmate's OIDC module cites `RFC 6749 §4.1.2.1` (which the author had to
// ignore by hand) and its PCI DSS module `Req 4.2.1.1`. A section number is a
// dotted quad byte for byte; the keyword in front of it is the evidence.
func TestNetworkScan_SectionNumbersAreNotAddresses(t *testing.T) {
	for _, line := range []string{
		"# OAuth 2.0 (RFC 6749 §4.1.2.1) + OIDC Core error codes.",
		"//   - Req 4.2.1.1: Certificate inventory and trusted CA verification",
		"See section 4.2.1.1 of the standard.",
		"per clause 3.1.2.1 of the contract",
		"Requirement 12.10.7.3 applies",
		"CIS benchmark, sec. 5.2.17.9",
		"§ 5.2.17.9 lists them",
		"Annex 1.2.3.4 describes the flow",
	} {
		if got := networkCats(t, line); len(got) != 0 {
			t.Errorf("a section reference must not be reported: %q -> %v", line, got)
		}
	}
}

// The other axis, and the reason the rule needs BOTH the keyword and small
// components: a real address next to the same words must still be reported.
func TestNetworkScan_SectionKeywordDoesNotHideRealAddresses(t *testing.T) {
	for _, line := range []string{
		"section 51.222.140.163",           // keyword, but large octets
		"req 45.33.32.156 GET /index.html", // an access-log line
		"see §65.109.163.154",              // keyword, large octets
		"ssh root@4.2.1.1",                 // small octets, no keyword
		"request 4.2.1.1 was routed",       // `req` is only a prefix of the word
		"primary = 4.2.1.1",                // small octets, no keyword
		"Requirement: connect to 51.222.140.163 on port 22",
	} {
		got := networkCats(t, line)
		if len(got) == 0 {
			t.Errorf("a real address must still be reported: %q", line)
		}
	}
}

// Rust test modules wire invented addresses: `fra:100.1.2.3,syd:100.4.5.6`.
// They are still listed, as info, with advice to double-check.
func TestNetworkScan_InventedAddressesAreInfoNotWarnings(t *testing.T) {
	for _, ip := range []string{
		"100.1.2.3", "100.4.5.6", "100.7.8.9", "100.3.2.1",
		"2.2.2.2", "7.7.7.7", "100.1.1.1",
		"1.2.3.4", // the original rule, unchanged
	} {
		got := networkCats(t, "peer = "+ip)
		if len(got) != 1 || got[0] != "info/ipv4_doc-placeholder" {
			t.Errorf("%s should be reported once, as an info doc-placeholder; got %v", ip, got)
		}
	}
}

// …and real-looking public addresses stay warnings. Every one of these is a
// server address taken from the sweep.
func TestNetworkScan_RealPublicAddressesStayWarnings(t *testing.T) {
	for _, ip := range []string{
		"51.222.140.163", "46.250.245.74", "65.109.163.154", "89.46.67.54",
		"3.65.34.64", "147.93.154.207", "185.220.101.42", "216.58.214.14",
		"20.30.41.50", // the last three are NOT in a run (30, 41, 50)
		"100.2.4.6",   // step 2 is not one of the steps the rule knows
		"100.0.1.2",   // a zero octet opts out
	} {
		got := networkCats(t, "host = "+ip)
		if len(got) != 1 || got[0] != "warning/ipv4_public" {
			t.Errorf("%s must stay a public-address warning; got %v", ip, got)
		}
	}
}

// The new rule sits AFTER the resolver check, because the well-known resolvers
// are repeated-octet addresses too. Reordering would relabel all of them.
func TestNetworkScan_ResolversAreNotRelabelledAsPlaceholders(t *testing.T) {
	for _, ip := range []string{"8.8.8.8", "1.1.1.1", "9.9.9.9", "4.2.2.2", "1.0.0.1"} {
		got := networkCats(t, "dns = "+ip)
		if len(got) != 1 || got[0] != "info/ipv4_public-resolver" {
			t.Errorf("%s is a public resolver, not a placeholder; got %v", ip, got)
		}
	}
}

func invalidPayloadFindings(t *testing.T, doc string) []Finding {
	t.Helper()
	var out []Finding
	for _, f := range evaluateMarkdownFile("d.md", t.TempDir(), []byte(doc), nil) {
		if strings.HasSuffix(f.FilePath, ":codeblock_invalid_payload") {
			out = append(out, f)
		}
	}
	return out
}

func mdBlock(lang, body string) string { return "```" + lang + "\n" + body + "\n```\n" }

// l0-classifier's contract doc annotates a value with a trailing `// comment`.
// The gate already accepted comments at the START of a line; one after a value
// slipped through. The fix strips comments OUTSIDE strings and accepts the
// block only if what is left parses.
func TestMarkdown_JSONTrailingCommentsAreIllustrative(t *testing.T) {
	for name, body := range map[string]string{
		"trailing line comment": "{\n  \"text\": \"string\",\n  \"features\": {\"key\": \"value\"}   // optional\n}",
		"block comment":         "{\n  \"a\": 1, /* the count */\n  \"b\": 2\n}",
		"url in a string":       "{\n  \"u\": \"http://x.io//a\",   // kept\n  \"v\": 1\n}",
	} {
		if got := invalidPayloadFindings(t, mdBlock("json", body)); len(got) != 0 {
			t.Errorf("%s: a block that parses once comments are removed must be accepted: %+v", name, got)
		}
	}
	// The other axis: a comment does not excuse a real syntax error.
	for name, body := range map[string]string{
		"comment plus a real error":   "{\n  \"a\": 1,,   // c\n}",
		"error after a url in string": "{\"u\": \"http://x.io\", \"v\": }",
		"unterminated block comment":  "{\"a\": 1 /* never closed\n}",
	} {
		if got := invalidPayloadFindings(t, mdBlock("json", body)); len(got) != 1 {
			t.Errorf("%s: still invalid, must still be reported; got %d findings", name, len(got))
		}
	}
}

// certmate-ng's PRIVACY.md shows the telemetry payload's SHAPE: `"tenants":
// <integer>`. Replaced by null outside strings, then parsed strictly.
func TestMarkdown_JSONTypePlaceholdersAreIllustrative(t *testing.T) {
	ok := "{\n  \"instance_id\": \"<random uuid>\",\n  \"tenants\": <integer>,\n  \"edition\": <string, one of community|pro>\n}"
	if got := invalidPayloadFindings(t, mdBlock("json", ok)); len(got) != 0 {
		t.Errorf("type placeholders must be accepted: %+v", got)
	}
	for name, body := range map[string]string{
		"unclosed placeholder":  "{\"n\": <integer}",
		"placeholder + error":   "{\"n\": <integer>,, \"m\": 1}",
		"angle in key position": "{<key>: 1}",
	} {
		if got := invalidPayloadFindings(t, mdBlock("json", body)); len(got) != 1 {
			t.Errorf("%s: must still be reported; got %d", name, len(got))
		}
	}
}

// wildbox's TROUBLESHOOTING.md lists alternative responses, one object per
// line. That is a stream of JSON values.
func TestMarkdown_JSONStreamIsNotOneBrokenDocument(t *testing.T) {
	ok := "{\"detail\": \"Could not validate credentials\"}\n{\"detail\": \"Token has expired\"}"
	if got := invalidPayloadFindings(t, mdBlock("json", ok)); len(got) != 0 {
		t.Errorf("a stream of JSON values must be accepted: %+v", got)
	}
	for name, body := range map[string]string{
		"second value broken": "{\"a\": 1}\n{\"b\": ",
		"single broken value": "{\"detail\": \"x\"",
		"garbage between":     "{\"a\": 1}\nnot json\n{\"b\": 2}",
	} {
		if got := invalidPayloadFindings(t, mdBlock("json", body)); len(got) != 1 {
			t.Errorf("%s: must still be reported; got %d", name, len(got))
		}
	}
}

// domainmate's troubleshooting page shows a wrong YAML on purpose:
// `# Bad: Missing space after colon`. A block the author calls broken is meant
// to be; flagging it is always wrong.
func TestMarkdown_BlockLabelledAsCounterExampleIsNotADefect(t *testing.T) {
	// Genuinely invalid YAML: the "no label" case below is the control that
	// proves it, so the labelled cases cannot pass vacuously.
	bad := "name: Smoke: engine syntax"
	for name, doc := range map[string]string{
		"comment inside the block": mdBlock("yaml", "# Bad: a colon inside a plain scalar\n"+bad),
		"bold label above":         "**Bad**:\n\n" + mdBlock("yaml", bad),
		"heading above":            "### Wrong\n" + mdBlock("yaml", bad),
		"emoji label above":        "❌ Bad example\n\n" + mdBlock("yaml", bad),
		"en dash separator":        mdBlock("yaml", "# Bad \u2013 a colon inside a plain scalar\n"+bad),
		"em dash separator":        mdBlock("yaml", "# Bad \u2014 a colon inside a plain scalar\n"+bad),
		"dont label above":         "Don't:\n" + mdBlock("yaml", bad),
	} {
		if got := invalidPayloadFindings(t, doc); len(got) != 0 {
			t.Errorf("%s: a labelled counter-example must not be reported: %+v", name, got)
		}
	}
	// …and the same broken block, NOT so labelled, is still a defect.
	for name, doc := range map[string]string{
		"no label":                   mdBlock("yaml", bad),
		"good label":                 "**Good**:\n\n" + mdBlock("yaml", bad),
		"a sentence, not a tag":      "This is the bad config we saw in production, see below:\n" + mdBlock("yaml", bad),
		"another paragraph between":  "**Bad**:\n\nAnd here is what we run:\n\n" + mdBlock("yaml", bad),
		"label three blank lines up": "**Bad**:\n\n\n\n" + mdBlock("yaml", bad),
		"word bad, no colon":         mdBlock("yaml", "# Bad config\n"+bad),
	} {
		if got := invalidPayloadFindings(t, doc); len(got) != 1 {
			t.Errorf("%s: an unlabelled broken block must still be reported; got %d", name, len(got))
		}
	}
}

func firesHTTPRemote(line string) bool {
	for _, f := range scanConnectionLine("conf.txt", 1, []byte(line+"\n")) {
		if strings.HasSuffix(f.FilePath, ":http_remote") {
			return true
		}
	}
	return false
}

// certmate's chain fixtures hold 88 `http://certs.godaddy.com/…` URLs. A
// certificate chain, a CRL and an OCSP responder are http:// by design
// (RFC 5280), and what is fetched is verified by signature.
func TestConnectionStrings_PKIFetchesAreCleartextByDesign(t *testing.T) {
	for _, line := range []string{
		"Authority Info Access: CA Issuers - URI:http://certs.godaddy.com/repository/gdig2.crt",
		"CRL Distribution Points: URI:http://crl.godaddy.com/gdroot-g2.crl",
		"OCSP - URI:http://ocsp.digicert.com",
		"http://cacerts.digicert.com/DigiCertGlobalRootG2.crt",
		"issuer = http://pki.acme.io/ca.cer",
		"http://acme.io/chain.p7b?version=2",
	} {
		if firesHTTPRemote(line) {
			t.Errorf("a PKI fetch must not be reported: %s", line)
		}
	}
	// The other axis: anything else over cleartext, including key material.
	for _, line := range []string{
		"http://files.acme.io/private.pem",
		"http://files.acme.io/keystore.p12",
		"http://files.acme.io/private_key.der",
		"http://files.acme.io/app.exe",
		"http://cdn.acme.io/ca.crt.tar",
		"http://api.acme.io/v1/crl",
		"http://api.acme.io/v1/login",
		"http://ocsp-proxy.acme.io/status", // first label is not exactly `ocsp`
	} {
		if !firesHTTPRemote(line) {
			t.Errorf("must still be reported: %s", line)
		}
	}
}

// Source headers and schema declarations quote license and standard URLs in
// their canonical http:// spelling. The PATH is part of the rule, because the
// same hosts serve real downloads.
func TestConnectionStrings_LicenseAndSchemaURLsAreIdentifiers(t *testing.T) {
	for _, line := range []string{
		"// Licensed under the Apache License: http://www.apache.org/licenses/LICENSE-2.0",
		"# See http://www.gnu.org/licenses/ for more details.",
		"You should have received a copy: http://www.fsf.org/licensing/licenses/lgpl.html",
		"SIL Open Font License: http://scripts.sil.org/OFL",
		"\"$schema\": \"http://json-schema.org/draft-07/schema#\"",
		"http://opensource.org/licenses/MIT",
		"http://creativecommons.org/licenses/by/4.0/",
	} {
		if firesHTTPRemote(line) {
			t.Errorf("a license/standard identifier must not be reported: %s", line)
		}
	}
	for _, line := range []string{
		"curl -O http://www.apache.org/dist/tomcat/tomcat-9/v9.0.1/bin/apache-tomcat-9.0.1.zip",
		"http://www.apache.org/",
		"wget http://www.gnu.org/software/bash/bash-5.2.tar.gz",
		"http://json-schema.org.evil-mirror.io/draft-07/schema#",
		"http://www.fsf.org.acme.io/licensing/x",
	} {
		if !firesHTTPRemote(line) {
			t.Errorf("a real cleartext download must still be reported: %s", line)
		}
	}
}

// The stock fake adversary of a security test. The label must be exactly one of
// the three, directly under the TLD.
func TestConnectionStrings_InventedAttackerHostsAreExamples(t *testing.T) {
	for _, line := range []string{
		`payload = "http://evil.com/x"`,
		"redirect to http://sub.attacker.com/login",
		"http://malicious.io/shell.sh",
		"http://evil.example/",
		"POST http://yourserver.com/hook",
	} {
		if firesHTTPRemote(line) {
			t.Errorf("an invented attacker host must not be reported: %s", line)
		}
	}
	for _, line := range []string{
		"http://evil-corp.com/x",
		"http://notevil.com/x",
		"http://evil.acme.io/x",
		"http://attackers.com/x",
		"http://yourcompany.com/x",
		"http://192.168.evil.net/api",     // an IP-looking prefix is an SSRF trick, never an example
		"http://169.254.attacker.io/meta", // even under a name the rule otherwise knows
	} {
		if !firesHTTPRemote(line) {
			t.Errorf("a real-looking host must still be reported: %s", line)
		}
	}
}

const conflictExample = "<<<<<<< HEAD\nconst b = 2;\n=======\nconst b = 3;\n>>>>>>> feature/x\n"

func mergeMarkerVerdict(t *testing.T, rel, content string) (severity string, line int) {
	t.Helper()
	root := initRepoWithFiles(t, map[string]string{rel: content})
	fs, err := checkMergeConflictMarkers(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fs {
		if f.FilePath == rel {
			return f.Severity, 0
		}
	}
	return "", 0
}

// slopless's docs/rules/VBC-006-B.md shows a conflict under "## Flagged". Every
// marker in it is inside one fenced block that holds the whole conflict. It is
// reported at warning, not error: see TestReview_ConflictInACodeFenceIsVisibleAndHonest.
func TestMergeMarkers_CompleteConflictInAFenceIsAnExample(t *testing.T) {
	doc := "# VBC-006-B\n\n## Flagged\n\n```ts\nconst a = 1;\n" + conflictExample + "```\n\nMore prose.\n"
	if got, _ := mergeMarkerVerdict(t, "docs/rule.md", doc); got != SeverityWarning {
		t.Errorf("a complete conflict inside a fenced block of a .md file is reported at warning (visible, not an error): severity = %q", got)
	}
	// Tilde fences and longer fences are fences too.
	for name, d := range map[string]string{
		"tilde":        "~~~\n" + conflictExample + "~~~\n",
		"four ticks":   "````md\n" + conflictExample + "````\n",
		"crlf":         "```\r\n" + strings.ReplaceAll(conflictExample, "\n", "\r\n") + "```\r\n",
		"markdown ext": "```\n" + conflictExample + "```\n",
	} {
		rel := "g.md"
		if name == "markdown ext" {
			rel = "g.markdown"
		}
		if got, _ := mergeMarkerVerdict(t, rel, d); got != SeverityWarning {
			t.Errorf("%s: severity = %q, want warning", name, got)
		}
	}
}

// The other axis: the reason this is a downgrade with conditions rather than a
// blanket skip. Each of these is a real conflict, or cannot be vouched for.
func TestMergeMarkers_RealConflictsStillError(t *testing.T) {
	cases := map[string]struct{ rel, content string }{
		"at the top level of a .md":           {"README.md", "# Title\n\n" + conflictExample},
		"a fence with only an opening marker": {"a.md", "```\n<<<<<<< HEAD\nx\n```\n"},
		"a fence with the wrong order":        {"a.md", "```\n>>>>>>> x\n=======\n<<<<<<< HEAD\n```\n"},
		"a fence with no separator":           {"a.md", "```\n<<<<<<< HEAD\nx\n>>>>>>> y\n```\n"},
		"an example plus a real conflict":     {"a.md", "```\n" + conflictExample + "```\n\n" + conflictExample},
		"conflict straddling a fence":         {"a.md", "<<<<<<< HEAD\n```sh\nA\n=======\n```sh\nB\n>>>>>>> x\n"},
		"a fence that is never closed":        {"a.md", "```\n" + conflictExample},
		"the same fenced text in a .txt":      {"notes.txt", "```\n" + conflictExample + "```\n"},
		"the same fenced text in a .go":       {"x.go", "/*\n```\n" + conflictExample + "```\n*/\n"},
		"the same fenced text in a .rst":      {"x.rst", "```\n" + conflictExample + "```\n"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got, _ := mergeMarkerVerdict(t, c.rel, c.content); got != SeverityError {
				t.Errorf("severity = %q, want error", got)
			}
		})
	}
}

// When a real conflict and an example share a file, the REAL one is reported.
func TestMergeMarkers_RealConflictWinsOverAnExampleInTheSameFile(t *testing.T) {
	doc := "```\n" + conflictExample + "```\n\nprose\n\n" + conflictExample
	line, example, ok := findMergeMarker("a.md", []byte(doc))
	if !ok || example {
		t.Fatalf("want a real conflict, got ok=%v example=%v", ok, example)
	}
	// Lines: 1 fence, 2-6 the example, 7 fence, 8 blank, 9 prose, 10 blank,
	// 11 the top-level marker. The example's marker (line 2) must NOT win.
	if line != 11 {
		t.Errorf("reported line %d, want the top-level marker at 11", line)
	}
}

// Non-Markdown files keep exactly the old behaviour, through the old function.
func TestMergeMarkers_FindFirstMergeMarkerUnchanged(t *testing.T) {
	line, ok := findFirstMergeMarker([]byte("a\nb\n<<<<<<< HEAD\nc\n"))
	if !ok || line != 3 {
		t.Errorf("findFirstMergeMarker = (%d, %v), want (3, true)", line, ok)
	}
}

// The gate read the password up to the FIRST `@`, so `user:p@ss@host` had the
// password `p`, one character, dropped as prose shorthand, and was never
// reported. A raw `@` in a password is common in sloppy configuration; the
// reviewer of the redaction work noticed the scanner and the redactor disagreed
// about where the userinfo ends. They now share userinfoEnd.
func TestConnectionStrings_AtSignInAPasswordIsStillReported(t *testing.T) {
	for _, line := range []string{
		"postgres://app:p@ss@db.prod.acme.io/app",
		"DATABASE_URL=postgres://app:Xk9@mQ2!vL@db.prod.acme.io:5432/app",
		"sqlserver://sa:Pa$$w@rd9@prod-db.acme.io:1433/app",
	} {
		if !firesCredsInURL(line) {
			t.Errorf("a password containing @ must still be reported: %s", line)
		}
	}
	// An `@` in the QUERY must not be read as the end of the userinfo: the
	// password here is `pw`, two characters, and stays shorthand; and a
	// placeholder followed by a query containing `@` stays a placeholder.
	for _, line := range []string{
		"https://user:pw@host.acme.io?x=a@b",
		"https://user:changeme@host.acme.io?next=a@b.c",
		"https://example.com/contact/someone@example.com",
	} {
		if firesCredsInURL(line) {
			t.Errorf("an @ outside the userinfo must not create a credential: %s", line)
		}
	}
}
