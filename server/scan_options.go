package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// scanOptions is the shared shape every git-tracked-file scanner consumes
// from its gate_options sub-tree. Extending the struct here keeps
// future per-gate knobs in one place.
type scanOptions struct {
	// ExcludePaths is a list of glob patterns (filepath.Match semantics)
	// matched against the relative-from-root file path. A file matching
	// any pattern is skipped before its content is read.
	ExcludePaths []string `json:"exclude_paths,omitempty"`

	// SkipDefaultFixturePaths controls whether the gate skips files in
	// well-known test/fixture locations: *_test.go / test_*.py /
	// *_test.py / *.test.{ts,tsx,js,jsx} / *.spec.{ts,tsx,js,jsx} /
	// *_test.rs / *Test.{java,kt} / *_spec.rb / *_test.rb / conftest.py,
	// plus any path traversing test/, tests/, __tests__/, spec/,
	// testdata/, fixtures/, __fixtures__/.
	//
	// Default true — test fixtures legitimately contain mock secrets,
	// fake IPs, and placeholder URLs. Set to false explicitly in
	// .l0git.json gate_options to scan fixture files as well.
	SkipDefaultFixturePaths *bool `json:"skip_default_fixture_paths,omitempty"`

	// SkipDefaultDataFiles controls whether content-scan gates skip
	// tabular / line-oriented data files where the addresses, URLs, and
	// keys ARE the payload of the file rather than embedded literals in
	// source code. Currently: .csv, .tsv, .jsonl, .ndjson, .parquet,
	// .arrow, .feather. Honoured only by content-scan gates via
	// shouldSkipContent; metadata-only gates (large_file_tracked,
	// vendored_dir_tracked, …) still see these files.
	//
	// Default true — scanning a 100k-row blocklist CSV for "public IPs"
	// is millions of findings against the file's reason to exist. Set
	// to false in .l0git.json gate_options if you're treating data
	// files as code (rare).
	SkipDefaultDataFiles *bool `json:"skip_default_data_files,omitempty"`
}

func parseScanOptions(opts json.RawMessage) scanOptions {
	var s scanOptions
	if len(opts) > 0 {
		_ = json.Unmarshal(opts, &s) // best-effort; bad shape is treated as no-op
	}
	// Default skip_default_fixture_paths to true when not explicitly set.
	if s.SkipDefaultFixturePaths == nil {
		t := true
		s.SkipDefaultFixturePaths = &t
	}
	if s.SkipDefaultDataFiles == nil {
		t := true
		s.SkipDefaultDataFiles = &t
	}
	return s
}

// shouldSkip combines pathExcluded with the optional default-fixture
// skip. Used by every gate. Note: this does NOT honour
// SkipDefaultDataFiles — metadata-only gates (vendored_dir_tracked,
// large_file_tracked, …) must still see data files. Content-scan gates
// should call shouldSkipContent instead.
func (s scanOptions) shouldSkip(rel string) bool {
	if pathExcluded(rel, s.ExcludePaths) {
		return true
	}
	if s.SkipDefaultFixturePaths != nil && *s.SkipDefaultFixturePaths && isDefaultFixturePath(rel) {
		return true
	}
	return false
}

// shouldSkipContent is shouldSkip plus the default-data-file skip. Used
// by gates that read file contents and would otherwise drown in
// findings on tabular data files where the payload IS addresses/URLs
// (blocklists, fingerprint datasets, exported logs, …).
func (s scanOptions) shouldSkipContent(rel string) bool {
	if s.shouldSkip(rel) {
		return true
	}
	if s.SkipDefaultDataFiles != nil && *s.SkipDefaultDataFiles && isDefaultDataFile(rel) {
		return true
	}
	return false
}

// pathExcluded returns true when rel matches any of the patterns. Match
// errors (bad glob) are ignored — patterns silently miss rather than
// fail the entire run.
func pathExcluded(rel string, patterns []string) bool {
	for _, p := range patterns {
		if ok, _ := filepath.Match(p, rel); ok {
			return true
		}
	}
	return false
}

// fixtureDirNames are directory names that, when present anywhere in a
// file's path, mark the file as a test/fixture target. Lower-case
// matched (case-insensitive on macOS / Windows is a non-issue because
// git stores paths verbatim).
var fixtureDirNames = map[string]bool{
	"test":         true,
	"tests":        true,
	"__tests__":    true,
	"__test__":     true,
	"spec":         true,
	"testdata":     true,
	"fixtures":     true,
	"__fixtures__": true,
}

// dataFileExtensions are tabular / line-oriented data file extensions
// whose contents are the payload (IPs, URLs, hashes, fingerprints, …)
// rather than embedded literals in source code. Content-scan gates
// skip these by default to avoid drowning users in findings.
var dataFileExtensions = map[string]bool{
	".csv":     true,
	".tsv":     true,
	".jsonl":   true,
	".ndjson":  true,
	".parquet": true,
	".arrow":   true,
	".feather": true,
}

// isDefaultDataFile returns true when rel has a data-file extension
// from dataFileExtensions. Case-insensitive on the extension.
func isDefaultDataFile(rel string) bool {
	return dataFileExtensions[strings.ToLower(filepath.Ext(rel))]
}

// isDefaultFixturePath returns true when the given relative path looks
// like test/fixture material under the conventions tests_present uses
// for detection. Used by content-scan gates with
// SkipDefaultFixturePaths enabled.
func isDefaultFixturePath(rel string) bool {
	base := filepath.Base(rel)
	if looksLikeTestFile(base) || base == "conftest.py" {
		return true
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	// Walk every directory component (exclude the basename).
	for i := 0; i < len(parts)-1; i++ {
		if fixtureDirNames[strings.ToLower(parts[i])] {
			return true
		}
	}
	return false
}
