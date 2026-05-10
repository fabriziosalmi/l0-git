package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeadPlaceholders_AllPatterns(t *testing.T) {
	cases := []struct {
		name      string
		content   string
		patternID string
	}{
		{"todo", "// TODO: refactor this", "todo"},
		{"todo_lower", "# todo: come back", "todo"},
		{"fixme", "/* FIXME: edge case ignored */", "fixme"},
		{"xxx", "// XXX: untested", "xxx"},
		{"hack", "# HACK: see issue 42", "hack"},
		{"update_later", "Will update this later, promise.", "update_later"},
		{"lorem_ipsum", "Lorem ipsum dolor sit amet.", "lorem_ipsum"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := initRepoWithFiles(t, map[string]string{"file.md": tc.content + "\n"})
			fs, err := checkDeadPlaceholders(context.Background(), root, nil)
			if err != nil {
				t.Fatal(err)
			}
			hit := false
			for _, f := range fs {
				if strings.HasSuffix(f.FilePath, ":"+tc.patternID) {
					hit = true
				}
			}
			if !hit {
				t.Fatalf("pattern %q not detected; findings: %+v", tc.patternID, fs)
			}
		})
	}
}

// Strict word-boundary + colon: "todoist" or "fixture" must not fire.
func TestDeadPlaceholders_NoFalsePositiveOnSubstrings(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		"main.go": "package todoist\nfunc fixture() {}\n",
	})
	fs, err := checkDeadPlaceholders(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fs {
		if strings.HasPrefix(f.FilePath, "main.go") {
			t.Errorf("substring match leaked: %+v", f)
		}
	}
}

func TestDeadPlaceholders_BinaryAndOversizeSkipped(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		"blob.bin": "TODO: hide this\x00more",
	})
	fs, err := checkDeadPlaceholders(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fs {
		if strings.HasPrefix(f.FilePath, "blob.bin") {
			t.Errorf("binary file was scanned: %+v", f)
		}
	}
}

// disabled_patterns silences specific markers project-wide.
func TestDeadPlaceholders_DisabledPatterns(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		"a.md": "TODO: unfinished\nFIXME: known\n",
	})
	opts := []byte(`{"disabled_patterns": ["todo"]}`)
	fs, err := checkDeadPlaceholders(context.Background(), root, opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fs {
		if strings.HasSuffix(f.FilePath, ":todo") {
			t.Errorf("disabled todo pattern must not appear: %+v", f)
		}
	}
	// FIXME still fires.
	hit := false
	for _, f := range fs {
		if strings.HasSuffix(f.FilePath, ":fixme") {
			hit = true
		}
	}
	if !hit {
		t.Errorf("expected fixme finding: %+v", fs)
	}
}

func TestDeadPlaceholders_LinePinning(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		"file.md": "# Header\n\nSomething.\n\n// FIXME: bug\n",
	})
	fs, err := checkDeadPlaceholders(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fs {
		if strings.HasSuffix(f.FilePath, ":fixme") {
			if !strings.Contains(f.FilePath, ":5:") {
				t.Errorf("expected line 5, got %q", f.FilePath)
			}
			return
		}
	}
	t.Fatalf("expected fixme finding")
}

// Files whose basename is a placeholder-registry (TODO.md, FIXME.md, …)
// must be skipped — they ARE the register, not a file with unwanted markers.
func TestDeadPlaceholders_RegistryFilesSkipped(t *testing.T) {
	for _, name := range []string{"TODO.md", "FIXME.md", "TODO.txt", "TODO"} {
		t.Run(name, func(t *testing.T) {
			root := initRepoWithFiles(t, map[string]string{
				name: "TODO: this is a planned feature\nFIXME: known bug\n",
			})
			fs, err := checkDeadPlaceholders(context.Background(), root, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range fs {
				base := filepath.Base(f.FilePath[:strings.Index(f.FilePath, ":")])
				if strings.EqualFold(base, name) {
					t.Errorf("%s must be skipped, got: %+v", name, f)
				}
			}
		})
	}
}
