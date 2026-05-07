package main

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Directory names we never descend into when looking for test files. Anything
// else under the project root is fair game.
var testScanSkipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	"dist":         true,
	"build":        true,
	"target":       true, // Rust / Java
	".venv":        true,
	"venv":         true,
	"__pycache__":  true,
	".idea":        true,
	".vscode":      true,
	".tox":         true,
	".gradle":      true,
	".next":        true,
	".cache":       true,
}

// projectMarkerFiles are root files that signal "this is a real source
// project" — used to decide whether the absence of tests is a warning
// (active project) or just info (sandbox/scratch repo).
var projectMarkerFiles = []string{
	"go.mod", "package.json", "pyproject.toml", "setup.py", "setup.cfg",
	"Cargo.toml", "pom.xml", "build.gradle", "build.gradle.kts",
	"Gemfile", "composer.json", "mix.exs",
}

func hasProjectMarker(root string) bool {
	for _, m := range projectMarkerFiles {
		if _, err := os.Stat(filepath.Join(root, m)); err == nil {
			return true
		}
	}
	return false
}

// looksLikeTestFile recognises common test-file naming conventions across
// the languages we care about. Folder-based hits (tests/, __tests__/) are
// handled in the walker so we don't pay for a directory probe per file.
func looksLikeTestFile(name string) bool {
	low := strings.ToLower(name)
	switch {
	case strings.HasSuffix(low, "_test.go"):
		return true
	case strings.HasSuffix(low, "_test.py") || strings.HasPrefix(low, "test_") && strings.HasSuffix(low, ".py"):
		return true
	case strings.HasSuffix(low, ".test.ts") || strings.HasSuffix(low, ".test.tsx") ||
		strings.HasSuffix(low, ".test.js") || strings.HasSuffix(low, ".test.jsx"):
		return true
	case strings.HasSuffix(low, ".spec.ts") || strings.HasSuffix(low, ".spec.tsx") ||
		strings.HasSuffix(low, ".spec.js") || strings.HasSuffix(low, ".spec.jsx"):
		return true
	case strings.HasSuffix(low, "_test.rs"):
		return true
	case strings.HasSuffix(low, "test.java") || strings.HasSuffix(low, "tests.java"):
		return true
	case strings.HasSuffix(low, "test.kt") || strings.HasSuffix(low, "tests.kt"):
		return true
	case strings.HasSuffix(low, "_spec.rb") || strings.HasSuffix(low, "_test.rb"):
		return true
	}
	return false
}

// testDirNames are folder names whose mere presence (with at least one file
// inside) counts as having tests. Conftest.py also satisfies the gate.
var testDirNames = map[string]bool{
	"tests":      true,
	"__tests__":  true,
	"test":       true,
	"spec":       true,
	"__test__":   true,
	"test_suite": true,
}

func checkTestsPresent(_ context.Context, root string) ([]Finding, error) {
	hasMarker := hasProjectMarker(root)
	found, err := walkForTests(root)
	if err != nil {
		return nil, err
	}
	if found {
		return nil, nil
	}
	severity := SeverityInfo
	if hasMarker {
		severity = SeverityWarning
	}
	return []Finding{{
		Severity: severity,
		Title:    "No tests found",
		Message:  "No test files detected anywhere under the project (looked for *_test.go, test_*.py / *_test.py, *.test.{ts,js}, *.spec.{ts,js}, *_test.rs, *Test.{java,kt}, *_spec.rb, conftest.py, and tests/ directories). Add at least one test to keep regressions out.",
	}}, nil
}

func walkForTests(root string) (bool, error) {
	found := false
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Permission errors deeper in the tree shouldn't kill the gate;
			// just skip the offending entry.
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if found {
			return filepath.SkipAll
		}
		name := d.Name()
		if d.IsDir() {
			if path == root {
				return nil
			}
			if testScanSkipDirs[name] || strings.HasPrefix(name, ".") && name != "." {
				// Skip dotfile-style dirs (.git, .vscode, …) wholesale.
				return fs.SkipDir
			}
			if testDirNames[strings.ToLower(name)] {
				// A test-named directory only counts if it actually
				// contains a file (empty placeholders don't).
				entries, _ := os.ReadDir(path)
				for _, e := range entries {
					if !e.IsDir() {
						found = true
						return filepath.SkipAll
					}
				}
			}
			return nil
		}
		if name == "conftest.py" {
			found = true
			return filepath.SkipAll
		}
		if looksLikeTestFile(name) {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	if walkErr != nil && walkErr != filepath.SkipAll {
		return false, walkErr
	}
	return found, nil
}
