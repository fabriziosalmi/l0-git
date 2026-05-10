package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ProjectConfig is the optional .l0git.json contract a project can drop at
// its root to opt out of specific gates or dial down their severity. Unknown
// fields are reported (not silently dropped) so typos don't lead to "why
// isn't my override applied?" debugging sessions.
type ProjectConfig struct {
	// Ignore lists gate IDs that should be skipped entirely for this
	// project. The gates_run array in the response will not include them.
	Ignore []string `json:"ignore,omitempty"`
	// Severity overrides the default (and any per-finding) severity for
	// listed gate IDs. Values must be one of "error", "warning", "info".
	Severity map[string]string `json:"severity,omitempty"`
	// GateOptions hands a JSON sub-tree to each gate's Check function.
	// The schema is gate-specific; see each gate's docstring.
	GateOptions map[string]json.RawMessage `json:"gate_options,omitempty"`
	// ExcludePaths is a list of glob patterns (filepath.Match semantics)
	// applied to every content-scanning gate before per-gate exclude_paths.
	// Use this to exclude generated code, vendored snapshots, or build
	// artefacts from all gates in one place instead of repeating the list
	// under every gate_options entry.
	//
	// Example:
	//   "exclude_paths": ["**/generated/**", "**/testdata/**"]
	ExcludePaths []string `json:"exclude_paths,omitempty"`
}

const projectConfigFilename = ".l0git.json"

// ErrNoConfig signals that the project simply has no .l0git.json. Callers
// treat this as "use defaults", not as a real error.
var ErrNoConfig = errors.New("no .l0git.json")

func loadProjectConfig(root string) (*ProjectConfig, error) {
	path := filepath.Join(root, projectConfigFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoConfig
		}
		return nil, fmt.Errorf("read %s: %w", projectConfigFilename, err)
	}
	cfg := &ProjectConfig{}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", projectConfigFilename, err)
	}
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", projectConfigFilename, err)
	}
	return cfg, nil
}

func (c *ProjectConfig) validate() error {
	for k, v := range c.Severity {
		switch v {
		case SeverityError, SeverityWarning, SeverityInfo:
		default:
			return fmt.Errorf("severity for %q must be error|warning|info (got %q)", k, v)
		}
	}
	return nil
}

func (c *ProjectConfig) ignored(gateID string) bool {
	if c == nil {
		return false
	}
	for _, id := range c.Ignore {
		if id == gateID {
			return true
		}
	}
	return false
}

func (c *ProjectConfig) severityFor(gateID, fallback string) string {
	if c == nil {
		return fallback
	}
	if s, ok := c.Severity[gateID]; ok {
		return s
	}
	return fallback
}

// severityOverride returns the configured severity (and ok=true) when the
// project explicitly set one for gateID; otherwise ok=false. Used by the
// runner to distinguish "user wants this severity" from "use the default".
func (c *ProjectConfig) severityOverride(gateID string) (string, bool) {
	if c == nil {
		return "", false
	}
	s, ok := c.Severity[gateID]
	return s, ok
}

// optionsFor returns the gate-specific JSON sub-tree from gate_options merged
// with any top-level exclude_paths. The global excludes are prepended to the
// gate's own exclude_paths list so a single project-level pattern suppresses a
// path across all content-scanning gates without repeating it everywhere.
func (c *ProjectConfig) optionsFor(gateID string) json.RawMessage {
	if c == nil {
		return nil
	}
	raw := c.GateOptions[gateID]
	if len(c.ExcludePaths) == 0 {
		return raw
	}
	// Parse the gate's options JSON into a generic map so we can inject the
	// global exclude_paths without knowing the gate-specific schema.
	m := map[string]json.RawMessage{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &m) // best-effort; non-object gates are rare
	}
	// Merge: global first, gate-specific appended so per-gate patterns
	// always win (they're evaluated last, but filepath.Match semantics
	// means first-match wins — global patterns therefore take precedence,
	// which is correct: a global exclusion can't be overridden by a gate).
	var gateExcludes []string
	if existing, ok := m["exclude_paths"]; ok {
		_ = json.Unmarshal(existing, &gateExcludes)
	}
	merged := append(c.ExcludePaths, gateExcludes...) //nolint:gocritic // intentional prepend
	b, _ := json.Marshal(merged)
	m["exclude_paths"] = b
	out, _ := json.Marshal(m)
	return out
}

