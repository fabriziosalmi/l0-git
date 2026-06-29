package main

import (
	"encoding/json"
	"path/filepath"
	"regexp"
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
	// Some content-scan gates additionally detect address lists by content
	// (network_scan: a .txt/other file whose lines are overwhelmingly bare
	// IP/CIDR literals) and gate that on this same flag.
	//
	// Default true — scanning a 100k-row blocklist CSV for "public IPs"
	// is millions of findings against the file's reason to exist. Set
	// to false in .l0git.json gate_options if you're treating data
	// files as code (rare).
	SkipDefaultDataFiles *bool `json:"skip_default_data_files,omitempty"`

	// SkipDefaultBackupPaths controls whether content-scan gates skip
	// files that look like local backups (bak/, backup/, backups/,
	// archive/, archived/ directories, or .bak/.backup/.old/.orig
	// extensions, or basenames ending in `-backup-YYYYMMDD-HHMMSS`).
	// These are tagged-and-shelved snapshots of past code — every
	// TODO, http://, or private key header inside them is a stale echo
	// of something that exists in the live tree.
	//
	// Default true. Metadata gates (vendored_dir_tracked,
	// large_file_tracked) still see them and may correctly flag the
	// backup files as something that shouldn't be in git at all.
	SkipDefaultBackupPaths *bool `json:"skip_default_backup_paths,omitempty"`

	// SkipDefaultGeneratedFiles controls whether content-scan gates skip
	// machine-generated artefacts where any pattern match is an artefact of
	// a build (or a value already present in the scanned source): source
	// maps (.map), dependency lockfiles (package-lock.json, go.sum,
	// Cargo.lock, …), and generated code (.pb.go). Minified bundles
	// (.min.js) are deliberately NOT included — build-time-injected
	// frontend secrets live there and nowhere else.
	//
	// Default true. Set to false to scan generated files too.
	SkipDefaultGeneratedFiles *bool `json:"skip_default_generated_files,omitempty"`

	// SkipDefaultDataDirs controls whether content-scan gates skip files
	// that live under a recognised *dataset directory* (data/, datasets/,
	// corpus/, samples/, payloads/, wordlists/, …) AND carry an ambiguous
	// data extension (.json/.txt/.xml/.cm/.list/.lst/.dat). Inside such a
	// directory those extensions are the dataset payload — a JSON corpus of
	// attack strings, a .txt of blocklist entries, an nl2bash .cm dump —
	// not authored source, so every IP / URL / token inside is a
	// self-evident FP. A code file in the same tree (.go/.py/.ts/…) is NOT
	// skipped: the extension allowlist is deliberately data-only so a real
	// source file under data/ is never silenced.
	//
	// Default true for the noisy content gates. secrets_scan and
	// secrets_scan_history deliberately do NOT honour it (they call
	// shouldSkipContentExceptDataDirs): a real credential committed into a
	// dataset file is still a leak that must be reported. Set to false to
	// scan dataset directories with the other gates too.
	SkipDefaultDataDirs *bool `json:"skip_default_data_dirs,omitempty"`
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
	if s.SkipDefaultBackupPaths == nil {
		t := true
		s.SkipDefaultBackupPaths = &t
	}
	if s.SkipDefaultGeneratedFiles == nil {
		t := true
		s.SkipDefaultGeneratedFiles = &t
	}
	if s.SkipDefaultDataDirs == nil {
		t := true
		s.SkipDefaultDataDirs = &t
	}
	return s
}

// skipEnabled treats nil as "use the default" (true). Each
// Skip-Default-… flag's design is "off only when explicitly set to
// false". This makes the helpers robust against custom option
// parsers that decode into the embedded scanOptions without going
// through parseScanOptions (markdown, html, dead_placeholders, …).
func skipEnabled(p *bool) bool {
	return p == nil || *p
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
	if skipEnabled(s.SkipDefaultFixturePaths) && isDefaultFixturePath(rel) {
		return true
	}
	return false
}

// shouldSkipContent is shouldSkip plus the default-data-file and
// default-backup-path skips. Used by gates that read file contents
// and would otherwise drown in findings on tabular data files
// (blocklists, fingerprint datasets) or local snapshot folders
// (bak/, backup/, archive/ — stale echoes of the live tree).
func (s scanOptions) shouldSkipContent(rel string) bool {
	if s.shouldSkipContentExceptDataDirs(rel) {
		return true
	}
	if skipEnabled(s.SkipDefaultDataDirs) && isDefaultDataDirFile(rel) {
		return true
	}
	return false
}

// shouldSkipContentExceptDataDirs is shouldSkipContent without the
// dataset-directory skip. secrets_scan and secrets_scan_history use it: a
// credential committed into a dataset file (data/seed.json, corpus/dump.txt)
// is still a real leak, so those gates must keep reading dataset directories
// even though the noisy network/URL gates skip them.
func (s scanOptions) shouldSkipContentExceptDataDirs(rel string) bool {
	if s.shouldSkip(rel) {
		return true
	}
	if skipEnabled(s.SkipDefaultDataFiles) && isDefaultDataFile(rel) {
		return true
	}
	if skipEnabled(s.SkipDefaultBackupPaths) && isDefaultBackupPath(rel) {
		return true
	}
	if skipEnabled(s.SkipDefaultGeneratedFiles) && isDefaultGeneratedFile(rel) {
		return true
	}
	return false
}

// generatedFileBasenames are dependency lockfiles — generated and updated by a
// package manager, never hand-edited. Any address / hash / URL inside is the
// tool's bookkeeping, not an authored literal.
var generatedFileBasenames = map[string]bool{
	"package-lock.json":   true,
	"npm-shrinkwrap.json": true,
	"yarn.lock":           true,
	"pnpm-lock.yaml":      true,
	"composer.lock":       true,
	"gemfile.lock":        true,
	"poetry.lock":         true,
	"pipfile.lock":        true,
	"cargo.lock":          true,
	"go.sum":              true,
	"flake.lock":          true,
}

