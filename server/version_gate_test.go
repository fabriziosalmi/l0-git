package main

import (
	"context"
	"path/filepath"
	"testing"
)

// Single manifest → no drift, regardless of value.
func TestVersionDrift_SingleManifestNoFinding(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "package.json"), `{"name":"x","version":"1.2.3"}`)
	fs, err := checkVersionDrift(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 0 {
		t.Errorf("expected no findings with one manifest, got: %+v", fs)
	}
}

// Two manifests, same version → no drift.
func TestVersionDrift_AgreeingManifestsNoFinding(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "package.json"), `{"version":"1.2.3"}`)
	mustWrite(t, filepath.Join(root, "VERSION"), "1.2.3\n")
	fs, err := checkVersionDrift(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 0 {
		t.Errorf("expected no findings when manifests agree, got: %+v", fs)
	}
}

// Two manifests disagree → exactly one finding pinned to the *non*-leader.
func TestVersionDrift_TwoWayDisagreement(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "package.json"), `{"version":"1.2.3"}`)
	mustWrite(t, filepath.Join(root, "VERSION"), "1.2.4\n")
	fs, err := checkVersionDrift(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 {
		t.Fatalf("expected 1 drift finding, got: %+v", fs)
	}
	// Leader is alphabetically first → "VERSION" sorts before "package.json"
	// (uppercase precedes lowercase). So the finding should pin package.json.
	if fs[0].FilePath != "package.json" {
		t.Errorf("finding should target package.json, got: %+v", fs[0])
	}
}

// Three manifests, two of which agree, one drifts.
func TestVersionDrift_ThreeWaySingleOutlier(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "package.json"), `{"version":"2.0.0"}`)
	mustWrite(t, filepath.Join(root, "Cargo.toml"), `[package]
name = "x"
version = "2.0.0"
`)
	mustWrite(t, filepath.Join(root, "pyproject.toml"), `[project]
name = "x"
version = "1.9.9"
`)
	fs, err := checkVersionDrift(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	// At least one finding; the outlier (pyproject.toml) must show up.
	pinned := map[string]bool{}
	for _, f := range fs {
		pinned[f.FilePath] = true
	}
	if !pinned["pyproject.toml"] {
		t.Errorf("expected pyproject.toml to be flagged, got: %+v", fs)
	}
}

// Garbage-shape values (template placeholders, dev markers) should be
// rejected by versionShape — no finding even if they "differ" from a
// real version.
func TestVersionDrift_RejectsNonShape(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "package.json"), `{"version":"{{VERSION}}"}`)
	mustWrite(t, filepath.Join(root, "VERSION"), "1.0.0\n")
	fs, err := checkVersionDrift(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 0 {
		t.Errorf("template placeholder should be ignored, got: %+v", fs)
	}
}

// pyproject.toml [tool.poetry] section is also recognised.
func TestVersionDrift_PoetryFlavour(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "pyproject.toml"), `[tool.poetry]
name = "x"
version = "0.5.0"
`)
	mustWrite(t, filepath.Join(root, "VERSION"), "0.5.1\n")
	fs, err := checkVersionDrift(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 {
		t.Errorf("expected 1 finding, got: %+v", fs)
	}
}

// pom.xml: pick first <version>, ignore later parent / dependency versions.
func TestVersionDrift_PomFirstVersion(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "pom.xml"), `<project>
  <version>3.1.4</version>
  <parent><version>1.0.0</version></parent>
</project>
`)
	mustWrite(t, filepath.Join(root, "VERSION"), "3.1.4\n")
	fs, err := checkVersionDrift(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 0 {
		t.Errorf("pom + VERSION agree on 3.1.4, expected no finding, got: %+v", fs)
	}
}
