package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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

// validateGateOptions strictly decodes every `gate_options` sub-tree against
// the gate that owns it, and returns one human-readable problem per offence.
//
// This is the check that was missing. Each gate parses its own options with
// `_ = json.Unmarshal(opts, &o)`, so a decode failure — a mistyped key, a
// string where a number belongs, an object where a list belongs — was thrown
// away and the gate silently ran on defaults. `"threshold_mb": "20"` left the
// threshold at 5, `"exclude_path"` excluded nothing, and neither produced an
// error, a warning, or a non-zero exit. The config file did not do what it
// said and there was no way to notice.
//
// Validation runs against what the user actually wrote, not optionsFor()'s
// output, which injects the merged top-level exclude_paths.
//
// Problems are reported, never fatal: RunChecks folds them into
// CheckResult.ConfigError, which is the channel the top-level config error
// already uses, for the reason recorded there — a bad config should not take
// the whole run with it.
func validateGateOptions(c *ProjectConfig, gates []Gate) (problems []string, rejected map[string]bool) {
	rejected = map[string]bool{}
	if c == nil || len(c.GateOptions) == 0 {
		return nil, rejected
	}
	byID := make(map[string]Gate, len(gates))
	for _, g := range gates {
		byID[g.ID] = g
	}
	// Deterministic order: the caller renders this into a single string, and
	// map iteration would reshuffle it between otherwise identical runs.
	ids := make([]string, 0, len(c.GateOptions))
	for id := range c.GateOptions {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		raw := c.GateOptions[id]
		g, ok := byID[id]
		if !ok {
			problems = append(problems, fmt.Sprintf(
				"gate_options.%s: no gate with that id (run `lgit gates` for the list)", id))
			continue
		}
		if g.NewOptions == nil {
			problems = append(problems, fmt.Sprintf(
				"gate_options.%s: this gate takes no options", id))
			continue
		}
		// A JSON null decodes into any pointer without error, so it would slip
		// past the strict decode below and leave the gate on defaults with
		// nothing said — the exact silence this function exists to end.
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
			problems = append(problems, fmt.Sprintf(
				"gate_options.%s: must be an object, got null", id))
			rejected[id] = true
			continue
		}
		dec := json.NewDecoder(bytes.NewReader(trimmed))
		dec.DisallowUnknownFields()
		if err := dec.Decode(g.NewOptions()); err != nil {
			problems = append(problems, fmt.Sprintf("gate_options.%s: %v", id, err))
			rejected[id] = true
		}
	}
	return problems, rejected
}

// optionsFor returns the gate-specific JSON sub-tree from gate_options merged
// with any top-level exclude_paths. The global excludes are prepended to the
// gate's own exclude_paths list so a single project-level pattern suppresses a
// path across all content-scanning gates without repeating it everywhere.
func (c *ProjectConfig) optionsFor(gateID string) json.RawMessage {
	if c == nil {
		return nil
	}
	return c.withGlobalExcludes(c.GateOptions[gateID])
}

// optionsWithoutGateEntry is optionsFor with the gate's own sub-tree dropped,
// for a gate whose options failed validation. The project-level exclude_paths
// still apply: they parsed fine and have nothing to do with the gate's mistake.
//
// Dropping the whole sub-tree rather than letting the gate's lenient parser
// salvage what it can is deliberate. `{"threshold_mb": 20, "treshold_mb": 1}`
// used to warn and then apply 20 anyway — a config half-obeyed is the same
// class of problem as one silently ignored, and it is impossible to document
// in a sentence a reader will remember.
func (c *ProjectConfig) optionsWithoutGateEntry() json.RawMessage {
	if c == nil {
		return nil
	}
	return c.withGlobalExcludes(nil)
}

func (c *ProjectConfig) withGlobalExcludes(raw json.RawMessage) json.RawMessage {
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
