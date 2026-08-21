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

	// SkipDefaultDataDirs controls whether content-scan gates skip data
	// payload files: those under a recognised *dataset directory* (data/,
	// datasets/, corpus/, samples/, payloads/, wordlists/, …) carrying an
	// ambiguous data extension (.json/.txt/.xml/.cm/.nl/.dat), PLUS list/log
	// files anywhere in the tree (.log/.list/.lst and log dumps such as
	// log.json). Inside a dataset dir those extensions are the payload — a
	// JSON corpus of attack strings, a .txt blocklist, an nl2bash .cm dump —
	// and a .log/.list is always payload, so every IP / URL / token inside is
	// a self-evident FP. A code file in a dataset tree (.go/.py/.ts/…) is NOT
	// skipped: the extension allowlist is deliberately data-only so a real
	// source file under data/ is never silenced.
	//
	// Default true for the noisy content gates. secrets_scan and
	// secrets_scan_history deliberately do NOT honour it (they call
	// shouldSkipContentExceptDataDirs): a real credential committed into a
	// dataset file is still a leak that must be reported. Set to false to
	// scan dataset directories with the other gates too.
	SkipDefaultDataDirs *bool `json:"skip_default_data_dirs,omitempty"`

	// SkipDefaultDependencyPaths controls whether content-scan gates skip
	// third-party dependency trees installed by a package manager
	// (node_modules/, vendor/, site-packages/, .venv/, Pods/, …). Nothing
	// under those paths was authored here, so every TODO, http:// URL, mock
	// credential, or IP literal inside is upstream's, not the user's — and
	// the one actionable statement about the tree ("this shouldn't be
	// committed") is already made once by vendored_dir_tracked.
	//
	// Default true. Deliberately NOT honoured by secrets_scan or
	// secrets_scan_history: a credential committed under vendor/ is still a
	// committed credential, and vendored_dir_tracked does not always emit a
	// compensating finding (a legitimate Go vendor/ is exempt from it).
	// Set to false to let the other gates read dependency code too.
	SkipDefaultDependencyPaths *bool `json:"skip_default_dependency_paths,omitempty"`

	// SkipDefaultGeneratedDirs controls whether content-scan gates skip
	// unambiguous tool-output directories (.next/, .vitepress/, _site/,
	// __pycache__/, htmlcov/, …). Findings there describe a build artefact
	// that is regenerated on the next run, so fixing them is impossible —
	// the fix belongs in the source the tool consumed.
	//
	// Deliberately does NOT cover dist/, build/, out/ or target/: those
	// names are hand-authored content directories often enough that skipping
	// them by name would silence real source.
	//
	// Default true.
	SkipDefaultGeneratedDirs *bool `json:"skip_default_generated_dirs,omitempty"`
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
	if s.SkipDefaultDependencyPaths == nil {
		t := true
		s.SkipDefaultDependencyPaths = &t
	}
	if s.SkipDefaultGeneratedDirs == nil {
		t := true
		s.SkipDefaultGeneratedDirs = &t
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
	if skipEnabled(s.SkipDefaultDataDirs) && isNoisyDataFile(rel) {
		return true
	}
	if skipEnabled(s.SkipDefaultGeneratedFiles) && isMinifiedBundle(rel) {
		return true
	}
	// The next three deliberately live HERE and not in the shared base, so
	// secrets_scan and secrets_scan_history keep reading these paths.
	//
	// The first draft put them in the base helper on the reasoning that
	// "vendored_dir_tracked already says the one actionable thing about the
	// tree". That reasoning is wrong exactly where it matters: a legitimate
	// Go `vendor/` (go.mod + vendor/modules.txt) is EXEMPT from that gate, so
	// a credential committed there would have produced no finding at all —
	// silently breaking the secrets gate's contract over tracked files.
	// The noise these remove is address/URL/TODO noise, and only those gates
	// need protecting from it.
	if skipEnabled(s.SkipDefaultDependencyPaths) && isDependencyPath(rel) {
		return true
	}
	if skipEnabled(s.SkipDefaultGeneratedDirs) && isGeneratedDirPath(rel) {
		return true
	}
	// Binary payloads are never source. isBinary (NUL byte in the first
	// 8 KiB) misses formats with an ASCII header — PDFs above all — so the
	// extension check runs first.
	if isBinaryPath(rel) {
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
	// Sample/mock trees: an `examples/feed.xml` or `__snapshots__/page.json`
	// is illustrative payload. Only the data extensions below are skipped,
	// so an `examples/main.go` is still scanned as the source it is.
	"examples":      true,
	"example":       true,
	"mocks":         true,
	"mock":          true,
	"stubs":         true,
	"snapshots":     true,
	"__snapshots__": true,
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
	".dat":  true,
}

// listLogExtensions are list/log payload files whose contents are the payload
// anywhere in the tree (not just under a dataset dir): a `.log` access log, a
// `.list`/`.lst` block/allow list. Never source. Skipped by the noisy content
// gates but — like the dataset-dir skip — NOT by secrets, since logs and lists
// are a real credential-leak vector.
var listLogExtensions = map[string]bool{
	".log":  true,
	".list": true,
	".lst":  true,
}

// isLogBasename catches log dumps that carry a generic extension: a JSON-lines
// access log named `log.json`, a `service.log.json`, etc. Their lines are log
// records full of IPs/URLs, not authored literals.
func isLogBasename(rel string) bool {
	base := strings.ToLower(filepath.Base(rel))
	if base == "log.json" || base == "logs.json" {
		return true
	}
	return strings.HasSuffix(base, ".log.json") || strings.HasSuffix(base, ".log.txt")
}

// isNoisyDataFile reports whether rel is dataset/list/log payload that the
// noisy content gates (network_scan, connection_strings, …) should skip but
// secrets_scan must still read. It unions the dataset-directory rule with the
// always-data list/log extensions and log basenames.
func isNoisyDataFile(rel string) bool {
	if listLogExtensions[strings.ToLower(filepath.Ext(rel))] {
		return true
	}
	if isLogBasename(rel) {
		return true
	}
	return isDefaultDataDirFile(rel)
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
		if isCamelCaseTestDir(parts[i]) {
			return true
		}
	}
	return false
}

