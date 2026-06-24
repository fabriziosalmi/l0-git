package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// initRepoWithMode is like initRepoWithFiles but flips the git mode of
// the listed files to 100755 via `git update-index --chmod=+x`. We use
// the explicit git command rather than os.Chmod because Windows
// filesystems don't carry the unix executable bit, and the test needs
// to work on the CI matrix.
func initRepoWithMode(t *testing.T, files map[string]string, executable []string) string {
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
	for _, rel := range executable {
		// Forces git mode 100755 portably across Linux/macOS/Windows.
		runGit(t, root, "update-index", "--chmod=+x", rel)
	}
	runGit(t, root, "commit", "-q", "-m", "x")
	return root
}

func TestUnexpectedExecutableBit_FlagsTextFiles(t *testing.T) {
	root := initRepoWithMode(t,
		map[string]string{
			"README.md":           "# x\n",
			"config.yaml":         "x: 1\n",
			"scripts/build.sh":    "#!/usr/bin/env bash\n",
			"package-lock.json":   "{}\n",
		},
		[]string{"README.md", "config.yaml", "scripts/build.sh", "package-lock.json"},
	)
	fs, err := checkUnexpectedExecutableBit(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	flagged := map[string]bool{}
	for _, f := range fs {
		flagged[f.FilePath] = true
	}
	if !flagged["README.md"] || !flagged["config.yaml"] || !flagged["package-lock.json"] {
		t.Errorf("expected text/data files flagged, got: %+v", flagged)
	}
	if flagged["scripts/build.sh"] {
		t.Errorf("shell scripts should NOT be flagged: %+v", fs)
	}
}

func TestUnexpectedExecutableBit_NoFalsePositiveOnRegularFile(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{"README.md": "# x\n"})
	fs, err := checkUnexpectedExecutableBit(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 0 {
		t.Errorf("regular non-executable text file must not fire, got: %+v", fs)
	}
}

// Files in conventional script directories (bin/, scripts/, tools/, …) must
// not trigger unexpected_executable_bit regardless of extension.
func TestUnexpectedExecutableBit_ScriptDirExempt(t *testing.T) {
	root := initRepoWithMode(t, map[string]string{
		"bin/deploy":          "#!/bin/sh\necho deploy\n",
		"scripts/bootstrap":   "#!/bin/bash\necho bootstrap\n",
		"tools/lint.sh":       "#!/bin/sh\necho lint\n",
		"hack/update.py":      "#!/usr/bin/env python3\n",
		"cmd/run":             "#!/bin/sh\n",
	}, []string{
		"bin/deploy", "scripts/bootstrap", "tools/lint.sh", "hack/update.py", "cmd/run",
	})
	fs, err := checkUnexpectedExecutableBit(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 0 {
		t.Errorf("script-dir files must be silent, got: %+v", fs)
	}
}

// A data file outside script dirs with the executable bit set must still fire.
func TestUnexpectedExecutableBit_DataFileOutsideScriptDirFires(t *testing.T) {
	root := initRepoWithMode(t, map[string]string{
		"config/settings.json": `{"key":"value"}`,
	}, []string{"config/settings.json"})
	fs, err := checkUnexpectedExecutableBit(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) == 0 {
		t.Errorf("json file with executable bit outside bin/ must produce a finding")
	}
}

func TestVendoredDirTracked_FlagsCommonDirs(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		"node_modules/foo/index.js": "x",
		"vendor/bar/lib.go":         "package bar",
		"src/main.go":               "package main",
	})
	fs, err := checkVendoredDirTracked(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 2 {
		t.Fatalf("expected 2 findings (node_modules + vendor), got %d: %+v", len(fs), fs)
	}
	keys := map[string]bool{}
	for _, f := range fs {
		keys[f.FilePath] = true
	}
	if !keys["node_modules"] || !keys["vendor"] {
		t.Errorf("expected node_modules + vendor keys, got: %v", keys)
	}
}

// TestVendoredDirTracked_AmbiguousNameNeedsMarker locks in the FP fix: build/,
// dist/, target/, .cache/ double as ordinary content dirs. Without a matching
// build-tool marker the dir is hand-authored source — flagging it would propose
// a destructive `git rm -r --cached`.
func TestVendoredDirTracked_AmbiguousNameNeedsMarker(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		"build/make_release.py": "print('release')\n", // hand-authored, .py (not a web asset)
		"target/notes.md":       "# design target\n",
		"README.md":             "# x\n",
	})
	fs, err := checkVendoredDirTracked(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	// vendored_dir_tracked keys FilePath on the bare dir name, so assert on count.
	if len(fs) != 0 {
		t.Fatalf("unmarked build//target/ must not flag, got: %+v", fs)
	}
}

