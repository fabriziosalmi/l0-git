package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ignoredRepo builds a repo, commits `tracked` normally, then force-adds each
// of `forced` past the .gitignore — the two ways a file ends up both tracked
// and ignored.
func ignoredRepo(t *testing.T, gitignore string, tracked, forced map[string]string) string {
	t.Helper()
	all := map[string]string{".gitignore": gitignore}
	for k, v := range tracked {
		all[k] = v
	}
	root := initRepoWithFiles(t, all)
	writeAll(t, root, forced)
	for rel := range forced {
		runGit(t, root, "add", "-f", rel)
	}
	return root
}

func ignoredFindings(t *testing.T, root string) []Finding {
	t.Helper()
	fs, err := checkIgnoredFileTracked(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

// The shape the public-repo sweep found: private keys force-added under a
// directory the .gitignore excludes.
func TestIgnoredFileTracked_ForceAddedUnderIgnoredDir(t *testing.T) {
	root := ignoredRepo(t, "data/\n", map[string]string{"README.md": "x\n"}, map[string]string{
		"data/node-1/ed25519_private_key.pem": "k\n",
		"data/node-2/ed25519_private_key.pem": "k\n",
	})
	fs := ignoredFindings(t, root)
	if len(fs) != 1 {
		t.Fatalf("expected one grouped finding, got %d: %+v", len(fs), fs)
	}
	if fs[0].FilePath != "data/" {
		t.Errorf("group key = %q, want data/", fs[0].FilePath)
	}
	if !strings.Contains(fs[0].Message, "2 tracked files") || !strings.Contains(fs[0].Message, "negation") {
		t.Errorf("message should give the count and both remedies: %s", fs[0].Message)
	}
}

// Committed first, ignored later: the rule does not untrack anything, and the
// file stays in every future commit.
func TestIgnoredFileTracked_CommittedBeforeTheRule(t *testing.T) {
	root := initRepoWithCommit(t, map[string]string{"lxc_autoscale/.env": "SSH_PASSWORD=\n"})
	writeAll(t, root, map[string]string{".gitignore": ".env\n"})
	runGit(t, root, "add", ".gitignore")
	fs := ignoredFindings(t, root)
	if len(fs) != 1 || fs[0].FilePath != "lxc_autoscale/.env" {
		t.Fatalf("expected lxc_autoscale/.env, got %+v", fs)
	}
}

// 260 files in one directory are one statement, not 260 — the count and a few
// examples carry the rest.
func TestIgnoredFileTracked_GroupsByDirectory(t *testing.T) {
	forced := map[string]string{}
	for i := 0; i < 40; i++ {
		forced[filepath.ToSlash(filepath.Join("cache", "c"+strings.Repeat("x", i%5)+string(rune('a'+i%26))+".txt"))] = "x\n"
	}
	forced["reports/cf_sync_1.md"] = "x\n"
	root := ignoredRepo(t, "cache/\nreports/\n", map[string]string{"README.md": "x\n"}, forced)
	fs := ignoredFindings(t, root)
	if len(fs) != 2 {
		t.Fatalf("expected one finding per top-level directory, got %d: %+v", len(fs), fs)
	}
	if !strings.Contains(fs[0].Message, "more)") {
		t.Errorf("a large group should truncate its example list: %s", fs[0].Message)
	}
}

// Files that exist to be force-added, or that another gate already reports.
func TestIgnoredFileTracked_ExemptFiles(t *testing.T) {
	root := ignoredRepo(t, "logs/*\n.env*\nnode_modules/\n", map[string]string{"README.md": "x\n"}, map[string]string{
		"logs/.gitkeep":         "",
		"docker/logs/.keep":     "",
		".env.example":          "A=1 # a\n",
		"node_modules/x/i.js":   "x\n",
		"web/node_modules/y.js": "x\n",
	})
	if fs := ignoredFindings(t, root); len(fs) != 0 {
		t.Errorf("exempt files reported: %+v", fs)
	}
}

// A negation is how an intentional exception is written down, and git then no
// longer considers the file ignored — so it must not be reported.
func TestIgnoredFileTracked_NegationIsRespected(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		".gitignore":      "data/*\n!data/schema.sql\n",
		"data/schema.sql": "create table x();\n",
	})
	if fs := ignoredFindings(t, root); len(fs) != 0 {
		t.Errorf("negated file reported: %+v", fs)
	}
}

// The result must be the same on every machine. .git/info/exclude and the
// user's global excludes file are local configuration; if either counted, a
// clean repository would report findings on one computer and not another.
func TestIgnoredFileTracked_IgnoresLocalAndGlobalExcludes(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		"src/main.go": "package main\n",
		"README.md":   "x\n",
	})
	mustWrite(t, filepath.Join(root, ".git", "info", "exclude"), "src/\n")
	global := filepath.Join(t.TempDir(), "gitignore_global")
	mustWrite(t, global, "README.md\n")
	runGit(t, root, "config", "core.excludesFile", global)
	if fs := ignoredFindings(t, root); len(fs) != 0 {
		t.Errorf("local or global excludes leaked into the result: %+v", fs)
	}
}

func TestIgnoredFileTracked_NestedGitignore(t *testing.T) {
	root := ignoredRepo(t, "", map[string]string{
		"app/.gitignore": "__pycache__/\n*.log\n",
		"app/main.py":    "x\n",
	}, map[string]string{"app/debug.log": "x\n"})
	fs := ignoredFindings(t, root)
	if len(fs) != 1 || fs[0].FilePath != "app/debug.log" {
		t.Fatalf("expected app/debug.log from the nested .gitignore, got %+v", fs)
	}
}

func TestIgnoredFileTracked_CleanRepoIsSilent(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{".gitignore": "*.log\n", "main.go": "x\n"})
	if fs := ignoredFindings(t, root); len(fs) != 0 {
		t.Errorf("clean repository reported: %+v", fs)
	}
	if err := os.WriteFile(filepath.Join(root, "untracked.log"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if fs := ignoredFindings(t, root); len(fs) != 0 {
		t.Errorf("an untracked ignored file is how things should be, not a finding: %+v", fs)
	}
}

// Root-level files can only have been excluded by the root .gitignore, so they
// are one statement. Keyed per file, `test_*.sh` produced a finding per script.
func TestIgnoredFileTracked_RootFilesShareOneFinding(t *testing.T) {
	root := ignoredRepo(t, "test_*.sh\n", map[string]string{"README.md": "x\n"}, map[string]string{
		"test_borders.sh": "x\n", "test_grep.sh": "x\n", "test_yaml.sh": "x\n",
	})
	fs := ignoredFindings(t, root)
	if len(fs) != 1 || fs[0].FilePath != ".gitignore" {
		t.Fatalf("expected one finding keyed .gitignore, got %+v", fs)
	}
	if !strings.Contains(fs[0].Message, "at the repository root") || !strings.Contains(fs[0].Message, "test_grep.sh") {
		t.Errorf("message should place the files at the root and name them: %s", fs[0].Message)
	}
}

// A single root file keeps its own path as the key: more precise, and it is
// what an editor needs to jump to.
func TestIgnoredFileTracked_SingleRootFileKeepsItsPath(t *testing.T) {
	root := ignoredRepo(t, "Cargo.lock\n", map[string]string{"README.md": "x\n"}, map[string]string{"Cargo.lock": "x\n"})
	fs := ignoredFindings(t, root)
	if len(fs) != 1 || fs[0].FilePath != "Cargo.lock" {
		t.Fatalf("expected Cargo.lock, got %+v", fs)
	}
}
