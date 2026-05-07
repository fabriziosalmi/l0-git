package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	id    string
	title string
	re    *regexp.Regexp
}

// secretPatterns is the active rule set. Adding a pattern means appending
// here — the gate auto-picks it up.
var secretPatterns = []secretPattern{
	{id: "aws_access_key", title: "AWS access key ID", re: regexp.MustCompile(`AKIA[0-9A-Z]{16}`)},
	{id: "github_pat_classic", title: "GitHub personal access token", re: regexp.MustCompile(`gh[psoru]_[A-Za-z0-9]{36}`)},
	{id: "github_pat_fg", title: "GitHub fine-grained PAT", re: regexp.MustCompile(`github_pat_[A-Za-z0-9_]{82}`)},
	{id: "openai_key", title: "OpenAI API key", re: regexp.MustCompile(`sk-[A-Za-z0-9]{48}`)},
	{id: "anthropic_key", title: "Anthropic API key", re: regexp.MustCompile(`sk-ant-[A-Za-z0-9_\-]{40,}`)},
	{id: "google_api_key", title: "Google API key", re: regexp.MustCompile(`AIza[0-9A-Za-z_\-]{35}`)},
	{id: "slack_token", title: "Slack token", re: regexp.MustCompile(`xox[abprs]-[0-9A-Za-z\-]{10,}`)},
	{id: "stripe_live", title: "Stripe live secret key", re: regexp.MustCompile(`sk_live_[0-9A-Za-z]{24,}`)},
	{id: "jwt", title: "JWT-like token", re: regexp.MustCompile(`eyJ[A-Za-z0-9_\-]{10,}\.eyJ[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}`)},
	{id: "private_key_header", title: "Private key", re: regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
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

	excludes := parseScanOptions(opts).ExcludePaths
	out := []Finding{}
	for _, rel := range files {
		if pathExcluded(rel, excludes) {
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

		line := 1
		start := 0
		emit := func(content []byte, lineNum int) {
			for _, p := range secretPatterns {
				if p.re.Match(content) {
					out = append(out, Finding{
						Severity: SeverityError,
						Title:    p.title + " in tracked file",
						Message:  fmt.Sprintf("Possible %s in %s:%d. Verify, rotate if real, then purge it from git history (e.g. with git-filter-repo).", p.title, rel, lineNum),
						FilePath: fmt.Sprintf("%s:%d:%s", rel, lineNum, p.id),
					})
				}
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

// isBinary uses the same heuristic git itself does: any NUL byte in the
// first 8 KiB means binary. Cheap and correct for our purposes.
func isBinary(data []byte) bool {
	n := len(data)
	if n > 8192 {
		n = 8192
	}
	return bytes.IndexByte(data[:n], 0) >= 0
}

