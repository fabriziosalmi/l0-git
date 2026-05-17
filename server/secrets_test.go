package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initRepoWithFiles creates a temp git repo, writes the given files, and
// stages+commits them so they show up in `git ls-files`.
func initRepoWithFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	gitInit(t, root)
	for rel, content := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, root, "config", "user.email", "t@t")
	runGit(t, root, "config", "user.name", "t")
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-q", "-m", "x")
	return root
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// TestSecretsScan_PatternsFire is table-driven: each row plants a single
// fake match in a tracked file and asserts the corresponding pattern_id
// shows up in the findings.
func TestSecretsScan_PatternsFire(t *testing.T) {
	// High-entropy strings are required for patterns with an entropy floor.
	// These are synthetic but random-looking; patterns without entropy checks
	// (private_key_header) use their literal structure.
	cases := []struct {
		name      string
		content   string
		patternID string
	}{
		// AWS: charset is [0-9A-Z] only — use uppercase + digits.
		{"aws", "AKIA1A2B3C4D5E6F7G8H", "aws_access_key"},
		// GitHub classic: mixed case + digits.
		{"github_classic", "ghp_" + highEntropyAlnum(36), "github_pat_classic"},
		{"github_fg", "github_pat_" + highEntropyAlnum(82), "github_pat_fg"},
		{"openai", "sk-" + highEntropyAlnum(48), "openai_key"},
		{"anthropic", "sk-ant-" + highEntropyAlnum(50), "anthropic_key"},
		{"google", "AIza" + highEntropyAlnum(35), "google_api_key"},
		{"slack", "xoxb-1a2B3c4D5e6F7g8H12", "slack_token"},
		{"stripe", "sk_live_" + highEntropyAlnum(30), "stripe_live"},
		{"jwt", "eyJ" + highEntropyAlnum(12) + ".eyJ" + highEntropyAlnum(12) + "." + highEntropyAlnum(12), "jwt"},
		{"pem", "-----BEGIN RSA PRIVATE KEY-----", "private_key_header"},
	}
	ctx := context.Background()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := initRepoWithFiles(t, map[string]string{"leaky.txt": tc.content + "\n"})
			fs, err := checkSecretsScan(ctx, root, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !findingsContainPattern(fs, tc.patternID) {
				t.Fatalf("pattern %q not detected; findings: %+v", tc.patternID, fs)
			}
		})
	}
}

// TestSecretsScan_NoFalsePositives confirms an ordinary tracked file with
// no secret material produces no findings.
func TestSecretsScan_NoFalsePositives(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		"README.md": "# Hello\n\nThis project is an example. AKIA short. ghp_short.\n",
		"main.go":   "package main\n\nfunc main() {}\n",
	})
	fs, err := checkSecretsScan(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 0 {
		t.Fatalf("expected no findings, got: %+v", fs)
	}
}

// TestSecretsScan_TrackedEnvFile flags a tracked .env regardless of contents
// but explicitly leaves .env.example alone.
func TestSecretsScan_TrackedEnvFile(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		".env":         "DATABASE_URL=postgres://x\n",
		".env.example": "DATABASE_URL=postgres://x\n",
	})
	fs, err := checkSecretsScan(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !findingsContainPattern(fs, "env_tracked") {
		t.Fatalf("expected env_tracked finding, got: %+v", fs)
	}
	for _, f := range fs {
		if strings.HasPrefix(f.FilePath, ".env.example") {
			t.Fatalf(".env.example should not be flagged: %+v", f)
		}
	}
}

// TestSecretsScan_GitignoredFileSkipped confirms .gitignore is honoured —
// untracked files are invisible to the gate.
func TestSecretsScan_GitignoredFileSkipped(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		".gitignore":   "secret.txt\n",
		"placeholder":  "x",
	})
	// Add the secret AFTER commit so it's untracked AND ignored.
	if err := os.WriteFile(filepath.Join(root, "secret.txt"), []byte("AKIA"+strings.Repeat("A", 16)), 0o644); err != nil {
		t.Fatal(err)
	}
	fs, err := checkSecretsScan(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fs {
		if strings.HasPrefix(f.FilePath, "secret.txt") {
			t.Fatalf("gitignored file was scanned: %+v", f)
		}
	}
}

// TestSecretsScan_NotGitRepo emits one info-level skip finding and no others.
func TestSecretsScan_NotGitRepo(t *testing.T) {
	root := t.TempDir()
	fs, err := checkSecretsScan(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 || fs[0].Severity != SeverityInfo {
		t.Fatalf("expected single info finding, got: %+v", fs)
	}
}

// TestSecretsScan_BinarySkipped: a file containing a NUL byte must not be
// scanned (otherwise we'd waste time on PNGs / minified bundles).
func TestSecretsScan_BinarySkipped(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		"blob.bin": "AKIA" + strings.Repeat("A", 16) + "\x00more",
	})
	fs, err := checkSecretsScan(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fs {
		if strings.HasPrefix(f.FilePath, "blob.bin") {
			t.Fatalf("binary file was scanned: %+v", f)
		}
	}
}

