package main

import (
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

func TestScanOptions_ShouldSkip(t *testing.T) {
	// ExcludePaths only — no fixture skip.
	o := scanOptions{ExcludePaths: []string{"vendor/*"}}
	if !o.shouldSkip("vendor/lib.go") {
		t.Errorf("ExcludePaths should match")
	}
	if o.shouldSkip("server/main_test.go") {
		t.Errorf("fixture path must NOT skip when SkipDefaultFixturePaths is false")
	}

	// SkipDefaultFixturePaths only.
	o = scanOptions{SkipDefaultFixturePaths: true}
	if !o.shouldSkip("server/main_test.go") {
		t.Errorf("fixture path must skip when SkipDefaultFixturePaths is true")
	}
	if o.shouldSkip("server/main.go") {
		t.Errorf("non-fixture path must not skip")
	}

	// Both: union semantics.
	o = scanOptions{ExcludePaths: []string{"vendor/*"}, SkipDefaultFixturePaths: true}
	if !o.shouldSkip("vendor/lib.go") || !o.shouldSkip("foo_test.go") {
		t.Errorf("union semantics broken: %+v", o)
	}
}
