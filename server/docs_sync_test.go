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