// TestSecretsScan_LargeFileSkipped: > secretsMaxFileSize is ignored.
func TestSecretsScan_LargeFileSkipped(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)
	big := make([]byte, secretsMaxFileSize+1024)
	for i := range big {
		big[i] = 'a'
	}
	copy(big[100:], []byte("AKIA"+strings.Repeat("A", 16)))
	if err := os.WriteFile(filepath.Join(root, "big.txt"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "config", "user.email", "t@t")
	runGit(t, root, "config", "user.name", "t")
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-q", "-m", "big")
	fs, err := checkSecretsScan(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fs {
		if strings.HasPrefix(f.FilePath, "big.txt") {
			t.Fatalf("oversized file was scanned: %+v", f)
		}
	}
}

// gate_options.secrets_scan.exclude_paths must skip files whose relative
// path matches a glob pattern, leaving genuine matches everywhere else.
func TestSecretsScan_ExcludePaths(t *testing.T) {
	key := "AKIA1A2B3C4D5E6F7G8H" // high-entropy AWS-format key (uppercase+digits)
	root := initRepoWithFiles(t, map[string]string{
		"src/leaky.txt":  key + "\n",
		"test/leaky.txt": key + "\n",
	})
	opts := []byte(`{"exclude_paths": ["test/*"]}`)
	fs, err := checkSecretsScan(context.Background(), root, opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fs {
		if strings.HasPrefix(f.FilePath, "test/") {
			t.Errorf("excluded path was scanned: %+v", f)
		}
	}
	// And the unexcluded file is still flagged.
	if !findingsContainPattern(fs, "aws_access_key") {
		t.Errorf("expected aws_access_key from src/, got: %+v", fs)
	}
}

// highEntropyAlnum returns a deterministic alphanumeric string of length n
// with Shannon entropy ≥ 3.5 bits/char, suitable for use in test vectors
// that must clear the entropy floor of secretPatterns.
func highEntropyAlnum(n int) string {
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	b := make([]byte, n)
	for i := range b {
		b[i] = alphabet[i%len(alphabet)]
	}
	return string(b)
}

// TestShannonEntropy validates the helper against known values.
func TestShannonEntropy(t *testing.T) {
	cases := []struct {
		s    string
		low  bool // true = below 3.5 threshold
	}{
		{strings.Repeat("A", 20), true},          // single char → entropy 0
		{"AKIA" + strings.Repeat("A", 16), true}, // mostly same char
		{"AKIA1a2B3c4D5e6F7g8H", false},          // 8 distinct chars, 4 bits
		{highEntropyAlnum(40), false},             // cycling 62-char alphabet
	}
	for _, c := range cases {
		h := shannonEntropy(c.s)
		below := h < 3.5
		if below != c.low {
			t.Errorf("shannonEntropy(%q) = %.2f; below-3.5=%v, want %v", c.s, h, below, c.low)
		}
	}
}

// TestSecretsScan_LowEntropySkipped verifies that pattern matches with
// entropy below 3.5 bits/char are silently dropped (not false-positived).
func TestSecretsScan_LowEntropySkipped(t *testing.T) {
	cases := []string{
		"AKIA" + strings.Repeat("A", 16), // AWS mock — all same char
		"ghp_" + strings.Repeat("a", 36), // GitHub mock
		"sk-" + strings.Repeat("a", 48),  // OpenAI mock
	}
	ctx := context.Background()
	for _, content := range cases {
		t.Run(content[:10], func(t *testing.T) {
			root := initRepoWithFiles(t, map[string]string{"low_entropy.txt": content + "\n"})
			fs, err := checkSecretsScan(ctx, root, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range fs {
				if f.FilePath != "low_entropy.txt:0:env_tracked" {
					t.Errorf("low-entropy match must be skipped, got: %+v", f)
				}
			}
		})
	}
}

// YARA / detection-rule files contain secret-shaped strings (header
// markers, token formats) as the rule's payload. The file's reason to
// exist IS the pattern — flagging is pure noise on security toolkit
// repos.
func TestSecretsScan_DetectionRuleFilesSkipped(t *testing.T) {
	pem := "-----BEGIN PRIVATE KEY-----"
	for _, name := range []string{
		"rules/secrets.yar",
		"detect/keys.yara",
	} {
		t.Run(name, func(t *testing.T) {
			root := initRepoWithFiles(t, map[string]string{
				name:        "rule pk { strings: $h = \"" + pem + "\" condition: $h }\n",
				"prod.go":   "var x = []byte(\"" + pem + "\")\n", // also caught by string-literal heuristic below
				"leak.pem":  pem + "\n",                            // genuine
			})
			fs, err := checkSecretsScan(context.Background(), root, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range fs {
				if strings.HasPrefix(f.FilePath, name) {
					t.Errorf("%s must be skipped, got finding: %+v", name, f)
				}
			}
			// Genuine PEM on its own line in a .pem file must still fire.
			hit := false
			for _, f := range fs {
				if strings.HasPrefix(f.FilePath, "leak.pem") {
					hit = true
				}
			}
			if !hit {
				t.Errorf("expected leak.pem to still fire, got: %+v", fs)
			}
		})
	}
}

// In key-parsing / key-matching code, the header string appears as a
// string literal next to an opening quote — it's a header constant, not
// committed key material. Detect by the immediate-preceding-quote
// + source-file-extension signature.
func TestSecretsScan_PrivateKeyHeaderLiteralInSource(t *testing.T) {
	cases := []struct {
		path    string
		content string
	}{
		{"src/key-match.ts", `const HEADER = "-----BEGIN PRIVATE KEY-----";` + "\n"},
		{"lib/parse.go", `var hdr = "-----BEGIN RSA PRIVATE KEY-----"` + "\n"},
		{"app.py", `HEADER = '-----BEGIN PRIVATE KEY-----'` + "\n"},
		{"x.rs", "let h = \"-----BEGIN EC PRIVATE KEY-----\";\n"},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			root := initRepoWithFiles(t, map[string]string{c.path: c.content})
			fs, err := checkSecretsScan(context.Background(), root, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range fs {
				if strings.HasSuffix(f.FilePath, ":private_key_header") {
					t.Errorf("header in source string literal must be skipped: %+v", f)
				}
			}
		})
	}
}

// Cross-language quote-preceded header is treated as a string literal
// regardless of file extension. Catches YAML rule files, Astro/Vue/
// HTML component attributes, .md inline code, …
func TestSecretsScan_PrivateKeyHeaderQuotedAnywhere(t *testing.T) {
	cases := []struct {
		path    string
		content string
	}{
		{"rules/security.yml", `  - "-----BEGIN RSA PRIVATE KEY-----"` + "\n"},
		{"web/Tool.astro", `placeholder="-----BEGIN PRIVATE KEY-----..."` + "\n"},
		{"app.vue", `placeholder="-----BEGIN PRIVATE KEY-----"` + "\n"},
		{"docs/security.md", "Private keys (`-----BEGIN PRIVATE KEY-----`) are not allowed.\n"},
		{"docs/dlp.md", "| `-----BEGIN RSA PRIVATE KEY-----` | Aho-Corasick |\n"},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			root := initRepoWithFiles(t, map[string]string{c.path: c.content})
			fs, err := checkSecretsScan(context.Background(), root, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range fs {
				if strings.HasSuffix(f.FilePath, ":private_key_header") {
					t.Errorf("quoted header in %s must be skipped: %+v", c.path, f)
				}
			}
		})
	}
}

// Comments in source files that describe supported headers are not
// committed secrets. Catches `// -----BEGIN ENCRYPTED PRIVATE KEY-----`
// and similar across C-like, shell, SQL, and XML-style comments.
func TestSecretsScan_PrivateKeyHeaderInComment(t *testing.T) {
	cases := []struct {
		path    string
		content string
	}{
		{"tls.swift", "    //   -----BEGIN ENCRYPTED PRIVATE KEY-----   (PKCS#8 encrypted)\n"},
		{"util.go", "// -----BEGIN PRIVATE KEY-----\n"},
		{"parse.py", "# -----BEGIN PRIVATE KEY-----\n"},
		{"old.sql", "-- -----BEGIN PRIVATE KEY-----\n"},
		{"page.html", "<!-- -----BEGIN PRIVATE KEY----- -->\n"},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			root := initRepoWithFiles(t, map[string]string{c.path: c.content})
			fs, err := checkSecretsScan(context.Background(), root, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range fs {
				if strings.HasSuffix(f.FilePath, ":private_key_header") {
					t.Errorf("comment-context header in %s must be skipped: %+v", c.path, f)
				}
			}
		})
	}
}

// A genuine PEM blob (header at column 0 on its own line) MUST still
// fire — the literal-in-source heuristic must not over-match.
func TestSecretsScan_PrivateKeyHeaderGenuinePEM(t *testing.T) {
	cases := []struct {
		path    string
		content string
	}{
		{"key.pem", "-----BEGIN PRIVATE KEY-----\nMIIE...\n-----END PRIVATE KEY-----\n"},
		{"secrets/leaked.txt", "-----BEGIN RSA PRIVATE KEY-----\nMIIB...\n"},
		// Even in a source file — if the header isn't preceded by a
		// quote (no string-literal pattern), it's a leak.
		{"main.go", "/*\n-----BEGIN PRIVATE KEY-----\nMIIB...\n*/\n"},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			root := initRepoWithFiles(t, map[string]string{c.path: c.content})
			fs, err := checkSecretsScan(context.Background(), root, nil)
			if err != nil {
				t.Fatal(err)
			}
			hit := false
			for _, f := range fs {
				if strings.HasSuffix(f.FilePath, ":private_key_header") {
					hit = true
				}
			}
			if !hit {
				t.Errorf("genuine PEM in %s must fire, got: %+v", c.path, fs)
			}
		})
	}
}

func findingsContainPattern(fs []Finding, patternID string) bool {
	for _, f := range fs {
		// FilePath looks like "<rel>:<line>:<pattern_id>".
		if i := strings.LastIndex(f.FilePath, ":"); i > 0 {
			if f.FilePath[i+1:] == patternID {
				return true
			}
		}
	}
	return false
}
