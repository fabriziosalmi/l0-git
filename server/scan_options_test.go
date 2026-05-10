package main

import (
	"encoding/json"
	"testing"
)

func TestIsDefaultFixturePath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		// Test files by basename convention
		{"server/secrets_test.go", true},
		{"src/foo_test.go", true},
		{"thing.test.ts", true},
		{"thing.spec.tsx", true},
		{"things_test.rs", true},
		{"ThingTest.java", true},
		{"ThingTests.kt", true},
		{"thing_spec.rb", true},
		{"thing_test.rb", true},
		{"test_things.py", true},
		{"things_test.py", true},
		{"conftest.py", true},
		{"pkg/conftest.py", true},

		// Directory traversal
		{"test/fixtures/foo.json", true},
		{"tests/integration/foo.go", true},
		{"src/__tests__/foo.tsx", true},
		{"spec/foo.rb", true},
		{"testdata/big.bin", true},
		{"src/fixtures/leaky.txt", true},
		{"src/__fixtures__/payload.json", true},

		// Genuine source — must NOT match
		{"src/main.go", false},
		{"server/secrets.go", false},
		{"docs/intro.md", false},
		{"package.json", false},
		{"contesting.md", false}, // word starts with "contest" but isn't a test path
		{"specification.txt", false},
		{"testbed.py", false}, // base "testbed.py" doesn't match test_*.py / *_test.py
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			if got := isDefaultFixturePath(c.path); got != c.want {
				t.Errorf("%q: got %v, want %v", c.path, got, c.want)
			}
		})
	}
}

func boolPtr(b bool) *bool { return &b }

func TestScanOptions_ShouldSkip(t *testing.T) {
	// ExcludePaths only, fixture skip explicitly disabled.
	o := scanOptions{ExcludePaths: []string{"vendor/*"}, SkipDefaultFixturePaths: boolPtr(false)}
	if !o.shouldSkip("vendor/lib.go") {
		t.Errorf("ExcludePaths should match")
	}
	if o.shouldSkip("server/main_test.go") {
		t.Errorf("fixture path must NOT skip when SkipDefaultFixturePaths is false")
	}

	// SkipDefaultFixturePaths explicitly true.
	o = scanOptions{SkipDefaultFixturePaths: boolPtr(true)}
	if !o.shouldSkip("server/main_test.go") {
		t.Errorf("fixture path must skip when SkipDefaultFixturePaths is true")
	}
	if o.shouldSkip("server/main.go") {
		t.Errorf("non-fixture path must not skip")
	}

	// Both: union semantics.
	o = scanOptions{ExcludePaths: []string{"vendor/*"}, SkipDefaultFixturePaths: boolPtr(true)}
	if !o.shouldSkip("vendor/lib.go") || !o.shouldSkip("foo_test.go") {
		t.Errorf("union semantics broken: %+v", o)
	}

	// parseScanOptions with empty opts defaults SkipDefaultFixturePaths to true.
	parsed := parseScanOptions(nil)
	if parsed.SkipDefaultFixturePaths == nil || !*parsed.SkipDefaultFixturePaths {
		t.Errorf("parseScanOptions nil opts must default SkipDefaultFixturePaths to true")
	}
	if !parsed.shouldSkip("foo_test.go") {
		t.Errorf("parsed default opts must skip fixture paths")
	}

	// parseScanOptions with explicit false respects the override.
	parsedOff := parseScanOptions(json.RawMessage(`{"skip_default_fixture_paths":false}`))
	if parsedOff.SkipDefaultFixturePaths == nil || *parsedOff.SkipDefaultFixturePaths {
		t.Errorf("explicit false must be respected")
	}
	if parsedOff.shouldSkip("foo_test.go") {
		t.Errorf("explicit false must not skip fixture paths")
	}
}
