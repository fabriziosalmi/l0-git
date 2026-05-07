package main

import (
	"context"
	"encoding/json"
	"fmt"
)

// largeBlobHistoryOptions: opt-in plus a configurable threshold mirroring
// the working-tree large_file_tracked gate. Default 5 MiB.
type largeBlobHistoryOptions struct {
	scanOptions
	Enabled     bool `json:"enabled,omitempty"`
	ThresholdMB int  `json:"threshold_mb,omitempty"`
}

// checkLargeBlobInHistory finds blobs that bloat .git even after they're
// removed from the working tree. The remediation is `git filter-repo`
// (or BFG); this gate's job is to surface what the cleanup would target.
func checkLargeBlobInHistory(ctx context.Context, root string, opts json.RawMessage) ([]Finding, error) {
	options := parseLargeBlobHistoryOptions(opts)
	if !options.Enabled {
		return nil, nil
	}
	if !isGitRepo(root) {
		return []Finding{{
			Severity: SeverityInfo,
			Title:    "large_blob_in_history skipped (not a git repository)",
			Message:  "Project root has no .git/.",
			FilePath: ".git",
		}}, nil
	}

	thresholdMB := options.ThresholdMB
	if thresholdMB <= 0 {
		thresholdMB = 5
	}
	threshold := int64(thresholdMB) * 1024 * 1024

	blobs, err := enumerateHistoryBlobs(ctx, root)
	if err != nil {
		return []Finding{{
			Severity: SeverityWarning,
			Title:    "large_blob_in_history failed",
			Message:  fmt.Sprintf("Could not enumerate history blobs: %v", err),
			FilePath: ".git",
		}}, nil
	}

	out := []Finding{}
	for _, b := range blobs {
		if options.shouldSkip(b.Path) {
			continue
		}
		if b.Size <= threshold {
			continue
		}
		out = append(out, Finding{
			Severity: SeverityWarning,
			Title:    "Large blob in git history",
			Message: fmt.Sprintf(
				"Blob %s (path %s, %s, threshold %d MiB) lives in .git even if it's no longer in the working tree. Big blobs in history bloat clones — purge with `git filter-repo --strip-blobs-bigger-than %dM` if it shouldn't have been committed.",
				shortHash(b.Hash), b.Path, humanSize(b.Size), thresholdMB, thresholdMB,
			),
			// One finding per blob hash (a blob shared across many
			// commits dedupes naturally via the unique constraint).
			FilePath: fmt.Sprintf("history:%s:0:large_blob", shortHash(b.Hash)),
		})
	}
	return out, nil
}

func parseLargeBlobHistoryOptions(opts json.RawMessage) largeBlobHistoryOptions {
	if len(opts) == 0 {
		return largeBlobHistoryOptions{}
	}
	var o largeBlobHistoryOptions
	_ = json.Unmarshal(opts, &o)
	return o
}
