package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeConflictMarkers_DetectsAllThreeMarkerKinds(t *testing.T) {
	cases := map[string]string{
		"left":   "ok\n<<<<<<< HEAD\nleft side\n=======\nright side\n>>>>>>> branch\n",
		"right":  "first line\nsecond line\n>>>>>>> remote/main\n",
		"diff3":  "a\n||||||| merged common ancestors\nbase\n=======\nbranch\n>>>>>>> branch\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			root := initRepoWithFiles(t, map[string]string{"file.txt": body})
			fs, err := checkMergeConflictMarkers(context.Background(), root, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(fs) != 1 {
				t.Fatalf("expected 1 finding, got %d: %+v", len(fs), fs)
			}
			if fs[0].Severity != SeverityError {
				t.Errorf("severity = %q, want error", fs[0].Severity)
			}
		})
	}
}

// Lines that LOOK like markers but lack the strict shape (no space, fewer
// chars, prose) must NOT trigger.
func TestMergeConflictMarkers_NoFalsePositives(t *testing.T) {
	body := strings.Join([]string{
		"<<<< three less-than-only",      // 4 < — too short
		"<<<<<<<<<X — eight, no space",   // 9 with no space → fail
		"normal text",
		"======= three equals separator", // separator alone (we don't trigger)
		"some <<<<<<< inline doesn't count because it's not at column 0",
		"// A C++ comment",
	}, "\n")
	root := initRepoWithFiles(t, map[string]string{"a.txt": body + "\n"})
	fs, err := checkMergeConflictMarkers(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 0 {
		t.Errorf("expected zero findings, got: %+v", fs)
	}
}

func TestMergeConflictMarkers_NotGitRepo(t *testing.T) {
	fs, err := checkMergeConflictMarkers(context.Background(), t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 || fs[0].Severity != SeverityInfo {
		t.Errorf("expected one info skip, got: %+v", fs)
	}
}

func TestLargeFileTracked_TripsAtThreshold(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)
	// 100 KiB file — well under default 5 MiB.
	if err := os.WriteFile(filepath.Join(root, "small.bin"), make([]byte, 100*1024), 0o644); err != nil {
		t.Fatal(err)
	}
	// 7 MiB file — over the default threshold.
	if err := os.WriteFile(filepath.Join(root, "big.bin"), make([]byte, 7*1024*1024), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "config", "user.email", "t@t")
	runGit(t, root, "config", "user.name", "t")
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-q", "-m", "x")

	fs, err := checkLargeFileTracked(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 || fs[0].FilePath != "big.bin" {
		t.Fatalf("expected single finding for big.bin, got: %+v", fs)
	}
	if fs[0].Severity != SeverityWarning {
		t.Errorf("severity = %q, want warning", fs[0].Severity)
	}
}

// The config knob lowers the threshold so a normally-fine file becomes a
// finding — confirms gate_options plumbing.
func TestLargeFileTracked_RespectsCustomThreshold(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		"medium.bin": strings.Repeat("a", 200*1024), // 200 KiB
	})
	// threshold_mb=0 is invalid (clamped to default), so use 1 MiB which is
	// still well above 200 KiB → no finding.
	loose := []byte(`{"threshold_mb": 1}`)
	fs, err := checkLargeFileTracked(context.Background(), root, loose)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 0 {
		t.Fatalf("expected zero findings at 1 MiB threshold, got: %+v", fs)
	}

	// Now drop the threshold below the file size — 1 MiB is still 1 MiB
	// because we accept threshold_mb >= 1 only. Build a 2 MiB file and
	// re-run with threshold_mb=1 to confirm the path fires.
	root2 := initRepoWithFiles(t, map[string]string{
		"big.bin": strings.Repeat("a", 2*1024*1024),
	})
	fs2, err := checkLargeFileTracked(context.Background(), root2, []byte(`{"threshold_mb": 1}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(fs2) != 1 {
		t.Fatalf("expected 1 finding with 1 MiB threshold on 2 MiB file, got: %+v", fs2)
	}
}

func TestLargeFileTracked_NotGitRepo(t *testing.T) {
	fs, err := checkLargeFileTracked(context.Background(), t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 || fs[0].Severity != SeverityInfo {
		t.Errorf("expected one info skip, got: %+v", fs)
	}
}
