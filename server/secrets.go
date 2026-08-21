package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// secretPattern is one rule for the secrets_scan gate. We deliberately keep
// a small, high-precision set: every false positive turns into a finding the
// user must triage, so loose patterns (generic "high-entropy string") are
// out of scope until we have an ignore-baseline mechanism.
type secretPattern struct {
	id         string
	title      string
	re         *regexp.Regexp
	minEntropy float64 // 0 = no entropy check; else Shannon bits/char floor
}

// secretPatterns is the active rule set. Adding a pattern means appending
// here — the gate auto-picks it up.
//
// minEntropy = 3.5 on all variable-body patterns filters out placeholder /
// mock / documentation strings (e.g. AKIAIOSFODNN7EXAMPLE, ghp_aaa…) while
// leaving real credentials untouched. Private-key headers are structural
// markers, not variable strings — no entropy check needed there.
var secretPatterns = []secretPattern{
	{id: "aws_access_key", title: "AWS access key ID", re: regexp.MustCompile(`AKIA[0-9A-Z]{16}`), minEntropy: 3.5},
	{id: "github_pat_classic", title: "GitHub personal access token", re: regexp.MustCompile(`gh[psoru]_[A-Za-z0-9]{36}`), minEntropy: 3.5},
	{id: "github_pat_fg", title: "GitHub fine-grained PAT", re: regexp.MustCompile(`github_pat_[A-Za-z0-9_]{82}`), minEntropy: 3.5},
	{id: "openai_key", title: "OpenAI API key", re: regexp.MustCompile(`sk-[A-Za-z0-9]{48}`), minEntropy: 3.5},
	{id: "anthropic_key", title: "Anthropic API key", re: regexp.MustCompile(`sk-ant-[A-Za-z0-9_\-]{40,}`), minEntropy: 3.5},
	{id: "google_api_key", title: "Google API key", re: regexp.MustCompile(`AIza[0-9A-Za-z_\-]{35}`), minEntropy: 3.5},
	// Real Slack tokens are `xox<kind>-<digits>-<digits>[-<digits>]-<hex/alnum secret>`.
	// The previous shape (`xox[abprs]-[0-9A-Za-z-]{10,}`) matched any hyphenated
	// word salad after the prefix, so documentation lines like
	// `SLACK_BOT_TOKEN=xoxb-workspace1-token` and a test's
	// `"xoxb-1234567890-1234567890-"` literal both reported as leaked tokens.
	{id: "slack_token", title: "Slack token", re: regexp.MustCompile(`xox[abprs]-[0-9]{10,13}-[0-9]{10,13}(?:-[0-9]{10,13})?-[A-Za-z0-9]{24,}`), minEntropy: 3.5},
	{id: "stripe_live", title: "Stripe live secret key", re: regexp.MustCompile(`sk_live_[0-9A-Za-z]{24,}`), minEntropy: 3.5},
	{id: "jwt", title: "JWT-like token", re: regexp.MustCompile(`eyJ[A-Za-z0-9_\-]{10,}\.eyJ[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}`), minEntropy: 3.5},
	{id: "private_key_header", title: "Private key", re: regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
}

// shannonEntropy returns the Shannon entropy in bits per character of s.
// Returns 0 for strings shorter than 2 characters.
func shannonEntropy(s string) float64 {
	if len(s) < 2 {
		return 0
	}
	freq := make(map[rune]int, 64)
	total := 0
	for _, c := range s {
		freq[c]++
		total++
	}
	var h float64
	for _, count := range freq {
		p := float64(count) / float64(total)
		h -= p * math.Log2(p)
	}
	return h
}

// Files larger than this are skipped — they're almost always artefacts
// (lockfiles, vendored dumps, generated bundles) where every line scan would
// be wasted I/O and any match would be noise.
const secretsMaxFileSize = 2 * 1024 * 1024

// secretMatchSuppressed reports whether a regex match for pattern p should be
// treated as a non-secret and dropped. This is the single FP-suppression chain
// shared by the working-tree gate (checkSecretsScan) and the history gate
// (scanHistoryBlob) so both apply identical filtering — a doc example or
// placeholder must not surface in history just because it was once committed.
//
//   - match:     the bytes the pattern matched
//   - rel:       path of the file/blob (for source-literal detection)
//   - content:   the full line/segment the match sits in
//   - at:        byte offset of the match start within content
//   - following: everything after this line, used to check whether a PEM
//     header is actually followed by key material
func secretMatchSuppressed(p secretPattern, match []byte, rel string, content []byte, at int, following []byte) bool {
	// Entropy floor: skip low-entropy matches (mock data, doc examples,
	// placeholder strings that happen to satisfy the pattern syntax).
	if p.minEntropy > 0 && shannonEntropy(string(match)) < p.minEntropy {
		return true
	}
	// Known-non-secret filter: skip values that are publicly documented
	// defaults, template placeholders, test key prefixes, or canonical
	// documentation examples — they carry zero information advantage.
	if isKnownNonSecret(string(match)) {
		return true
	}
	// Synthetic filler that the entropy floor cannot catch: a token body
	// spelled out as a monotone character run (`abcdefghijklmnopqrstuvwxyz`,
	// `0123456789`) scores maximal entropy precisely because every character
	// is distinct.
	if p.minEntropy > 0 && hasSequentialRun(string(match)) {
		return true
	}
	// Structural marker patterns (private_key_header) match a header string,
	// not a value. In source code that parses or matches keys, the header
	// appears as a literal string — that's code, not a leak. Skip when the
	// match sits immediately after an opening quote in a source file.
	if p.id == "private_key_header" && isQuotedLiteralInSource(rel, content, at) {
		return true
	}
	// A PEM header with no key material after it is a MENTION of the format,
	// not a key: an error message, a README explaining what to paste, a
	// changelog entry describing this very rule. A real key always puts a
	// base64 body on the next line — that is what makes the file a key.
	if p.id == "private_key_header" && !isKeyFileName(rel) &&
		!pemBodyFollows(content, at+len(match), following) {
		return true
	}
	return false
}

// keyFileExtensions / keyFileBasenames name a file whose whole reason to
// exist is to hold a private key. In one of those the header is proof enough
// — the body check is skipped so an unusually-wrapped or truncated key still
// fires.
var keyFileExtensions = map[string]bool{
	".pem": true, ".key": true, ".p8": true, ".pkcs8": true, ".pk8": true,
	".priv": true, ".ppk": true, ".jks": true, ".p12": true, ".pfx": true,
}

var keyFileBasenames = map[string]bool{
	"id_rsa": true, "id_dsa": true, "id_ecdsa": true, "id_ed25519": true,
	"server.key": true, "client.key": true, "privkey.pem": true,
}

// isKeyFileName reports whether rel names a private-key file.
func isKeyFileName(rel string) bool {
	base := strings.ToLower(filepath.Base(rel))
	if keyFileBasenames[base] {
		return true
	}
	return keyFileExtensions[strings.ToLower(filepath.Ext(base))]
}

// pemBase64LineRe matches a full line of PEM key material: at least 40
// base64 characters and nothing else. Real PEM bodies wrap at 64.
var pemBase64LineRe = regexp.MustCompile(`^[A-Za-z0-9+/]{40,}={0,2}$`)

// pemBodyFollows reports whether key material follows a PEM header. It looks
// at the remainder of the header's own line first (a one-line PEM blob) and
// then at the next few non-empty lines, which is where a normally-formatted
// key puts its first base64 row.
func pemBodyFollows(content []byte, afterMatch int, following []byte) bool {
	if afterMatch < len(content) && pemBase64LineRe.Match(bytes.TrimSpace(content[afterMatch:])) {
		return true
	}
	checked := 0
	for _, raw := range bytes.Split(following, []byte("\n")) {
		line := bytes.TrimSpace(raw)
		if len(line) == 0 {
			continue
		}
		if pemBase64LineRe.Match(line) {
			return true
		}
		checked++
		// Only the first couple of non-empty lines can hold the body; past
		// that we are reading unrelated content.
		if checked >= 2 {
			return false
		}
	}
	return false
}

func checkSecretsScan(ctx context.Context, root string, opts json.RawMessage) ([]Finding, error) {
	if !isGitRepo(root) {
		return []Finding{{
			Severity: SeverityInfo,
			Title:    "secrets_scan skipped (not a git repository)",
			Message:  "Project root has no .git/. Initialize git or run gates from inside a clone — the secrets gate uses 'git ls-files' to honour .gitignore.",
			FilePath: ".git",
		}}, nil
	}

	files, err := gitLsFiles(ctx, root)
	if err != nil {
		// Surface as a finding rather than aborting the whole batch; a
		// missing `git` binary or permission issue shouldn't kill checks
		// for the other gates.
		return []Finding{{
			Severity: SeverityWarning,
			Title:    "secrets_scan failed",
			Message:  fmt.Sprintf("Could not enumerate tracked files: %v", err),
			FilePath: ".git",
		}}, nil
	}

	scan := parseScanOptions(opts)
	out := []Finding{}
	for _, rel := range files {
		// Content-scan gate: honour the default data-file / backup-path
		// skips too, not just exclude_paths + fixtures. A credential-shaped
		// column in a .csv export or a shelved .bak snapshot is the file's
		// payload, not an embedded literal. Deliberately uses the
		// except-data-dirs variant: a real credential committed into a
		// dataset directory (data/, corpus/) is still a leak we must report.
		if scan.shouldSkipContentExceptDataDirs(rel) {
			continue
		}
		// Detection-rule files (YARA, …) contain secret patterns as the
		// payload of the rule — the file's reason to exist is the
		// pattern, not its leak. Skip outright.
		if isDetectionRuleFile(rel) {
			continue
		}
		// Tracked .env files are flagged regardless of content (the file
		// itself is the smell). .env.example / .env.template / .env.sample
		// are intentional and don't count.
		base := strings.ToLower(filepath.Base(rel))
		if base == ".env" {
			out = append(out, Finding{
				Severity: SeverityError,
				Title:    "Tracked .env file",
				Message:  fmt.Sprintf("%s is tracked in git. .env files typically hold secrets and shouldn't be committed. Move secrets to a vault and add .env to .gitignore.", rel),
				FilePath: rel + ":0:env_tracked",
			})
		}

		abs := filepath.Join(root, rel)
		info, err := os.Stat(abs)
		if err != nil || info.IsDir() {
			continue
		}
		if info.Size() > secretsMaxFileSize {
			continue
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		if isBinary(data) {
			continue
		}

		relForLookup := rel
		line := 1
		start := 0
		emit := func(content []byte, lineNum int, following []byte) {
			for _, p := range secretPatterns {
				idx := p.re.FindIndex(content)
				if idx == nil {
					continue
				}
				match := content[idx[0]:idx[1]]
				if secretMatchSuppressed(p, match, relForLookup, content, idx[0], following) {
					continue
				}
				out = append(out, Finding{
					Severity: SeverityError,
					Title:    p.title + " in tracked file",
					Message:  fmt.Sprintf("Possible %s in %s:%d. Verify, rotate if real, then purge it from git history (e.g. with git-filter-repo).", p.title, rel, lineNum),
					FilePath: fmt.Sprintf("%s:%d:%s", rel, lineNum, p.id),
				})
			}
		}
		for i := 0; i < len(data); i++ {
			if data[i] == '\n' {
				emit(data[start:i], line, data[i+1:])
				line++
				start = i + 1
			}
		}
		if start < len(data) {
			emit(data[start:], line, nil)
		}
	}
	return out, nil
}

// isGitRepo accepts both a regular .git directory and the "gitdir: ..." file
// used by worktrees and submodules.
func isGitRepo(root string) bool {
	info, err := os.Stat(filepath.Join(root, ".git"))
	if err != nil {
		return false
	}
	return info.IsDir() || info.Mode().IsRegular()
}

func gitLsFiles(ctx context.Context, root string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "ls-files", "-z")
	stdout, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	parts := bytes.Split(stdout, []byte{0})
	files := make([]string, 0, len(parts))
	for _, p := range parts {
		if len(p) > 0 {
			files = append(files, string(p))
		}
	}
	return files, nil
}

// gitFileEntry describes one entry from `git ls-files -s -z`. Mode is the
// 6-digit octal git mode (100644 / 100755 / 120000 / 160000 / 040000),
// not a unix file mode — git only stores a coarse subset.
type gitFileEntry struct {
	Mode string
	Hash string
	Path string
}

// gitLsFilesWithMode runs `git ls-files -s -z` and returns parsed entries.
// Format: "<mode> <hash> <stage>\t<path>\0". The stage is always 0 in a
// non-merge state; we drop it.
func gitLsFilesWithMode(ctx context.Context, root string) ([]gitFileEntry, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "ls-files", "-s", "-z")
	stdout, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	parts := bytes.Split(stdout, []byte{0})
	out := make([]gitFileEntry, 0, len(parts))
	for _, p := range parts {
		if len(p) == 0 {
			continue
		}
		// Find the tab that separates the metadata triple from the path.
		tab := bytes.IndexByte(p, '\t')
		if tab < 0 {
			continue
		}
		meta := string(p[:tab])
		path := string(p[tab+1:])
		fields := bytes.Fields([]byte(meta))
		if len(fields) < 3 {
			continue
		}
		out = append(out, gitFileEntry{
			Mode: string(fields[0]),
			Hash: string(fields[1]),
			Path: path,
		})
	}
	return out, nil
}

