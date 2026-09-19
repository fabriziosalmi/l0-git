package main

import (
	"context"
	"path/filepath"
	"testing"
)

// Findings are stored under filepath.Abs(root) by RunChecks. Every reader must
// accept the other spellings of the same directory, or it answers "0" for a
// project that has findings.
func TestStore_ProjectFilterAcceptsEquivalentSpellings(t *testing.T) {
	root := t.TempDir()
	sep := string(filepath.Separator)
	stored, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}

	spellings := map[string]string{
		"canonical":      stored,
		"trailing slash": stored + sep,
		"dot-dot":        stored + sep + "sub" + sep + "..",
		"dot":            stored + sep + ".",
	}
	for name, project := range spellings {
		t.Run(name, func(t *testing.T) {
			s := newTestStore(t)
			ctx := context.Background()
			seed(t, s, stored, "a")
			seed(t, s, stored, "b")
			// A sibling whose path shares the prefix must never match.
			seed(t, s, stored+"-other", "c")

			got, err := s.List(ctx, FindingFilter{Project: project})
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 2 {
				t.Errorf("List(%q) = %d findings, want 2", project, len(got))
			}

			st, err := s.Stats(ctx, project)
			if err != nil {
				t.Fatal(err)
			}
			if st.Total != 2 {
				t.Errorf("Stats(%q).Total = %d, want 2", project, st.Total)
			}

			n, err := s.ClearProject(ctx, project)
			if err != nil {
				t.Fatal(err)
			}
			if n != 2 {
				t.Errorf("ClearProject(%q) removed %d, want 2", project, n)
			}
			left, _ := s.List(ctx, FindingFilter{})
			if len(left) != 1 || left[0].Project != stored+"-other" {
				t.Errorf("after clear, left = %+v, want only the sibling project", left)
			}
		})
	}
}

func TestStore_ProjectFilterRelativePath(t *testing.T) {
	parent := t.TempDir()
	t.Chdir(parent)
	stored := filepath.Join(parent, "repo")

	s := newTestStore(t)
	seed(t, s, stored, "a")

	got, err := s.List(context.Background(), FindingFilter{Project: "repo"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("List(\"repo\") from %s = %d findings, want 1", parent, len(got))
	}
}

// Empty still means every project, not "the project named after the cwd".
func TestStore_EmptyProjectFilterMeansAll(t *testing.T) {
	s := newTestStore(t)
	seed(t, s, "/p1", "a")
	seed(t, s, "/p2", "b")

	st, err := s.Stats(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if st.Total != 2 || st.Project != "" {
		t.Errorf("Stats(\"\") = total %d project %q, want 2 and \"\"", st.Total, st.Project)
	}
}

func seed(t *testing.T, s *Store, project, file string) {
	t.Helper()
	if _, err := s.Upsert(context.Background(), Finding{
		Project: project, GateID: "g", Severity: SeverityWarning,
		Title: "T", Message: "m", FilePath: file,
	}); err != nil {
		t.Fatal(err)
	}
}
