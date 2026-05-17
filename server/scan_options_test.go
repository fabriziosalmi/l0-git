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

func TestIsDefaultDataFile(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"data/blocklist.csv", true},
		{"data/Blocklist.CSV", true}, // case-insensitive ext
		{"fingerprints.jsonl", true},
		{"events.ndjson", true},
		{"export.tsv", true},
		{"snapshot.parquet", true},
		{"table.arrow", true},
		{"vec.feather", true},
		// must NOT skip
		{"README.md", false},
		{"server/main.go", false},
		{"data.json", false},   // .json is structured but not line-delimited; gates handle it
		{"config.yaml", false}, // structured config — must still scan
		{"weights.bin", false}, // binary, but content scanners catch it via isBinary
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			if got := isDefaultDataFile(c.path); got != c.want {
				t.Errorf("%q: got %v, want %v", c.path, got, c.want)
			}
		})
	}
}

func TestIsDefaultBackupPath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		// directory components
		{"bak/build.func", true},
		{"src/bak/old.go", true},
		{"backup/snapshot.tar", true},
		{"backups/2025-11-29/db.sql", true},
		{"archive/v1/index.html", true},
		{"archived/legacy/main.py", true},
		{"src/Bak/old.go", true}, // case-insensitive
		// extensions
		{"main.go.bak", true},
		{"config.yaml.backup", true},
		{"build.func.old", true},
		{"server.conf.orig", true},
		// timestamped suffixes
		{"bak/core_bak/build.func - advanced-backup-20251127-154005.func", true},
		{"build.func.backup-20251029-123804", true},
		{"foo.backup-20260514", true},
		// underscore-separated timestamp in a directory name
		{"security_fixes_backup_20250626_003832/main.py", true},
		{"security_fixes_backup_20250626/main.py", true},
		// must NOT match
		{"src/main.go", false},
		{"docs/backups.md", false},                       // basename mention, not a backup file
		{"archive_test.go", false},                       // "archive" not a directory component
		{"backupable_module.py", false},                  // contains "backup" as substring only
		{"old/file.go", false},                           // "old" alone isn't a backup-dir marker
		{"checks/aws/efs/check_backup_enabled.py", false}, // "backup" is domain word, no timestamp
		{"src/check_backup_policy.py", false},            // same — code checking backup policies
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			if got := isDefaultBackupPath(c.path); got != c.want {
				t.Errorf("%q: got %v, want %v", c.path, got, c.want)
			}
		})
	}
}

func TestScanOptions_ShouldSkipContent(t *testing.T) {
	// Default (nil opts): data files are skipped by content scanners,
	// but the metadata-only shouldSkip path still sees them.
	parsed := parseScanOptions(nil)
	if !parsed.shouldSkipContent("data/blocklist.csv") {
		t.Errorf("default opts must skip .csv in content scan")
	}
	if parsed.shouldSkip("data/blocklist.csv") {
		t.Errorf("shouldSkip (metadata path) must NOT skip .csv — large_file/vendored need to see it")
	}

	// Explicit false: data files become scannable.
	parsedOff := parseScanOptions(json.RawMessage(`{"skip_default_data_files":false}`))
	if parsedOff.shouldSkipContent("data/blocklist.csv") {
		t.Errorf("explicit false must not skip .csv")
	}

	// ExcludePaths still works on top of data-file skip.
	o := scanOptions{ExcludePaths: []string{"docs/*"}, SkipDefaultDataFiles: boolPtr(true), SkipDefaultFixturePaths: boolPtr(true), SkipDefaultBackupPaths: boolPtr(true)}
	if !o.shouldSkipContent("docs/README.md") || !o.shouldSkipContent("data/x.csv") {
		t.Errorf("shouldSkipContent must respect ExcludePaths AND data-file skip")
	}

	// Default: backup paths are skipped by content scanners.
	if !parsed.shouldSkipContent("bak/build.func") || !parsed.shouldSkipContent("main.go.bak") {
		t.Errorf("default opts must skip backup paths in content scan")
	}
	// But shouldSkip (metadata path) must still see backups —
	// vendored_dir_tracked / large_file_tracked may want to flag them.
	if parsed.shouldSkip("bak/build.func") {
		t.Errorf("shouldSkip (metadata path) must NOT skip backup paths")
	}

	// Explicit false disables backup skip.
	parsedNoBak := parseScanOptions(json.RawMessage(`{"skip_default_backup_paths":false}`))
	if parsedNoBak.shouldSkipContent("bak/foo.go") {
		t.Errorf("explicit false must not skip backup paths in content scan")
	}
}
