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

// commitAndRemoveNamed is commitAndRemove for an arbitrary relative path (so a
// test can exercise the detection-rule / data-file skips that key off the path,
// not just secret.txt). Content is written verbatim.
func commitAndRemoveNamed(t *testing.T, relPath, content string) string {
	t.Helper()
	root := t.TempDir()
	gitInit(t, root)
	runGit(t, root, "config", "user.email", "t@t")
	runGit(t, root, "config", "user.name", "t")

	full := filepath.Join(root, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-q", "-m", "introduce")

	if err := os.Remove(full); err != nil {
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
	// A realistic high-entropy key — clears the 3.5 bits/char entropy floor
	// the history gate now shares with the working-tree gate. (A synthetic
	// all-same-char key would be suppressed as a placeholder, which is
	// exactly what TestSecretsScanHistory_SuppressesDocExample asserts.)
	root := commitAndRemove(t, "AKIA2E4F7HXMPL9QRSTU")
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

// TestSecretsScanHistory_SuppressesDocExample locks in the FP fix: the history
// gate must apply the same suppression chain as the working-tree gate. A
// canonical AWS documentation key committed-then-removed must NOT surface in
// history (it carries no information advantage and would otherwise re-appear
// on every scan forever with a destructive filter-repo remedy).
func TestSecretsScanHistory_SuppressesDocExample(t *testing.T) {
	root := commitAndRemove(t, "AKIAIOSFODNN7EXAMPLE")
	hist, err := checkSecretsScanHistory(context.Background(), root, []byte(`{"enabled": true}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range hist {
		if strings.HasSuffix(f.FilePath, ":aws_access_key") {
			t.Fatalf("doc-example key must be suppressed in history, got: %+v", f)
		}
	}
}

// TestSecretsScanHistory_SuppressesYaraRule locks in the detection-rule skip on
// the history path: a YARA file whose payload IS a private-key header must not
// surface as a history secret.
func TestSecretsScanHistory_SuppressesYaraRule(t *testing.T) {
	root := commitAndRemoveNamed(t, "rules/keys.yar", `rule k { strings: $h = "-----BEGIN RSA PRIVATE KEY-----" condition: $h }`)
	hist, err := checkSecretsScanHistory(context.Background(), root, []byte(`{"enabled": true}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range hist {
		if strings.HasSuffix(f.FilePath, ":private_key_header") {
			t.Fatalf("YARA-rule payload must be suppressed in history, got: %+v", f)
		}
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

