package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Found by re-running l0-git over the author's public, non-archived repositories
// on 2026-10-05 (89 repos, v0.3.0). Each class has the case it must NOT silence.

func linkFindings(t *testing.T, files map[string]string, rel string) []Finding {
	t.Helper()
	root := t.TempDir()
	for p, c := range files {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var out []Finding
	for _, f := range evaluateMarkdownFile(rel, root, []byte(files[rel]), nil) {
		if strings.HasSuffix(f.FilePath, ":link_local_broken") || strings.HasSuffix(f.FilePath, ":link_anchor_broken") {
			out = append(out, f)
		}
	}
	return out
}

// 15 of 64 network warnings were synthetic /24 networks in Rust tests. The
// synthetic-octet rule needs a non-zero last octet, which a network address
// never has. The same /24 of a well-known resolver is the provider's own prefix.
func TestNetworkScan_SyntheticAndResolverNetworks(t *testing.T) {
	for _, c := range []string{"1.2.3.0/24", "2.2.2.0/24", "3.3.3.0/24", "3.2.1.0/24", "4.3.2.0/24", "5.6.7.0/24", "9.8.7.0/24"} {
		if got := networkCats(t, "allow = "+c); len(got) != 1 || got[0] != "info/cidr_doc-placeholder" {
			t.Errorf("%s is an invented network: want one info doc-placeholder, got %v", c, got)
		}
	}
	for _, c := range []string{"1.1.1.0/24", "1.0.0.0/24", "8.8.8.0/24", "8.8.4.0/24", "9.9.9.0/24"} {
		if got := networkCats(t, "allow = "+c); len(got) != 1 || got[0] != "info/cidr_public-resolver" {
			t.Errorf("%s is a resolver provider's prefix: want info public-resolver, got %v", c, got)
		}
	}
	// A network that is already classified keeps its category: the refinement
	// only ever moves a PUBLIC range.
	for c, want := range map[string]string{
		"10.11.12.0/24": "info/cidr_private", "172.17.18.0/24": "info/cidr_private",
		"192.168.1.0/24": "info/cidr_private",
	} {
		if got := networkCats(t, "allow = "+c); len(got) != 1 || got[0] != want {
			t.Errorf("%s: want %s, got %v", c, want, got)
		}
	}
	// The other axis: real prefixes stay warnings, and so do wide ones.
	for _, c := range []string{
		"51.222.140.0/24", "46.250.245.0/24", "100.1.2.0/24", "100.1.1.0/24", "20.30.41.0/24", "1.2.4.0/24",
		"1.0.0.0/8",    // contains 1.1.1.1 but is not the resolver's prefix
		"8.8.0.0/16",   // same
		"1.2.3.0/16",   // synthetic only counts at /24 or narrower
		"1.2.3.128/25", // a host-sized slice is not a network address
	} {
		if got := networkCats(t, "allow = "+c); len(got) != 1 || !strings.HasPrefix(got[0], "warning/cidr_public") {
			t.Errorf("%s must stay a public-network warning, got %v", c, got)
		}
	}
}

// `[x](./vulnerability-remediation-v11.2)` under VitePress is the page
// vulnerability-remediation-v11.2.md. The extensionless fallback was skipped
// because filepath.Ext saw ".2".
func TestMarkdown_VersionNumberIsNotAnExtension(t *testing.T) {
	files := map[string]string{
		"docs/.vitepress/config.mts":              "export default {}\n",
		"docs/index.md":                           "[a](./vulnerability-remediation-v11.2) [b](./release-1.0) [c](./guide.v2)\n",
		"docs/vulnerability-remediation-v11.2.md": "# x\n",
		"docs/release-1.0/index.md":               "# y\n",
		"docs/guide.v2.md":                        "# z\n",
	}
	if got := linkFindings(t, files, "docs/index.md"); len(got) != 0 {
		t.Errorf("a dotted page name is not an extension: %+v", got)
	}
	// Outside a site generator the same names are plain missing files: on
	// GitHub `./release-1.0` does not open release-1.0.md.
	bare := map[string]string{}
	for k, v := range files {
		if !strings.Contains(k, ".vitepress") {
			bare[k] = v
		}
	}
	// (`./release-1.0` is a directory holding index.md, so that one resolves
	// on disk either way.)
	if got := linkFindings(t, bare, "docs/index.md"); len(got) != 2 {
		t.Errorf("without a site generator the 2 page-named links are broken, got %d: %+v", len(got), got)
	}
	// The typo guard stays: a REAL extension that does not exist, and a numeric
	// one with no page, are reported.
	// A real file type is not a version: `diagram.png` does not open
	// diagram.png.md, even under a site generator.
	typo := map[string]string{
		"docs/.vitepress/config.mts": "export default {}\n",
		"docs/index.md":              "[a](./diagram.png)\n",
		"docs/diagram.png.md":        "# not the image\n",
	}
	if got := linkFindings(t, typo, "docs/index.md"); len(got) != 1 {
		t.Errorf("a file-type extension must not fall back to a page: got %d", len(got))
	}
	files["docs/index.md"] = "[a](./missing-v11.2) [b](./guide.mdd) [c](./release-9.9)\n"
	if got := linkFindings(t, files, "docs/index.md"); len(got) != 3 {
		t.Errorf("want 3 broken links, got %d: %+v", len(got), got)
	}
}

// `[x](../../security/advisories)` from a root file is how a README links to the
// repository's own GitHub pages: the base URL is /owner/repo/blob/<branch>/, so
// two `..` leave the file tree and land on /owner/repo/.
func TestMarkdown_GitHubRelativeRepoPages(t *testing.T) {
	ok := map[string]string{
		"SECURITY.md": "[a](../../security/advisories) [b](../../issues) [c](../../issues/new) " +
			"[d](../../discussions/new?category=ideas) [e](../../actions) [f](../../pulls) [g](../../wiki) [h](../../releases)\n",
	}
	if got := linkFindings(t, ok, "SECURITY.md"); len(got) != 0 {
		t.Errorf("GitHub-relative repo pages must not be reported: %+v", got)
	}
	// Depth matters: from docs/guide/ it takes FOUR `..` to reach the repo URL.
	deep := map[string]string{"docs/guide/x.md": "[a](../../../../issues) [b](../../../../security/advisories)\n"}
	if got := linkFindings(t, deep, "docs/guide/x.md"); len(got) != 0 {
		t.Errorf("depth-aware GitHub-relative links must not be reported: %+v", got)
	}
	// The other axis.
	bad := map[string]string{
		"SECURITY.md":     "[a](../../docs/guide.md) [b](../../settings) [c](../issues) [d](../../../issues) [e](../../notapage) [f](../../issues/../settings)\n",
		"docs/guide/x.md": "[f](../../issues) [g](../../../issues)\n",
	}
	if got := linkFindings(t, bad, "SECURITY.md"); len(got) != 6 {
		t.Errorf("root file: want 6 broken, got %d: %+v", len(got), got)
	}
	if got := linkFindings(t, bad, "docs/guide/x.md"); len(got) != 2 {
		t.Errorf("a wrong number of `..` for the depth must still be reported: got %d: %+v", len(got), got)
	}
}

// GitHub numbers a repeated heading: the second "Overview" is #overview-1
// (checked against a README rendered by github.com). The gate only knew the
// first, so every link to a repeated section was reported.
func TestMarkdown_DuplicateHeadingsAreNumberedLikeGitHub(t *testing.T) {
	doc := "# T\n\n## Usage\n\n## Usage\n\n## Usage\n\n[a](#usage) [b](#usage-1) [c](#usage-2)\n"
	if got := linkFindings(t, map[string]string{"R.md": doc}, "R.md"); len(got) != 0 {
		t.Errorf("#usage, #usage-1 and #usage-2 all exist: %+v", got)
	}
	doc = "# T\n\n## Usage\n\n## Usage\n\n[a](#usage-2) [b](#usage-3) [c](#usage-0)\n"
	if got := linkFindings(t, map[string]string{"R.md": doc}, "R.md"); len(got) != 3 {
		t.Errorf("only two headings: #usage-2, #usage-3 and #usage-0 do not exist; got %d", len(got))
	}
	// Numbering is per slug, not global.
	doc = "# T\n\n## A\n\n## B\n\n## A\n\n[x](#a-1) [y](#b-1)\n"
	if got := linkFindings(t, map[string]string{"R.md": doc}, "R.md"); len(got) != 1 || !strings.Contains(got[0].Message, "#b-1") {
		t.Errorf("only #a-1 exists; #b-1 must be reported alone: %+v", got)
	}
}

// `[x](#)` points at the top of the page; and an anchor to a non-ASCII heading
// is often written percent-encoded.
func TestMarkdown_EmptyAndPercentEncodedAnchors(t *testing.T) {
	doc := "# Café Ünï\n\n[top](#) [a](#caf%C3%A9-%C3%BCn%C3%AF) [b](#café-ünï)\n"
	if got := linkFindings(t, map[string]string{"R.md": doc}, "R.md"); len(got) != 0 {
		t.Errorf("an empty anchor and a percent-encoded one are valid: %+v", got)
	}
	doc = "# Café\n\n[a](#nope%20x) [b](#cafe)\n"
	if got := linkFindings(t, map[string]string{"R.md": doc}, "R.md"); len(got) != 2 {
		t.Errorf("anchors that match no heading must still be reported: got %d", len(got))
	}
}

func firesLegacy(line, id string) bool {
	for _, f := range scanConnectionLine("conf.txt", 1, []byte(line+"\n")) {
		if strings.HasSuffix(f.FilePath, ":"+id) {
			return true
		}
	}
	return false
}

// Cleartext FTP to the machine itself exposes nothing, as http://localhost is
// already exempt. And `ftp://,` or a regex-escaped `127\.0\.0\.1!` is a mention
// or a pattern, not a connection.
func TestConnectionStrings_LegacySchemesToLoopbackAndNonHosts(t *testing.T) {
	for _, c := range []struct{ line, id string }{
		{"ftp://localhost/pub", "ftp"}, {"ftp://127.0.0.1:2121/pub", "ftp"}, {"ftp://[::1]/x", "ftp"},
		{"ftp://0.0.0.0/x", "ftp"}, {"telnet://localhost:2323", "telnet"}, {"smb://127.0.0.1/share", "smb"},
		{"nfs://localhost/exports", "nfs"}, {"rsync://127.0.0.1/mod", "rsync"}, {"ldap://localhost:389", "ldap_unencrypted"},
		{"ftp://127.0.0.1!", "ftp"}, {`ftp://127\.0\.0\.1!`, "ftp"}, {"schemes: http://, ftp://, sftp://", "ftp"},
		{"ftp://,", "ftp"}, {"ftp://;x", "ftp"},
		{"ftp://anonymous@localhost/pub", "ftp"}, {"ftp://ftp@127.0.0.1:21/", "ftp"},
	} {
		if firesLegacy(c.line, c.id) {
			t.Errorf("must not be reported: %s", c.line)
		}
	}
	// A LAN host is still cleartext on a wire; so is anything that only LOOKS local.
	for _, c := range []struct{ line, id string }{
		{"ftp://files.acme.io/x", "ftp"}, {"ftp://pve.lan/x", "ftp"}, {"ftp://192.168.1.10/x", "ftp"},
		{"ftp://127.0.0.1.evil.com/x", "ftp"}, {"ftp://localhost.evil.io/x", "ftp"}, {"ftp://10.0.0.5/x", "ftp"},
		{"telnet://router.acme.io", "telnet"}, {"ldap://dc.acme.io", "ldap_unencrypted"},
		{"ftp://user@127.0.0.1.evil.com/x", "ftp"},
	} {
		if !firesLegacy(c.line, c.id) {
			t.Errorf("must still be reported: %s", c.line)
		}
	}
}

// The loopback exemption is for the legacy rules only: a password in a URL is
// still in the repository whatever machine the URL names.
func TestConnectionStrings_LoopbackExemptionDoesNotReachCredentials(t *testing.T) {
	for _, line := range []string{
		"ftp://svc:Zq8nRt4vLw2@localhost/x",
		"postgres://svc:Zq8nRt4vLw2@127.0.0.1/app",
	} {
		if !firesLegacy(line, "creds_in_url") {
			t.Errorf("a credential on a loopback host must still be reported: %s", line)
		}
	}
}