// isDefaultGeneratedFile reports whether rel is a machine-generated artefact
// content-scan gates should skip: a source map (.map), a dependency lockfile, or
// generated Go protobuf (.pb.go). Minified bundles are intentionally excluded.
func isDefaultGeneratedFile(rel string) bool {
	base := strings.ToLower(filepath.Base(rel))
	if generatedFileBasenames[base] {
		return true
	}
	if strings.HasSuffix(base, ".map") || strings.HasSuffix(base, ".pb.go") {
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

// dataDirNames are directory names that, when present anywhere in a file's
// path, mark the file as living inside a dataset tree. Curated to names that
// are rarely a source-code package: a repo's `data/` or `corpus/` holds
// payloads, not authored code.
var dataDirNames = map[string]bool{
	"data":      true,
	"datasets":  true,
	"dataset":   true,
	"corpus":    true,
	"corpora":   true,
	"samples":   true,
	"payloads":  true,
	"wordlists": true,
}

// dataDirExtensions are extensions that are ambiguous globally (a .json can
// be config, a .txt can be docs) but are unmistakably dataset payload when
// they live under a dataDirNames directory. Deliberately data-only — source
// extensions (.go/.py/.ts/…) are absent so a real source file under data/ is
// never silenced.
var dataDirExtensions = map[string]bool{
	".json": true,
	".txt":  true,
	".xml":  true,
	".cm":   true, // command corpora (nl2bash bash side)
	".nl":   true, // natural-language corpora (nl2bash NL side)
	".list": true,
	".lst":  true,
	".dat":  true,
}

// isDefaultDataDirFile reports whether rel is an ambiguous-extension data file
// nested under a recognised dataset directory. Both conditions are required:
// the extension gate keeps it from skipping source code, the directory gate
// keeps it from skipping a top-level config.json / notes.txt.
func isDefaultDataDirFile(rel string) bool {
	if !dataDirExtensions[strings.ToLower(filepath.Ext(rel))] {
		return false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for i := 0; i < len(parts)-1; i++ {
		if dataDirNames[strings.ToLower(parts[i])] {
			return true
		}
	}
	return false
}

// listMinLines / listRatio gate the "this file is a list of atoms" heuristic
// shared by network_scan (IP/CIDR lists) and connection_strings (URL lists):
// a file must have at least listMinLines content lines (blank and full-line
// comment lines do not count) and at least listRatio of those must be a bare
// item before the whole file is treated as a data payload. The floor keeps a
// short config (a few pinned hosts/URLs) from being mistaken for a dump.
const (
	listMinLines = 10
	listRatio    = 0.8
)

// looksLikeListFile reports whether data is a line-oriented list whose lines
// are overwhelmingly a single bare item (as judged by isItem) rather than
// source that happens to mention one. Blank lines and full-line comments
// (`#`, `;`, `//`) are excluded from the denominator so a licence header or
// section comments above a dump don't dilute the ratio.
func looksLikeListFile(data []byte, isItem func(string) bool) bool {
	considered, hits := 0, 0
	for _, raw := range strings.Split(string(data), "\n") {
		s := strings.TrimSpace(raw)
		if s == "" || strings.HasPrefix(s, "#") || strings.HasPrefix(s, ";") || strings.HasPrefix(s, "//") {
			continue
		}
		considered++
		if isItem(s) {
			hits++
		}
	}
	if considered < listMinLines {
		return false
	}
	return float64(hits) >= listRatio*float64(considered)
}

// backupDirNames are directory names that, when present anywhere in a
// file's path, mark the file as a local backup snapshot. Lower-cased
// match.
var backupDirNames = map[string]bool{
	"bak":      true,
	"backup":   true,
	"backups":  true,
	"archive":  true,
	"archived": true,
}

// backupExtensions are file extensions that mark snapshot/copy files.
var backupExtensions = map[string]bool{
	".bak":    true,
	".backup": true,
	".old":    true,
	".orig":   true,
}

// backupTimestampedRe matches names containing a backup timestamp
// suffix like `foo.func.backup-20251029-123804`,
// `build.func - advanced-backup-20251127-154005.func`, or
// `security_fixes_backup_20250626_003832`. Conservative: requires the
// literal `backup` token, then a separator (`-` or `_`), then
// YYYYMMDD, then optionally another separator + HHMMSS. The leading
// `[-_ .]` anchor avoids matching `check_backup_*.py` (where
// "backup" is a domain word, not a backup marker).
var backupTimestampedRe = regexp.MustCompile(`[-_ .]backup[-_]\d{8}([-_]\d{6})?`)

// isDefaultBackupPath returns true for files that look like local
// backups/snapshots — a directory component matches backupDirNames,
// any directory component embeds a `backup-YYYYMMDD` timestamp, the
// extension is one of backupExtensions, or the basename embeds a
// `backup-YYYYMMDD` timestamp.
func isDefaultBackupPath(rel string) bool {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for i := 0; i < len(parts)-1; i++ {
		p := strings.ToLower(parts[i])
		if backupDirNames[p] {
			return true
		}
		// e.g. `security_fixes_backup_20250626_003832/...`
		if backupTimestampedRe.MatchString(p) {
			return true
		}
	}
	base := strings.ToLower(filepath.Base(rel))
	if backupExtensions[strings.ToLower(filepath.Ext(base))] {
		return true
	}
	if backupTimestampedRe.MatchString(base) {
		return true
	}
	return false
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
