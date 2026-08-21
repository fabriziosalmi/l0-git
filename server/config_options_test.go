package main

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

func cfgWith(t *testing.T, raw string) *ProjectConfig {
	t.Helper()
	c := &ProjectConfig{}
	if err := json.Unmarshal([]byte(raw), c); err != nil {
		t.Fatalf("fixture config is not valid JSON: %v", err)
	}
	return c
}

// Every one of these used to be accepted in silence: the gate's own
// `_ = json.Unmarshal(opts, &o)` discarded the error and ran on defaults, so
// the config file did not do what it said and nothing anywhere said so.
func TestValidateGateOptions_ReportsWhatUsedToBeSwallowed(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			"wrong type",
			`{"gate_options":{"large_file_tracked":{"threshold_mb":"20"}}}`,
			"cannot unmarshal string",
		},
		{
			"typo in an option key",
			`{"gate_options":{"large_file_tracked":{"treshold_mb":20}}}`,
			`unknown field "treshold_mb"`,
		},
		{
			"typo in the shared exclude_paths",
			`{"gate_options":{"dead_placeholders":{"exclude_path":["src/*"]}}}`,
			`unknown field "exclude_path"`,
		},
		{
			"string where a list belongs",
			`{"gate_options":{"dead_placeholders":{"exclude_paths":"src/*"}}}`,
			"cannot unmarshal string",
		},
		{
			"gate id that does not exist",
			`{"gate_options":{"secrets":{"exclude_paths":["x"]}}}`,
			"no gate with that id",
		},
		{
			"gate that takes no options",
			`{"gate_options":{"readme_present":{"threshold_mb":1}}}`,
			"takes no options",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _ := validateGateOptions(cfgWith(t, c.raw), gateRegistry())
			if len(got) != 1 {
				t.Fatalf("expected exactly one problem, got %d: %v", len(got), got)
			}
			if !strings.Contains(got[0], c.want) {
				t.Errorf("problem %q does not mention %q", got[0], c.want)
			}
		})
	}
}

func TestValidateGateOptions_AcceptsValidConfig(t *testing.T) {
	raw := `{"gate_options":{
		"large_file_tracked":{"threshold_mb":20,"exclude_paths":["dist/*"]},
		"secrets_scan_history":{"enabled":true,"max_blobs":10000},
		"markdown_lint":{"enabled_rules":["codeblock_no_language"]},
		"network_scan":{"report_loopback":true},
		"compose_lint":{"additional_orchestrator_images":["my-org/deployer"]}
	}}`
	if got, _ := validateGateOptions(cfgWith(t, raw), gateRegistry()); len(got) != 0 {
		t.Errorf("valid config reported problems: %v", got)
	}
}

// Reported in a stable order — the caller joins these into one string, and map
// iteration would reshuffle it between otherwise identical runs.
func TestValidateGateOptions_DeterministicOrder(t *testing.T) {
	raw := `{"gate_options":{"zzz_nope":{},"aaa_nope":{},"mmm_nope":{}}}`
	first, _ := validateGateOptions(cfgWith(t, raw), gateRegistry())
	if len(first) != 3 {
		t.Fatalf("expected 3 problems, got %v", first)
	}
	if !sort.StringsAreSorted(first) {
		t.Errorf("problems are not in a stable order: %v", first)
	}
	for i := 0; i < 20; i++ {
		if got, _ := validateGateOptions(cfgWith(t, raw), gateRegistry()); !equalStrings(got, first) {
			t.Fatalf("order changed between runs:\n%v\n%v", first, got)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// A prototype that is not a struct pointer, or one that somehow permits
// unknown fields, would make validation silently useless for that gate —
// which is the exact failure being fixed, reintroduced one gate at a time.
func TestGateOptions_EveryPrototypeRejectsUnknownFields(t *testing.T) {
	for _, g := range gateRegistry() {
		if g.NewOptions == nil {
			continue
		}
		t.Run(g.ID, func(t *testing.T) {
			raw := `{"gate_options":{"` + g.ID + `":{"__definitely_not_an_option__":1}}}`
			got, _ := validateGateOptions(cfgWith(t, raw), gateRegistry())
			if len(got) != 1 || !strings.Contains(got[0], "unknown field") {
				t.Errorf("prototype does not reject unknown fields, got: %v", got)
			}
		})
	}
}

// The omission this guards against: a gate starts reading options but nobody
// adds NewOptions, so its whole options block goes back to being validated by
// nothing. A gate that truly ignores options must produce identical findings
// whether or not options are passed — so if this fails, the gate reads them.
func TestGateOptions_GatesWithoutAPrototypeReallyIgnoreOptions(t *testing.T) {
	root := initRepoWithCommit(t, map[string]string{
		"README.md":         "# x\n\nTODO: something\n",
		"src/app.js":        "const u = 'http://198.51.100.7:8080';\n",
		"src/page.html":     `<meta name="viewport" content="user-scalable=no">`,
		"style.css":         "body { text-align: justify; }\n",
		"Dockerfile":        "FROM node:latest\nCMD [\"node\"]\n",
		".env.example":      "TOKEN=\n",
		"docs/guide.md":     "![](missing.png)\n",
		"big/data.bin":      strings.Repeat("x", 1024),
		".vscode/junk.json": "{}\n",
	})
	// A blanket exclusion: any gate that reads scan options at all behaves
	// differently under it.
	opts := json.RawMessage(`{"exclude_paths":["**","*","*/*","*/*/*"]}`)

	for _, g := range gateRegistry() {
		if g.NewOptions != nil {
			continue
		}
		g := g
		t.Run(g.ID, func(t *testing.T) {
			withNil, err := g.Check(context.Background(), root, nil)
			if err != nil {
				t.Fatalf("check with nil options: %v", err)
			}
			withOpts, err := g.Check(context.Background(), root, opts)
			if err != nil {
				t.Fatalf("check with options: %v", err)
			}
			// Compare the findings themselves, not how many there are: a gate
			// could start honouring an option that changes a severity, a
			// message or a path while the count stays put, and a count-only
			// assertion would stay green while validation was being bypassed.
			if a, b := fingerprint(withNil), fingerprint(withOpts); a != b {
				t.Errorf("gate has no NewOptions but reacts to them — give it a "+
					"prototype so gate_options.%s is validated\n with nil:  %s\n with opts: %s",
					g.ID, a, b)
			}
		})
	}
}

// fingerprint renders findings into a stable, comparable form. Gates are free
// to emit in any order, so it sorts.
func fingerprint(fs []Finding) string {
	parts := make([]string, 0, len(fs))
	for _, f := range fs {
		parts = append(parts, strings.Join([]string{
			f.Severity, f.Title, f.Message, f.FilePath, f.Tags,
		}, "|"))
	}
	sort.Strings(parts)
	return strings.Join(parts, "\n")
}
