package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProjectConfig_Missing(t *testing.T) {
	cfg, err := loadProjectConfig(t.TempDir())
	if !errors.Is(err, ErrNoConfig) {
		t.Fatalf("got err=%v, want ErrNoConfig", err)
	}
	if cfg != nil {
		t.Fatalf("got cfg=%+v, want nil", cfg)
	}
}

func TestLoadProjectConfig_Valid(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, projectConfigFilename),
		`{"ignore":["readme_present","secrets_scan"],"severity":{"changelog_present":"error"}}`)
	cfg, err := loadProjectConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ignored("readme_present") || !cfg.ignored("secrets_scan") {
		t.Errorf("expected ignore set to include both ids: %+v", cfg.Ignore)
	}
	if cfg.ignored("license_present") {
		t.Errorf("license_present should not be ignored")
	}
	if got := cfg.severityFor("changelog_present", SeverityInfo); got != SeverityError {
		t.Errorf("severity override: got %q, want error", got)
	}
	if got := cfg.severityFor("readme_present", SeverityWarning); got != SeverityWarning {
		t.Errorf("fallback severity: got %q, want warning", got)
	}
}

func TestLoadProjectConfig_BadSeverity(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, projectConfigFilename),
		`{"severity":{"x":"critical"}}`)
	_, err := loadProjectConfig(root)
	if err == nil {
		t.Fatal("expected an error for unknown severity value")
	}
}

func TestLoadProjectConfig_UnknownField(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, projectConfigFilename),
		`{"ignore":[],"new_field":true}`)
	_, err := loadProjectConfig(root)
	if err == nil {
		t.Fatal("expected an error for unknown field (DisallowUnknownFields)")
	}
}

func TestLoadProjectConfig_Malformed(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, projectConfigFilename), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := loadProjectConfig(root)
	if err == nil {
		t.Fatal("expected an error parsing malformed JSON")
	}
}

func TestProjectConfig_GlobalExcludePaths_InjectedIntoGateOptions(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, projectConfigFilename), `{
		"exclude_paths": ["**/generated/**", "vendor/**"],
		"gate_options": {
			"secrets_scan": {"exclude_paths": ["**/fixtures/**"]}
		}
	}`)
	cfg, err := loadProjectConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	// secrets_scan has its own exclude_paths — global must be prepended.
	opts := parseScanOptions(cfg.optionsFor("secrets_scan"))
	want := []string{"**/generated/**", "vendor/**", "**/fixtures/**"}
	if len(opts.ExcludePaths) != len(want) {
		t.Fatalf("secrets_scan ExcludePaths: got %v, want %v", opts.ExcludePaths, want)
	}
	for i, p := range want {
		if opts.ExcludePaths[i] != p {
			t.Errorf("ExcludePaths[%d]: got %q, want %q", i, opts.ExcludePaths[i], p)
		}
	}

	// conn_gate has no gate_options entry — only global patterns should appear.
	opts2 := parseScanOptions(cfg.optionsFor("connection_strings"))
	if len(opts2.ExcludePaths) != 2 {
		t.Fatalf("connection_strings ExcludePaths: got %v, want 2 global entries", opts2.ExcludePaths)
	}
}

func TestProjectConfig_GlobalExcludePaths_NoGateOptions(t *testing.T) {
	cfg := &ProjectConfig{ExcludePaths: []string{"dist/**"}}
	opts := parseScanOptions(cfg.optionsFor("secrets_scan"))
	if len(opts.ExcludePaths) != 1 || opts.ExcludePaths[0] != "dist/**" {
		t.Errorf("expected [dist/**], got %v", opts.ExcludePaths)
	}
}

func TestProjectConfig_NilSafe(t *testing.T) {
	var cfg *ProjectConfig
	if cfg.ignored("anything") {
		t.Errorf("nil config must report nothing ignored")
	}
	if got := cfg.severityFor("g", SeverityWarning); got != SeverityWarning {
		t.Errorf("nil config severity should fall through, got %q", got)
	}
}
