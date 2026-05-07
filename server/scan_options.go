package main

import (
	"encoding/json"
	"path/filepath"
)

// scanOptions is the shared shape every git-tracked-file scanner consumes
// from its gate_options sub-tree. Extending the struct here keeps
// future per-gate knobs in one place.
type scanOptions struct {
	// ExcludePaths is a list of glob patterns (filepath.Match semantics)
	// matched against the relative-from-root file path. A file matching
	// any pattern is skipped before its content is read.
	ExcludePaths []string `json:"exclude_paths,omitempty"`
}

func parseScanOptions(opts json.RawMessage) scanOptions {
	if len(opts) == 0 {
		return scanOptions{}
	}
	var s scanOptions
	_ = json.Unmarshal(opts, &s) // best-effort; bad shape is treated as no-op
	return s
}

// pathExcluded returns true when rel matches any of the patterns. Match
// errors (bad glob) are ignored — patterns silently miss rather than
// fail the entire run.
func pathExcluded(rel string, patterns []string) bool {
	for _, p := range patterns {
		if ok, _ := filepath.Match(p, rel); ok {
			return true
		}
	}
	return false
}