// isCamelCaseTestDir recognises the Xcode / .NET / Java convention of naming
// a test target `<Product>Tests` (`proxymateTests/`, `AppKitTests/`,
// `Acme.Web.Tests/`). Matching requires the capital `T` of the convention, so
// an ordinary lower-case word that merely ends in "tests" (`contests/`) is
// left alone.
func isCamelCaseTestDir(name string) bool {
	// Deliberately no "Spec"/"Specs": Ruby's lower-case `spec/` is already an
	// exact match above, and a CamelCase `OpenApiSpec/` is an API definition,
	// not a test target — silencing it could hide a real credential.
	for _, suffix := range []string{"Tests", "Test"} {
		if len(name) > len(suffix) && strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// =============================================================================
// dependency trees, generated output, binary payloads
// =============================================================================

// dependencyDirNames are directory names that, anywhere in a path, mark the
// file as third-party dependency code installed by a package manager. Nothing
// under them was authored in this repository: a TODO, an http:// URL, a mock
// AWS key, or an IP literal inside `node_modules/` belongs to upstream, and
// the only actionable finding about the tree as a whole is the one
// vendored_dir_tracked already emits ("don't commit node_modules").
//
// Curated to names that cannot plausibly be hand-authored source: `lib/`,
// `packages/`, `external/` and friends are deliberately absent because
// first-party code lives under those names all the time.
var dependencyDirNames = map[string]bool{
	"node_modules":     true,
	"bower_components": true,
	"jspm_packages":    true,
	"site-packages":    true,
	"dist-packages":    true,
	".venv":            true,
	"venv":             true,
	"virtualenv":       true,
	".tox":             true,
	".pnpm-store":      true,
	".gradle":          true,
	".terraform":       true,
	".pub-cache":       true,
	// NOTE: CocoaPods' "Pods" is deliberately absent from this map and
	// matched case-sensitively below — a lower-case `pods/` is a normal
	// Kubernetes manifest directory, and skipping it would hide real
	// findings in hand-written YAML.
	"third_party": true,
	"thirdparty":  true,
	"vendor":      true, // Go -mod=vendor, PHP Composer, vendored web assets
}

// dependencySubtrees covers tool directories that mix a first-party config
// file with a third-party cache. `.cargo/config.toml` and `.bundle/config`
// are hand-written — the first commonly holds registry and mirror URLs and can
// hold credentials — so only the named cache subtrees below count as
// dependency code. Anything else under these directories is still scanned.
var dependencySubtrees = map[string]map[string]bool{
	".cargo":  {"registry": true, "git": true, "bin": true},
	".bundle": {}, // only ever holds a hand-written `config`
	".yarn":   {"cache": true, "unplugged": true, "releases": true, "plugins": true, "sdks": true, "berry": true},
}

// isDependencyPath reports whether rel traverses a third-party dependency
// directory. The basename is excluded from the walk so a file *named*
// `vendor` (rare, but legal) is not mistaken for a directory.
func isDependencyPath(rel string) bool {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for i := 0; i < len(parts)-1; i++ {
		lower := strings.ToLower(parts[i])
		if dependencyDirNames[lower] {
			return true
		}
		// CocoaPods always capitalises; `pods/` in lower case is a
		// Kubernetes manifest directory.
		if parts[i] == "Pods" {
			return true
		}
		if subtrees, ok := dependencySubtrees[lower]; ok {
			if i+1 < len(parts)-1 && subtrees[strings.ToLower(parts[i+1])] {
				return true
			}
		}
	}
	return false
}

// generatedDirNames are directory names that, anywhere in a path, mark the
// file as build/tool output rather than source. Every one of these is created
// by a tool and is unambiguous: unlike `dist/`, `build/`, `out/` or `target/`
// — which are routinely hand-authored content directories and are therefore
// deliberately absent here — no project hand-writes a `.next/` or a
// `__pycache__/`.
var generatedDirNames = map[string]bool{
	".next":              true,
	".nuxt":              true,
	".output":            true,
	".svelte-kit":        true,
	".astro":             true,
	".docusaurus":        true,
	".vitepress":         true,
	".vuepress":          true,
	".parcel-cache":      true,
	".turbo":             true,
	".angular":           true,
	"_site":              true, // Jekyll
	".jekyll-cache":      true,
	"__pycache__":        true,
	".pytest_cache":      true,
	".mypy_cache":        true,
	".ruff_cache":        true,
	".ipynb_checkpoints": true,
	"htmlcov":            true,
	".nyc_output":        true,
	".sass-cache":        true,
	".serverless":        true,
}

// isGeneratedDirPath reports whether rel traverses a tool-output directory.
// Beyond the fixed names, any directory called `cache`/`caches` or ending in
// `_cache`/`-cache`/`.cache` is a tool's scratch space by universal
// convention (`.stargazer_cache/`, `.gradle_cache/`).
func isGeneratedDirPath(rel string) bool {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for i := 0; i < len(parts)-1; i++ {
		p := strings.ToLower(parts[i])
		if generatedDirNames[p] {
			return true
		}
		// Only the decorated forms. A bare `cache/` is a perfectly ordinary
		// source package (`internal/cache/redis.go`), and skipping it would
		// hide real credentials and addresses in first-party code.
		if strings.HasSuffix(p, "_cache") || strings.HasSuffix(p, "-cache") ||
			strings.HasSuffix(p, ".cache") {
			return true
		}
	}
	return false
}

// isMinifiedBundle reports whether rel is a minified frontend bundle. These
// are excluded from secrets_scan's skip list on purpose (a build-time-injected
// key lives there and nowhere else) but are pure noise for the address/URL
// gates, where every match came from the library that was bundled.
func isMinifiedBundle(rel string) bool {
	base := strings.ToLower(filepath.Base(rel))
	return strings.HasSuffix(base, ".min.js") || strings.HasSuffix(base, ".min.css") ||
		strings.HasSuffix(base, ".min.mjs") || strings.HasSuffix(base, ".bundle.js")
}

// binaryFileExtensions are extensions whose payload is binary even when the
// first 8 KiB happens to contain no NUL byte — the case isBinary misses. A
// PDF in particular starts with an ASCII header and an uncompressed object
// table, so a byte-scan of one yields "IPv4 addresses" and "http:// URLs"
// lifted out of compressed streams and font tables.
var binaryFileExtensions = map[string]bool{
	".pdf": true, ".ps": true, ".eps": true,
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".bmp": true,
	".tif": true, ".tiff": true, ".webp": true, ".ico": true, ".icns": true,
	".avif": true, ".heic": true, ".psd": true, ".ai": true, ".sketch": true,
	".mp3": true, ".wav": true, ".flac": true, ".ogg": true, ".m4a": true,
	".aac": true, ".opus": true, ".mid": true, ".midi": true,
	".mp4": true, ".mkv": true, ".mov": true, ".avi": true, ".webm": true,
	".wmv": true, ".flv": true, ".m4v": true,
	".zip": true, ".gz": true, ".tgz": true, ".bz2": true, ".xz": true,
	".zst": true, ".7z": true, ".rar": true, ".tar": true, ".lz4": true,
	".jar": true, ".war": true, ".ear": true, ".apk": true, ".ipa": true,
	".deb": true, ".rpm": true, ".pkg": true, ".dmg": true, ".msi": true,
	".exe": true, ".dll": true, ".so": true, ".dylib": true, ".a": true,
	".o": true, ".obj": true, ".class": true, ".wasm": true, ".bin": true,
	".pyc": true, ".pyo": true, ".pyd": true,
	".doc": true, ".docx": true, ".xls": true, ".xlsx": true, ".ppt": true,
	".pptx": true, ".odt": true, ".ods": true, ".odp": true, ".rtf": true,
	".woff": true, ".woff2": true, ".ttf": true, ".otf": true, ".eot": true,
	".db": true, ".sqlite": true, ".sqlite3": true, ".mdb": true,
	".safetensors": true, ".onnx": true, ".pt": true, ".pth": true,
	".ckpt": true, ".h5": true, ".pb": true, ".tflite": true, ".gguf": true,
	".pkl": true, ".pickle": true, ".npy": true, ".npz": true, ".bson": true,
}

// isBinaryPath reports whether rel carries a known-binary extension. Content
// gates check this before reading so a PDF or a model checkpoint is never
// byte-scanned for credentials, addresses, or TODO markers.
func isBinaryPath(rel string) bool {
	return binaryFileExtensions[strings.ToLower(filepath.Ext(rel))]
}

// buildOutputSubPaths are two-segment path prefixes that are unambiguous
// build output even though their first segment (`target/`) is ambiguous on
// its own. Cargo and Maven both write `target/debug` / `target/release`; no
// project hand-authors one.
var buildOutputSubPaths = [][2]string{
	{"target", "debug"},
	{"target", "release"},
}

// isSubsumedByVendoredFinding reports whether a per-file finding about rel
// would merely restate what vendored_dir_tracked already says once about the
// whole directory. `node_modules/typescript/lib/typescript.js is 8.7 MiB` and
// `node_modules/didyoumean/package.json is mode 100755` are not separate
// problems from "node_modules is tracked" — they are the same problem,
// itemised. Metadata gates use this so the actionable finding stays alone.
func isSubsumedByVendoredFinding(rel string) bool {
	if isDependencyPath(rel) || isGeneratedDirPath(rel) {
		return true
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for i := 0; i+2 < len(parts); i++ {
		for _, sub := range buildOutputSubPaths {
			if strings.EqualFold(parts[i], sub[0]) && strings.EqualFold(parts[i+1], sub[1]) {
				return true
			}
		}
	}
	return false
}
