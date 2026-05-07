package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitignoreCoverage_NodeProjectMissingNodeModules(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "package.json"), `{}`)
	mustWrite(t, filepath.Join(root, ".gitignore"), "*.log\n")
	fs, err := checkGitignoreCoverage(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	hit := false
	for _, f := range fs {
		if strings.Contains(f.FilePath, "node_modules") {
			hit = true
		}
	}
	if !hit {
		t.Fatalf("expected node_modules missing finding, got: %+v", fs)
	}
}

func TestGitignoreCoverage_NodeProjectWithNodeModulesIgnoredOK(t *testing.T) {
	for _, line := range []string{"node_modules", "node_modules/", "/node_modules", "/node_modules/"} {
		t.Run(line, func(t *testing.T) {
			root := t.TempDir()
			mustWrite(t, filepath.Join(root, "package.json"), `{}`)
			mustWrite(t, filepath.Join(root, ".gitignore"), line+"\n")
			fs, err := checkGitignoreCoverage(context.Background(), root, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range fs {
				if strings.Contains(f.FilePath, "node_modules") {
					t.Errorf("%q must satisfy node_modules coverage, got: %+v", line, f)
				}
			}
		})
	}
}

func TestGitignoreCoverage_RustProject(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "Cargo.toml"), "[package]\nname='x'\nversion='0.1.0'\n")
	mustWrite(t, filepath.Join(root, ".gitignore"), "*.log\n")
	fs, err := checkGitignoreCoverage(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	hit := false
	for _, f := range fs {
		if strings.Contains(f.FilePath, "target") {
			hit = true
		}
	}
	if !hit {
		t.Fatalf("expected target/ missing for Cargo project: %+v", fs)
	}
}

func TestGitignoreCoverage_PythonProject(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "pyproject.toml"), "[project]\nname='x'\n")
	mustWrite(t, filepath.Join(root, ".gitignore"), "*.log\n")
	fs, err := checkGitignoreCoverage(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	missing := map[string]bool{}
	for _, f := range fs {
		missing[f.FilePath] = true
	}
	hasPycache := false
	hasVenv := false
	for k := range missing {
		if strings.Contains(k, "__pycache__") {
			hasPycache = true
		}
		if strings.Contains(k, ".venv") {
			hasVenv = true
		}
	}
	if !hasPycache || !hasVenv {
		t.Fatalf("expected both __pycache__ and .venv missing, got: %v", missing)
	}
}

// No stack markers → silent (no false positives on empty/exotic repos).
func TestGitignoreCoverage_NoStackSilent(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".gitignore"), "")
	fs, err := checkGitignoreCoverage(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 0 {
		t.Errorf("no stack markers → must be silent, got: %+v", fs)
	}
}

// disabled_patterns silences specific entries while keeping the others.
func TestGitignoreCoverage_DisabledPatterns(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "package.json"), `{}`)
	mustWrite(t, filepath.Join(root, ".gitignore"), "")
	// Disable the universal .DS_Store check, keep node_modules.
	opts := []byte(`{"disabled_patterns": [".DS_Store"]}`)
	fs, err := checkGitignoreCoverage(context.Background(), root, opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fs {
		if strings.Contains(f.FilePath, ".DS_Store") {
			t.Errorf("disabled .DS_Store must not appear: %+v", f)
		}
	}
}

// .gitignore comments and negation lines are ignored.
func TestGitignoreCoverage_CommentsAndNegationsSkipped(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "package.json"), `{}`)
	mustWrite(t, filepath.Join(root, ".gitignore"),
		"# managed by us\nnode_modules\n!node_modules/important\n")
	fs, err := checkGitignoreCoverage(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fs {
		if strings.Contains(f.FilePath, "node_modules") {
			t.Errorf("node_modules covered by line 2; must be silent: %+v", f)
		}
	}
}
