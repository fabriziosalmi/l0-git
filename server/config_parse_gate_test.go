package main

import (
	"strings"
	"testing"
)

func TestConfigKind(t *testing.T) {
	cases := map[string]cfgKind{
		"package.json":          cfgJSON,
		"composer.json":         cfgJSON,
		"sub/dir/app.json":      cfgJSON,
		"k8s.yaml":              cfgYAML,
		"deploy.yml":            cfgYAML,
		".github/workflows/ci.yml": cfgYAML,
		// JSONC family — never parsed as strict JSON.
		"tsconfig.json":         cfgNone,
		"tsconfig.build.json":   cfgNone,
		"jsconfig.json":         cfgNone,
		".vscode/settings.json": cfgNone,
		".devcontainer/devcontainer.json": cfgNone,
		"theme.jsonc":           cfgNone,
		// Not config we parse.
		"data.jsonl":            cfgNone,
		"README.md":             cfgNone,
		"main.go":               cfgNone,
	}
	for rel, want := range cases {
		if got := configKind(rel); got != want {
			t.Errorf("configKind(%q) = %d, want %d", rel, got, want)
		}
	}
}

// TestConfigParseNoFalsePositives is the zero-FP contract: every input here is
// a legitimately-valid (or legitimately-skipped) config and MUST NOT flag.
func TestConfigParseNoFalsePositives(t *testing.T) {
	cases := []struct {
		name string
		kind cfgKind
		rel  string
		data string
	}{
		{"plain json object", cfgJSON, "package.json", `{"name":"x","version":"1.0.0"}`},
		{"json array", cfgJSON, "a.json", `[1,2,3]`},
		{"json nested + unicode", cfgJSON, "a.json", `{"a":{"b":[true,null,"http://x.test"]},"e":"café"}`},
		{"json with UTF-8 BOM", cfgJSON, "a.json", "\xef\xbb\xbf{\"a\":1}"},
		// A plain .json that happens to use JSONC niceties: rescued, never flagged.
		{"json with line comment", cfgJSON, "config.json", "{\n  // a note\n  \"a\": 1\n}"},
		{"json with block comment", cfgJSON, "config.json", "{/* hdr */\"a\":1}"},
		{"json with trailing comma", cfgJSON, "config.json", "{\n  \"a\": 1,\n}"},
		{"json comment-ish inside string", cfgJSON, "a.json", `{"url":"http://x//y","note":"a, b,"}`},

		{"simple yaml", cfgYAML, "c.yml", "a: 1\nb: two\n"},
		{"multi-doc yaml", cfgYAML, "k8s.yaml", "---\nkind: A\n---\nkind: B\n"},
		{"yaml comments only", cfgYAML, "c.yml", "# just a comment\n"},
		{"empty yaml is valid", cfgYAML, "c.yml", "\n"},
		{"yaml custom tag (cloudformation)", cfgYAML, "tpl.yaml", "Resources:\n  Bucket:\n    Name: !Ref MyBucket\n  Arn: !GetAtt Bucket.Arn\n"},
		{"yaml anchors/aliases", cfgYAML, "c.yml", "base: &b\n  x: 1\nuse:\n  <<: *b\n"},
		// Templates are not standalone YAML — skipped, never flagged.
		{"helm template braces", cfgYAML, "templates/deploy.yaml", "image: {{ .Values.image }}\nport: {{ .Values.port }}\n"},
		{"erb template", cfgYAML, "config/database.yml", "production:\n  host: <%= ENV['DB_HOST'] %>\n"},
		{"gh actions expression (valid yaml plain scalar)", cfgYAML, ".github/workflows/ci.yml", "jobs:\n  b:\n    steps:\n      - run: echo done\n"},
	}
	for _, c := range cases {
		if msg, bad := configParseError(c.kind, c.rel, []byte(c.data)); bad {
			t.Errorf("FALSE POSITIVE %q: flagged a valid/skipped config: %s", c.name, msg)
		}
	}
}

// TestConfigParseTruePositives: genuinely-broken configs MUST flag.
func TestConfigParseTruePositives(t *testing.T) {
	cases := []struct {
		name string
		kind cfgKind
		rel  string
		data string
	}{
		{"json missing value", cfgJSON, "package.json", `{"a": }`},
		{"json unterminated", cfgJSON, "package.json", `{"a": 1`},
		{"json garbage", cfgJSON, "a.json", `{not json at all}`},
		{"json double comma not rescuable", cfgJSON, "a.json", `{"a": 1,, "b": 2}`},
		{"yaml tab indentation", cfgYAML, "c.yml", "a:\n\tb: 1\n"},
		{"yaml unterminated quote", cfgYAML, "c.yml", "a: \"unterminated\nb: 2\n"},
		{"yaml bad mapping", cfgYAML, "c.yml", "a: b: c\n"},
	}
	for _, c := range cases {
		if _, bad := configParseError(c.kind, c.rel, []byte(c.data)); !bad {
			t.Errorf("FALSE NEGATIVE %q: failed to flag broken config", c.name)
		}
	}
}

func TestLooksLikeTemplate(t *testing.T) {
	yes := []string{
		"a: {{ x }}", "h: <%= y %>", "x: {{- trim }}",
		// Jinja/Salt/Ansible control blocks with NO {{ }} expression.
		"{% if grains['os'] == 'Ubuntu' %}\napache:\n  pkg.installed: []\n{% endif %}",
		"{%- for i in services %}",
	}
	no := []string{"a: 1", "url: http://x/y", "a: '{ literal brace }'"}
	for _, s := range yes {
		if !looksLikeTemplate([]byte(s)) {
			t.Errorf("looksLikeTemplate(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if looksLikeTemplate([]byte(s)) {
			t.Errorf("looksLikeTemplate(%q) = true, want false", s)
		}
	}
}

func TestStripJSONCKeepsStrings(t *testing.T) {
	// The // inside the string must survive; the trailing // comment goes.
	in := "{\n  \"url\": \"http://example.test/a//b\", // trailing\n}"
	got := string(stripJSONC([]byte(in)))
	if !strings.Contains(got, "http://example.test/a//b") {
		t.Errorf("stripJSONC ate a // inside a string: %q", got)
	}
}