// isDetectionRuleFile returns true for files that exist to declare
// detection patterns (YARA, …). These legitimately contain secret-like
// strings as the rule's payload — flagging them generates noise on
// every security/detection toolkit repo.
func isDetectionRuleFile(rel string) bool {
	switch strings.ToLower(filepath.Ext(rel)) {
	case ".yar", ".yara", ".sigma":
		return true
	}
	// A YAML/JSON/Markdown file under a detection-rule directory carries the
	// pattern as its payload: a placeholder marker quoted as a regex in
	// `rules/VBC-056.yaml`, a cleartext-protocol probe in
	// `rules/vulnerability.json`, the prose describing either in
	// `docs/rules/VBC-056.md`. The marker is what the file is ABOUT.
	//
	// Requires both the directory and a rule-carrying extension, so a real
	// source file under rules/ (`rules/engine.go`) is never silenced.
	switch strings.ToLower(filepath.Ext(rel)) {
	case ".yml", ".yaml", ".json", ".md", ".toml", ".conf":
	default:
		return false
	}
	// A file NAMED for the rule set is the same thing without the directory:
	// `rules.md`, `config/rules.yaml`, `signatures.json`.
	switch strings.TrimSuffix(strings.ToLower(filepath.Base(rel)), filepath.Ext(rel)) {
	case "rules", "ruleset", "signatures", "detections", "patterns":
		return true
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for i := 0; i < len(parts)-1; i++ {
		switch strings.ToLower(parts[i]) {
		case "rules", "signatures", "detections", "detection-rules", "sigs", "yara":
			return true
		}
	}
	return false
}

// sourceCodeExtensions covers files where a comment-prefixed line
// (`//`, `#`, `/*`, `*`, `--`, `;`) bearing a key-header string is
// almost always documentation about the parser/format, not committed
// key material. Used only for comment-context detection; quote-context
// detection now runs on every file because string-literal embedding
// of header markers is a cross-language pattern (YAML rule files,
// Astro/Vue/Svelte/HTML attributes, .md inline code, …).
var sourceCodeExtensions = map[string]bool{
	".go":     true,
	".rs":     true,
	".ts":     true,
	".tsx":    true,
	".js":     true,
	".jsx":    true,
	".mjs":    true,
	".cjs":    true,
	".py":     true,
	".rb":     true,
	".java":   true,
	".kt":     true,
	".kts":    true,
	".scala":  true,
	".cs":     true,
	".cpp":    true,
	".cc":     true,
	".c":      true,
	".h":      true,
	".hpp":    true,
	".php":    true,
	".swift":  true,
	".m":      true,
	".mm":     true,
	".dart":   true,
	".astro":  true,
	".vue":    true,
	".svelte": true,
	".html":   true,
	".htm":    true,
	".sql":    true,
	".sh":     true,
	".bash":   true,
	".zsh":    true,
	".ps1":    true,
	".lua":    true,
	".pl":     true,
	".r":      true,
	".jl":     true,
}

// isQuotedLiteralInSource returns true when a private-key-header match
// at offset `at` inside the line `content` looks like a header string
// in a literal context — not committed key material. Three signatures:
//
//  1. Quote-preceded (any file): the char immediately before `at` is
//     `"`, `'`, or “ ` “. Covers TS/Go/Py string literals, YAML
//     `- "-----BEGIN …"`, Astro/HTML `placeholder="…"`, and Markdown
//     inline code “ `-----BEGIN …` “. Cross-language.
//
//  2. Comment-line in a source file: the line up to `at`, after
//     trimming whitespace, starts with a comment marker (`//`, `#`,
//     `/*`, `*`, `--`, `;`, `<!--`). Catches docstrings and inline
//     comments explaining a parser's supported headers.
//
// Genuine PEM blobs sit at column 0 on their own line with no quote
// or comment ahead of them — they keep firing.
func isQuotedLiteralInSource(rel string, content []byte, at int) bool {
	if at <= 0 {
		return false
	}
	prev := content[at-1]
	if prev == '"' || prev == '\'' || prev == '`' {
		return true
	}
	if !sourceCodeExtensions[strings.ToLower(filepath.Ext(rel))] {
		return false
	}
	// The marker can sit anywhere inside the literal, not just at its
	// start — an error message reads
	// `"Private key must be PEM with -----BEGIN PRIVATE KEY----- (PKCS#8)."`
	// and a Rust test builds `"head -----BEGIN RSA PRIVATE KEY-----\n…"`.
	// Both are code that mentions the header, not a key.
	if insideStringLiteral(content, at) {
		return true
	}
	// Comment-line check: scan the line prefix and see whether it
	// opens with a comment marker.
	prefix := bytes.TrimLeft(content[:at], " \t")
	for _, marker := range commentMarkers {
		if bytes.HasPrefix(prefix, []byte(marker)) {
			return true
		}
	}
	return false
}

// commentMarkers are the leading sequences that open a line-or-block
// comment across the languages we care about. Order is irrelevant —
// every prefix is tried.
var commentMarkers = []string{"//", "#", "/*", "*", "--", ";", "<!--"}

// isBinary uses the same heuristic git itself does: any NUL byte in the
// first 8 KiB means binary. Cheap and correct for our purposes.
func isBinary(data []byte) bool {
	n := len(data)
	if n > 8192 {
		n = 8192
	}
	return bytes.IndexByte(data[:n], 0) >= 0
}

// insideStringLiteral reports whether byte offset at sits inside a quoted
// string literal on its line. It counts unescaped quotes of each flavour
// before the offset: an odd count means the offset is inside an open literal.
//
// Deliberately per-line and per-flavour, which keeps it cheap and makes the
// failure mode a miss (no suppression) rather than an over-suppression.
func insideStringLiteral(content []byte, at int) bool {
	lineStart := bytes.LastIndexByte(content[:at], '\n') + 1
	prefix := content[lineStart:at]
	counts := map[byte]int{'"': 0, '\'': 0, '`': 0}
	for i := 0; i < len(prefix); i++ {
		c := prefix[i]
		if c == '\\' {
			i++ // skip the escaped byte
			continue
		}
		if _, tracked := counts[c]; tracked {
			counts[c]++
		}
	}
	for _, n := range counts {
		if n%2 == 1 {
			return true
		}
	}
	return false
}

// syntheticRunRe is not usable here (Go's RE2 has no backreferences), so
// hasSequentialRun does the work directly.

// sequentialRunFloor is how many consecutive characters make a string
// obviously hand-typed filler. Eight is long enough that no real credential
// has ever contained such a run by chance (probability ~62^-7 per position)
// and short enough to catch `abcdefgh…` and `01234567…`.
const sequentialRunFloor = 8

// hasSequentialRun reports whether s contains a run of at least
// sequentialRunFloor characters whose code points step by exactly +1 or -1.
//
// This is the hole the Shannon-entropy floor cannot cover: a fake token like
// `ghp_abcdefghijklmnopqrstuvwxyz0123456789` uses 36 distinct characters and
// therefore scores the MAXIMUM possible entropy (~5.17 bits/char), sailing
// past a 3.5 floor — while being the most obviously synthetic string a
// developer could type. Real credentials are drawn at random and effectively
// never contain a monotone run this long.
func hasSequentialRun(s string) bool {
	runes := []rune(s)
	if len(runes) < sequentialRunFloor {
		return false
	}
	asc, desc := 1, 1
	for i := 1; i < len(runes); i++ {
		switch runes[i] - runes[i-1] {
		case 1:
			asc++
			desc = 1
		case -1:
			desc++
			asc = 1
		default:
			asc, desc = 1, 1
		}
		if asc >= sequentialRunFloor || desc >= sequentialRunFloor {
			return true
		}
	}
	return false
}
