package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestTestsPresent_Empty(t *testing.T) {
	fs, err := checkTestsPresent(context.Background(), t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 {
		t.Fatalf("got %d findings, want 1", len(fs))
	}
	if fs[0].Severity != SeverityInfo {
		t.Errorf("severity = %q, want info (no project marker present)", fs[0].Severity)
	}
}

func TestTestsPresent_WithMarkerNoTests(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module x\n")
	mustWrite(t, filepath.Join(root, "main.go"), "package main\nfunc main(){}\n")
	fs, err := checkTestsPresent(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 || fs[0].Severity != SeverityWarning {
		t.Fatalf("expected one warning finding, got: %+v", fs)
	}
}

// fileVariants confirms we recognise every test-file naming convention we
// claim to support. Each variant alone must satisfy the gate.
func TestTestsPresent_FileVariants(t *testing.T) {
	cases := []struct {
		name     string
		filename string
	}{
		{"go", "thing_test.go"},
		{"py_prefix", "test_things.py"},
		{"py_suffix", "things_test.py"},
		{"ts_test", "thing.test.ts"},
		{"ts_spec", "thing.spec.ts"},
		{"js_test", "thing.test.js"},
		{"jsx_spec", "thing.spec.jsx"},
		{"rs", "things_test.rs"},
		{"java", "ThingTest.java"},
		{"kotlin", "ThingTests.kt"},
		{"ruby_spec", "thing_spec.rb"},
		{"ruby_test", "thing_test.rb"},
		{"conftest", "conftest.py"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			mustWrite(t, filepath.Join(root, c.filename), "x")
			fs, err := checkTestsPresent(context.Background(), root, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(fs) != 0 {
				t.Errorf("variant %q should satisfy the gate, got: %+v", c.filename, fs)
			}
		})
	}
}

// directoryVariants: a non-empty test-named directory satisfies the gate
// even if individual files inside don't match the naming heuristics.
func TestTestsPresent_DirectoryVariants(t *testing.T) {
	for _, dir := range []string{"tests", "__tests__", "spec"} {
		t.Run(dir, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
				t.Fatal(err)
			}
			mustWrite(t, filepath.Join(root, dir, "anything.txt"), "x")
			fs, err := checkTestsPresent(context.Background(), root, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(fs) != 0 {
				t.Errorf("dir %q should satisfy the gate, got: %+v", dir, fs)
			}
		})
	}
}

// Empty `tests/` directories don't count — placeholders shouldn't game the
// gate.
func TestTestsPresent_EmptyTestDir(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "tests"), 0o755); err != nil {
		t.Fatal(err)
	}
	fs, err := checkTestsPresent(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 {
		t.Errorf("empty tests/ should NOT satisfy the gate, got: %+v", fs)
	}
}

// E2E / integration test directories must satisfy the gate.
func TestTestsPresent_E2EDirs(t *testing.T) {
	for _, dir := range []string{"cypress", "playwright", "e2e", "integration", "features"} {
		t.Run(dir, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
				t.Fatal(err)
			}
			mustWrite(t, filepath.Join(root, dir, "foo.spec.ts"), "test content")
			fs, err := checkTestsPresent(context.Background(), root, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(fs) != 0 {
				t.Errorf("%s/ dir must satisfy gate, got: %+v", dir, fs)
			}
		})
	}
}

// package.json with a well-known test runner must satisfy the gate.
func TestTestsPresent_PackageJSONTestRunner(t *testing.T) {
	for _, runner := range []string{"jest", "vitest", "cypress", "@playwright/test"} {
		t.Run(runner, func(t *testing.T) {
			root := t.TempDir()
			mustWrite(t, filepath.Join(root, "package.json"),
				`{"devDependencies":{"`+runner+`":"^1.0.0"}}`)
			fs, err := checkTestsPresent(context.Background(), root, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(fs) != 0 {
				t.Errorf("package.json with %q must satisfy gate, got: %+v", runner, fs)
			}
		})
	}
}

// Skip directories listed in testScanSkipDirs even if they contain
// test-named files (vendored deps, node_modules, …).
func TestTestsPresent_SkipsVendoredDirs(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "foo"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(root, "node_modules", "foo", "thing.test.ts"), "x")
	fs, err := checkTestsPresent(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 {
		t.Errorf("vendored test should NOT count, got: %+v", fs)
	}
}
