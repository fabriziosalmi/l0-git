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
	cases := []struct {
		name      string
		content   string
		patternID string
	}{
		{"aws", "AKIA" + strings.Repeat("A", 16), "aws_access_key"},
		{"github_classic", "ghp_" + strings.Repeat("a", 36), "github_pat_classic"},
		{"github_fg", "github_pat_" + strings.Repeat("a", 82), "github_pat_fg"},
		{"openai", "sk-" + strings.Repeat("a", 48), "openai_key"},
		{"anthropic", "sk-ant-" + strings.Repeat("a", 50), "anthropic_key"},
		{"google", "AIza" + strings.Repeat("a", 35), "google_api_key"},
		{"slack", "xoxb-" + strings.Repeat("a", 20), "slack_token"},
		{"stripe", "sk_live_" + strings.Repeat("a", 30), "stripe_live"},
		{"jwt", "eyJ" + strings.Repeat("a", 12) + ".eyJ" + strings.Repeat("a", 12) + "." + strings.Repeat("a", 12), "jwt"},
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
	root := initRepoWithFiles(t, map[string]string{
		"src/leaky.txt":  "AKIA" + strings.Repeat("A", 16) + "\n",
		"test/leaky.txt": "AKIA" + strings.Repeat("B", 16) + "\n",
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
