package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The gate reference on the docs site is hand-written prose keyed by gate id.
// Nothing structural connected the two, and they had already drifted: the site
// advertised "34 built-in gates" and listed 34 rows while the registry held 35,
// so config_parse_error shipped undocumented. The sidebar had drifted the other
// way too, linking ~35 gate pages of which three existed — every other link was
// a 404 in production.
//
// These tests make both directions of that drift a build failure.

var gateMetaRe = regexp.MustCompile(`<GateMeta\s+id="([^"]+)"\s+severity="([^"]+)"\s+tags="([^"]*)"`)

// docsGatesDir locates docs/gates relative to the server package, and reports
// whether it is there at all — a checkout of server/ on its own is a legitimate
// setup, and these tests have nothing to say about it.
func docsGatesDir(t *testing.T) (string, bool) {
	t.Helper()
	dir := filepath.Join("..", "docs", "gates")
	if _, err := os.Stat(dir); err != nil {
		return "", false
	}
	return dir, true
}

func gateDocSlug(gateID string) string { return strings.ReplaceAll(gateID, "_", "-") }

func TestDocs_EveryGateHasAPage(t *testing.T) {
	dir, ok := docsGatesDir(t)
	if !ok {
		t.Skip("docs/gates not present in this checkout")
	}
	for _, g := range gateRegistry() {
		page := filepath.Join(dir, gateDocSlug(g.ID)+".md")
		if _, err := os.Stat(page); err != nil {
			t.Errorf("gate %q has no documentation page (expected docs/gates/%s.md)",
				g.ID, gateDocSlug(g.ID))
		}
	}
}

func TestDocs_EveryPageIsARealGate(t *testing.T) {
	dir, ok := docsGatesDir(t)
	if !ok {
		t.Skip("docs/gates not present in this checkout")
	}
	known := map[string]bool{}
	for _, g := range gateRegistry() {
		known[gateDocSlug(g.ID)] = true
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".md") || name == "index.md" {
			continue
		}
		slug := strings.TrimSuffix(name, ".md")
		if !known[slug] {
			t.Errorf("docs/gates/%s documents a gate that is not in the registry", name)
		}
	}
}

// A page can exist and still lie. The <GateMeta> block states the gate's id,
// severity and tags, and a severity that no longer matches the registry is the
// kind of wrong that survives review because the page still looks right.
func TestDocs_GateMetaMatchesRegistry(t *testing.T) {
	dir, ok := docsGatesDir(t)
	if !ok {
		t.Skip("docs/gates not present in this checkout")
	}
	for _, g := range gateRegistry() {
		g := g
		t.Run(g.ID, func(t *testing.T) {
			page := filepath.Join(dir, gateDocSlug(g.ID)+".md")
			body, err := os.ReadFile(page)
			if err != nil {
				t.Skipf("no page: %v", err) // TestDocs_EveryGateHasAPage owns this failure
			}
			m := gateMetaRe.FindSubmatch(body)
			if m == nil {
				t.Fatalf("no <GateMeta id=… severity=… tags=…> block in docs/gates/%s.md",
					gateDocSlug(g.ID))
			}
			if got := string(m[1]); got != g.ID {
				t.Errorf("GateMeta id = %q, registry says %q", got, g.ID)
			}
			if got := string(m[2]); got != g.Severity {
				t.Errorf("GateMeta severity = %q, registry says %q", got, g.Severity)
			}
			if got := string(m[3]); got != g.Tags {
				t.Errorf("GateMeta tags = %q, registry says %q", got, g.Tags)
			}
		})
	}
}

// The index is the page people actually browse. A gate missing from it is
// undiscoverable even when its own page is perfect.
func TestDocs_IndexLinksEveryGate(t *testing.T) {
	dir, ok := docsGatesDir(t)
	if !ok {
		t.Skip("docs/gates not present in this checkout")
	}
	body, err := os.ReadFile(filepath.Join(dir, "index.md"))
	if err != nil {
		t.Fatal(err)
	}
	index := string(body)
	for _, g := range gateRegistry() {
		link := fmt.Sprintf("(/gates/%s)", gateDocSlug(g.ID))
		if !strings.Contains(index, link) {
			t.Errorf("gate %q is not linked from the gate reference index", g.ID)
		}
	}
}

// ruleTableRe matches a row of a per-rule documentation table:
//
//	| `rule_id` | severity … | description |
//
// Only the first word of the severity cell counts, so `info, **off by
// default**` reads as info.
var ruleTableRe = regexp.MustCompile("(?m)^\\| `([a-z_]+)` \\| (error|warning|info)\\b")

// The gate-level guard above checks each page's <GateMeta> against the gate's
// DEFAULT severity. It could not see a per-rule table, and that is how
// connection-strings.md came to say "everything else reports at info" while
// ftp, telnet, smb, nfs and rsync were warning in code — written from memory,
// and wrong from the day it was published.
//
// Every gate with a per-rule table is pinned here in both directions: each
// documented row must carry the code's severity, and each rule in code must
// have a row.
func TestDocs_PerRuleSeveritiesMatchCode(t *testing.T) {
	dir, ok := docsGatesDir(t)
	if !ok {
		t.Skip("docs/gates not present in this checkout")
	}
	tables := map[string]map[string]string{
		"dockerfile-lint":    {},
		"compose-lint":       {},
		"html-lint":          {},
		"css-lint":           {},
		"markdown-lint":      {},
		"connection-strings": {},
	}
	for _, r := range dockerfileRules {
		tables["dockerfile-lint"][r.id] = r.severity
	}
	for id, r := range composeRules {
		tables["compose-lint"][id] = r.severity
	}
	for id, r := range htmlRules {
		tables["html-lint"][id] = r.severity
	}
	for id, r := range cssRules {
		tables["css-lint"][id] = r.severity
	}
	for id, r := range mdRules {
		tables["markdown-lint"][id] = r.severity
	}
	for _, p := range connectionPatterns {
		tables["connection-strings"][p.id] = p.severity
	}

	for slug, code := range tables {
		slug, code := slug, code
		t.Run(slug, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(dir, slug+".md"))
			if err != nil {
				t.Fatal(err)
			}
			documented := map[string]string{}
			for _, m := range ruleTableRe.FindAllStringSubmatch(string(body), -1) {
				documented[m[1]] = m[2]
			}
			for id, sev := range code {
				got, ok := documented[id]
				switch {
				case !ok:
					t.Errorf("rule %q exists in code but has no row in docs/gates/%s.md", id, slug)
				case got != sev:
					t.Errorf("rule %q: docs say %s, code says %s", id, got, sev)
				}
			}
			for id := range documented {
				if _, ok := code[id]; !ok {
					t.Errorf("docs/gates/%s.md documents rule %q, which does not exist in code", slug, id)
				}
			}
		})
	}
}
