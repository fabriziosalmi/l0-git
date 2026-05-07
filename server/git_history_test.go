package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// commitAndRemove sets up a repo where `secret.txt` was committed once
// (carrying a fake AWS key) and then removed in a second commit. The
// working tree is therefore clean — only history holds the secret.
func commitAndRemove(t *testing.T, secret string) string {
	t.Helper()
	root := t.TempDir()
	gitInit(t, root)
	runGit(t, root, "config", "user.email", "t@t")
	runGit(t, root, "config", "user.name", "t")

	if err := os.WriteFile(filepath.Join(root, "secret.txt"), []byte(secret+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-q", "-m", "introduce")

	if err := os.Remove(filepath.Join(root, "secret.txt")); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-q", "-m", "remove")
	return root
}

func TestSecretsScanHistory_DefaultOff(t *testing.T) {
	root := commitAndRemove(t, "AKIA"+strings.Repeat("A", 16))
	fs, err := checkSecretsScanHistory(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 0 {
		t.Errorf("default should be silent (opt-in only), got: %+v", fs)
	}
}

func TestSecretsScanHistory_FindsRemovedSecret(t *testing.T) {
	root := commitAndRemove(t, "AKIA"+strings.Repeat("A", 16))
	// Sanity: working-tree gate must not see anything.
	fs, err := checkSecretsScan(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fs {
		if strings.HasPrefix(f.FilePath, "secret.txt") {
			t.Fatalf("working-tree gate must not see removed file: %+v", f)
		}
	}
	// Now the history-aware gate, opt-in.
	hist, err := checkSecretsScanHistory(context.Background(), root, []byte(`{"enabled": true}`))
	if err != nil {
		t.Fatal(err)
	}
	hit := false
	for _, f := range hist {
		if strings.HasSuffix(f.FilePath, ":aws_access_key") {
			hit = true
			if !strings.Contains(f.Message, "filter-repo") {
				t.Errorf("history finding message must point to filter-repo: %q", f.Message)
			}
		}
	}
	if !hit {
		t.Fatalf("expected aws_access_key history finding, got: %+v", hist)
	}
}

func TestSecretsScanHistory_NotGitRepo(t *testing.T) {
	fs, err := checkSecretsScanHistory(context.Background(), t.TempDir(), []byte(`{"enabled": true}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 || fs[0].Severity != SeverityInfo {
		t.Fatalf("expected single info skip-finding, got: %+v", fs)
	}
}

func TestLargeBlobInHistory_DefaultOff(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{"hi.txt": "hi"})
	fs, err := checkLargeBlobInHistory(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 0 {
		t.Errorf("default off, got: %+v", fs)
	}
}

func TestLargeBlobInHistory_FindsRemovedBlob(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)
	runGit(t, root, "config", "user.email", "t@t")
	runGit(t, root, "config", "user.name", "t")

	// Commit a 6 MiB blob, then remove it.
	big := make([]byte, 6*1024*1024)
	for i := range big {
		big[i] = 'a' + byte(i%26)
	}
	if err := os.WriteFile(filepath.Join(root, "big.bin"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-q", "-m", "add big")
	if err := os.Remove(filepath.Join(root, "big.bin")); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-q", "-m", "remove big")

	fs, err := checkLargeBlobInHistory(context.Background(), root, []byte(`{"enabled": true, "threshold_mb": 5}`))
	if err != nil {
		t.Fatal(err)
	}
	hit := false
	for _, f := range fs {
		if strings.Contains(f.Message, "big.bin") {
			hit = true
			if !strings.Contains(f.Message, "filter-repo --strip-blobs-bigger-than") {
				t.Errorf("expected filter-repo guidance: %q", f.Message)
			}
		}
	}
	if !hit {
		t.Fatalf("expected big.bin finding, got: %+v", fs)
	}
}

func TestLargeBlobInHistory_BelowThresholdSilent(t *testing.T) {
	root := commitAndRemove(t, "tiny secret content")
	fs, err := checkLargeBlobInHistory(context.Background(), root, []byte(`{"enabled": true, "threshold_mb": 1}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fs {
		if strings.Contains(f.Message, "secret.txt") {
			t.Errorf("tiny blob below 1 MiB threshold must not fire: %+v", f)
		}
	}
}

// enumerateHistoryBlobs deduplicates by hash even when a blob appears in
// multiple commits / paths.
func TestEnumerateHistoryBlobs_Dedupe(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)
	runGit(t, root, "config", "user.email", "t@t")
	runGit(t, root, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("same content"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-q", "-m", "c1")
	// Re-touch to bump mtime but content stays the same → same blob.
	now := time.Now()
	if err := os.Chtimes(filepath.Join(root, "a.txt"), now, now); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "commit", "--allow-empty", "-q", "-m", "c2")

	blobs, err := enumerateHistoryBlobs(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, b := range blobs {
		if b.Path == "a.txt" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 dedupe'd blob entry for a.txt, got %d (blobs=%+v)", count, blobs)
	}
}