// With a corroborating marker the ambiguous dir IS rebuildable output → flag.
func TestVendoredDirTracked_AmbiguousNameFlaggedWithMarker(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		"pyproject.toml":   "[build-system]\nrequires = [\"setuptools\"]\n",
		"build/lib/mod.py": "x = 1\n", // setuptools build output, non-asset extension
		"README.md":        "# x\n",
	})
	fs, err := checkVendoredDirTracked(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) == 0 || fs[0].FilePath != "build" {
		t.Fatalf("build/ with a pyproject.toml marker must flag, got: %+v", fs)
	}
}

// One finding per offending top-level dir, even with thousands of nested files.
func TestVendoredDirTracked_DeduplicatesByDir(t *testing.T) {
	files := map[string]string{}
	for i := 0; i < 50; i++ {
		files[filepath.Join("node_modules", "pkg", "f"+strings.Repeat("x", i)+".js")] = "x"
	}
	root := initRepoWithFiles(t, files)
	fs, err := checkVendoredDirTracked(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 {
		t.Errorf("expected exactly 1 finding for the dir, got %d", len(fs))
	}
}

func TestVendoredDirTracked_SilentForGoVendor(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		"go.mod":                    "module example.com/m\ngo 1.22\n",
		"vendor/modules.txt":        "# github.com/pkg/errors v0.9.1\n",
		"vendor/github.com/pkg/errors/errors.go": "package errors",
		"main.go":                   "package main",
	})
	fs, err := checkVendoredDirTracked(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fs {
		if f.FilePath == "vendor" {
			t.Errorf("go vendor/ with modules.txt must be silent, got: %+v", f)
		}
	}
}

// A `vendor` dir under a served static web root (e.g. ui/public/vendor) is
// hand-committed assets, not rebuildable package vendoring — must NOT be flagged
// (untracking would delete served files nothing rebuilds).
func TestVendoredDirTracked_SkipsServedStaticAssets(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		"ui/public/vendor/chart.min.js":       "x",
		"ui/public/vendor/fonts/inter.woff2":  "y",
		"src/static/vendor/lib.js":            "z",
		"vendor/foo/foo.go":                   "package foo", // root vendor still flagged
	})
	fs, err := checkVendoredDirTracked(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]bool{}
	for _, f := range fs {
		keys[f.FilePath] = true
	}
	if keys["ui/public/vendor"] || keys["src/static/vendor"] {
		t.Errorf("served static vendor dirs must NOT be flagged, got: %v", keys)
	}
	if !keys["vendor"] {
		t.Errorf("root vendor/ (no manifest) should still be flagged, got: %v", keys)
	}
}

// A vendor/ of hand-committed web assets (self-hosted fonts/CSS/JS that kill
// third-party egress) is served, not rebuildable — must NOT be flagged, even at
// the repo root (lws/vendor/font-awesome) or under docs/ for GitHub Pages
// (blacklists/docs/vendor/chart.js), neither of which the served-static-root
// list covers. node_modules with .js MUST still be flagged.
func TestVendoredDirTracked_SkipsHandVendoredWebAssets(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		"vendor/font-awesome/all.min.css":        "x",
		"vendor/font-awesome/fa-solid-900.woff2":  "y",
		"docs/vendor/chart.umd.min.js":            "z",
		"src/internal/thing.go":                   "package internal",
		"node_modules/react/index.js":             "m",
	})
	fs, err := checkVendoredDirTracked(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]bool{}
	for _, f := range fs {
		keys[f.FilePath] = true
	}
	if keys["vendor"] {
		t.Errorf("root vendor/ of webfonts must NOT be flagged (hand-vendored web assets), got: %v", keys)
	}
	if keys["docs/vendor"] {
		t.Errorf("docs/vendor of a vendored JS lib must NOT be flagged, got: %v", keys)
	}
	if !keys["node_modules"] {
		t.Errorf("node_modules must STILL be flagged (package-managed, not served), got: %v", keys)
	}
}

func TestVendoredDirTracked_FlagsVendorWithoutModulesTxt(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		"go.mod":            "module example.com/m\ngo 1.22\n",
		"vendor/foo/foo.go": "package foo",
		"main.go":           "package main",
	})
	fs, err := checkVendoredDirTracked(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range fs {
		if f.FilePath == "vendor" {
			found = true
		}
	}
	if !found {
		t.Errorf("vendor/ without modules.txt should still be flagged")
	}
}

func TestIdeArtifactTracked_FlagsArtefacts(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		".vscode/settings.json": "{}",
		".DS_Store":             "x",
		"src/foo.go.swp":        "x",
		"src/main.go":           "package main",
	})
	fs, err := checkIdeArtifactTracked(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	flagged := map[string]bool{}
	for _, f := range fs {
		flagged[f.FilePath] = true
	}
	if !flagged[".vscode/settings.json"] || !flagged[".DS_Store"] || !flagged["src/foo.go.swp"] {
		t.Errorf("expected all 3 artefacts flagged, got: %v", flagged)
	}
	if flagged["src/main.go"] {
		t.Errorf("regular source file must not be flagged: %+v", fs)
	}
}

