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
	{id: "slack_token", title: "Slack token", re: regexp.MustCompile(`xox[abprs]-[0-9A-Za-z\-]{10,}`), minEntropy: 3.5},
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
		if scan.shouldSkip(rel) {
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
		emit := func(content []byte, lineNum int) {
			for _, p := range secretPatterns {
				idx := p.re.FindIndex(content)
				if idx == nil {
					continue
				}
				match := content[idx[0]:idx[1]]
				// Entropy floor: skip low-entropy matches (mock data, doc examples,
				// placeholder strings that happen to satisfy the pattern syntax).
				if p.minEntropy > 0 && shannonEntropy(string(match)) < p.minEntropy {
					continue
				}
				// Known-non-secret filter: skip values that are publicly
				// documented defaults, template placeholders, test key prefixes,
				// or canonical documentation examples — they carry zero
				// information advantage for an attacker.
				if isKnownNonSecret(string(match)) {
					continue
				}
				// Structural marker patterns (private_key_header) match a
				// header string, not a value. In source code that parses
				// or matches keys, the header appears as a literal string
				// — that's code, not a leak. Skip when the match sits
				// immediately after an opening quote in a source file.
				if p.id == "private_key_header" && isQuotedLiteralInSource(relForLookup, content, idx[0]) {
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
				emit(data[start:i], line)
				line++
				start = i + 1
			}
		}
		if start < len(data) {
			emit(data[start:], line)
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
	case ".yar", ".yara":
		return true
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
//     `"`, `'`, or `` ` ``. Covers TS/Go/Py string literals, YAML
//     `- "-----BEGIN …"`, Astro/HTML `placeholder="…"`, and Markdown
//     inline code `` `-----BEGIN …` ``. Cross-language.
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

