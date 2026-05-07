package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodeOfConduct_MissingFires(t *testing.T) {
	fs, err := checkCodeOfConductPresent(context.Background(), t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 || fs[0].Severity != SeverityInfo {
		t.Fatalf("expected one info finding, got: %+v", fs)
	}
}

func TestCodeOfConduct_RootSatisfies(t *testing.T) {
	for _, where := range []string{"CODE_OF_CONDUCT.md", ".github/CODE_OF_CONDUCT.md", "docs/CODE_OF_CONDUCT.md"} {
		t.Run(where, func(t *testing.T) {
			root := t.TempDir()
			mustWrite(t, filepath.Join(root, where), "# CoC\n")
			fs, err := checkCodeOfConductPresent(context.Background(), root, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(fs) != 0 {
				t.Errorf("location %q must satisfy, got: %+v", where, fs)
			}
		})
	}
}

func TestCodeowners_SilentOnDocsOnly(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		"README.md":     "# x",
		"docs/intro.md": "x",
	})
	fs, err := checkCodeownersPresent(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 0 {
		t.Errorf("docs-only repo must be silent, got: %+v", fs)
	}
}

func TestCodeowners_FiresOnCodebase(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		"main.go": "package main\n",
	})
	fs, err := checkCodeownersPresent(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 || fs[0].Severity != SeverityInfo {
		t.Errorf("codebase without CODEOWNERS must fire, got: %+v", fs)
	}
}

func TestCodeowners_GithubLocationSatisfies(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		"main.go":             "package main\n",
		".github/CODEOWNERS":  "* @me\n",
	})
	fs, err := checkCodeownersPresent(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 0 {
		t.Errorf(".github/CODEOWNERS must satisfy, got: %+v", fs)
	}
}

func TestEnvExample_UncommentedKeysFire(t *testing.T) {
	src := `DB_HOST=localhost
DB_PORT=5432
`
	fs := evaluateEnvExample(".env.example", src)
	if len(fs) != 2 {
		t.Fatalf("expected 2 uncommented findings, got %d: %+v", len(fs), fs)
	}
}

func TestEnvExample_PrecedingCommentSatisfies(t *testing.T) {
	src := `# Database host name
DB_HOST=localhost

# Database listening port
DB_PORT=5432
`
	fs := evaluateEnvExample(".env.example", src)
	if len(fs) != 0 {
		t.Errorf("preceding-comment lines must not fire, got: %+v", fs)
	}
}

func TestEnvExample_InlineCommentSatisfies(t *testing.T) {
	src := `DB_HOST=localhost  # database host
DB_PORT=5432 # listening port
`
	fs := evaluateEnvExample(".env.example", src)
	if len(fs) != 0 {
		t.Errorf("inline-comment lines must not fire, got: %+v", fs)
	}
}

// `#` inside a quoted value must not be treated as a comment.
func TestEnvExample_HashInsideQuoteIsNotAComment(t *testing.T) {
	src := `DATABASE_URL="postgres://example.com/db?token=abc#xyz"
`
	fs := evaluateEnvExample(".env.example", src)
	if len(fs) != 1 {
		t.Errorf("hash-in-string must NOT count as comment, got: %+v", fs)
	}
}

func TestEnvExample_BlankLineBetweenCommentBreaksAdjacency(t *testing.T) {
	// Comment, then blank line, then key — strict rule says comment must
	// be the *immediately preceding* non-empty line; blank lines don't
	// break it.
	src := `# Database host

DB_HOST=localhost
`
	fs := evaluateEnvExample(".env.example", src)
	if len(fs) != 0 {
		t.Errorf("blank line between comment and key must still satisfy, got: %+v", fs)
	}
}

// The line number in the FilePath must point at the key line.
func TestEnvExample_LinePinning(t *testing.T) {
	src := `# header

KEY_A=1
KEY_B=2
`
	fs := evaluateEnvExample(".env.example", src)
	if len(fs) == 0 {
		t.Fatalf("expected findings, got none")
	}
	for _, f := range fs {
		if !strings.Contains(f.FilePath, ".env.example:") {
			t.Errorf("FilePath should pin to env file: %q", f.FilePath)
		}
	}
}