func TestFilenameQuality_Classifications(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		"file with spaces.md": "x",
		"café.txt":       "x", // non-ASCII (é)
		"plain.go":            "package x",
	})
	fs, err := checkFilenameQuality(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	flaggedKinds := map[string][]string{}
	for _, f := range fs {
		flaggedKinds[f.FilePath] = []string{f.Title}
	}
	if _, ok := flaggedKinds["file with spaces.md"]; !ok {
		t.Errorf("expected spaces flag on 'file with spaces.md', got: %v", flaggedKinds)
	}
	if _, ok := flaggedKinds["café.txt"]; !ok {
		t.Errorf("expected non-ASCII flag, got: %v", flaggedKinds)
	}
	if _, ok := flaggedKinds["plain.go"]; ok {
		t.Errorf("plain.go must not fire: %v", flaggedKinds)
	}
}

func TestNvmrcMissing_TriggersOnlyWithPackageJson(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "package.json"), `{"name":"x"}`)
	fs, err := checkNvmrcMissing(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 || fs[0].Severity != SeverityInfo {
		t.Fatalf("expected 1 info finding, got: %+v", fs)
	}
}

func TestNvmrcMissing_SilentWithoutPackageJson(t *testing.T) {
	fs, err := checkNvmrcMissing(context.Background(), t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 0 {
		t.Errorf("expected silent with no package.json, got: %+v", fs)
	}
}

func TestNvmrcMissing_SilentWithPin(t *testing.T) {
	for _, pin := range []string{".nvmrc", ".node-version"} {
		t.Run(pin, func(t *testing.T) {
			root := t.TempDir()
			mustWrite(t, filepath.Join(root, "package.json"), `{}`)
			mustWrite(t, filepath.Join(root, pin), "20.10.0\n")
			fs, err := checkNvmrcMissing(context.Background(), root, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(fs) != 0 {
				t.Errorf("%s present: must be silent, got: %+v", pin, fs)
			}
		})
	}
}

func TestNvmrcMissing_SilentWithEnginesNode(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "package.json"), `{"engines":{"node":">=20.0.0"}}`)
	fs, err := checkNvmrcMissing(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 0 {
		t.Errorf("engines.node present: must be silent, got: %+v", fs)
	}
}

func TestNvmrcMissing_SilentWithVolta(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "package.json"), `{"volta":{"node":"20.10.0"}}`)
	fs, err := checkNvmrcMissing(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 0 {
		t.Errorf("volta.node present: must be silent, got: %+v", fs)
	}
}

// LFS-awareness: a 10 MiB file declared as filter=lfs in .gitattributes
// must NOT trigger large_file_tracked even though it's huge.
func TestLargeFileTracked_LFSMarkerSkipsFile(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)
	if err := os.WriteFile(filepath.Join(root, ".gitattributes"),
		[]byte("*.psd filter=lfs diff=lfs merge=lfs -text\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "design.psd"),
		make([]byte, 10*1024*1024), 0o644); err != nil {
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
	for _, f := range fs {
		if strings.HasPrefix(f.FilePath, "design.psd") {
			t.Errorf("LFS-managed file must be skipped, got: %+v", f)
		}
	}
}

// Without an LFS pattern the same large file should still fire.
func TestLargeFileTracked_NoLFSStillFires(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)
	if err := os.WriteFile(filepath.Join(root, "design.psd"),
		make([]byte, 10*1024*1024), 0o644); err != nil {
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
	hit := false
	for _, f := range fs {
		if f.FilePath == "design.psd" {
			hit = true
			break
		}
	}
	if !hit {
		t.Errorf("expected design.psd to fire without LFS marker, got: %+v", fs)
	}
}

// Sanity: gitLsFilesWithMode actually returns the mode digits.
func TestGitLsFilesWithMode_ReturnsMode(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{"plain.txt": "hi\n"})
	entries, err := gitLsFilesWithMode(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Mode != "100644" || entries[0].Path != "plain.txt" {
		t.Errorf("unexpected entry: %+v", entries[0])
	}
}


// vendor/ in a Ruby project with Gemfile + vendor/bundle/ is legitimate.
func TestVendoredDirTracked_SilentForRubyBundlerDeployment(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		"Gemfile":                       "source 'https://rubygems.org'\ngem 'sinatra'\n",
		"vendor/bundle/ruby/gems/x.rb":  "# gem\n",
	})
	fs, err := checkVendoredDirTracked(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fs {
		if strings.HasPrefix(f.FilePath, "vendor") {
			t.Errorf("Ruby bundler vendor must be exempt, got: %+v", f)
		}
	}
}
