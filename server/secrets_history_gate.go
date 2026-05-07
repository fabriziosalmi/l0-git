package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// secretsHistoryOptions plumbs the standard scanOptions plus an explicit
// opt-in. History scanning is opt-in because:
//   - it walks every blob reachable from any ref (slow on big repos)
//   - it's a different remediation story (`git filter-repo`) so users
//     have to actively choose to surface those findings
type secretsHistoryOptions struct {
	scanOptions
	Enabled        bool `json:"enabled,omitempty"`
	MaxBlobs       int  `json:"max_blobs,omitempty"`
	MaxBlobSizeMB  int  `json:"max_blob_size_mb,omitempty"`
}

func checkSecretsScanHistory(ctx context.Context, root string, opts json.RawMessage) ([]Finding, error) {
	options := parseSecretsHistoryOptions(opts)
	if !options.Enabled {
		// Default off: walking history on every check would be noisy and
		// expensive. Users opt in via gate_options.secrets_scan_history.enabled.
		return nil, nil
	}
	if !isGitRepo(root) {
		return []Finding{{
			Severity: SeverityInfo,
			Title:    "secrets_scan_history skipped (not a git repository)",
			Message:  "Project root has no .git/. History scanning needs a git repo.",
			FilePath: ".git",
		}}, nil
	}

	maxBlobs := options.MaxBlobs
	if maxBlobs <= 0 {
		maxBlobs = 5000
	}
	maxSize := int64(options.MaxBlobSizeMB) * 1024 * 1024
	if maxSize <= 0 {
		maxSize = secretsMaxFileSize // reuse 2 MiB cap from working-tree gate
	}

	blobs, err := enumerateHistoryBlobs(ctx, root)
	if err != nil {
		return []Finding{{
			Severity: SeverityWarning,
			Title:    "secrets_scan_history failed",
			Message:  fmt.Sprintf("Could not enumerate history blobs: %v", err),
			FilePath: ".git",
		}}, nil
	}

	out := []Finding{}
	scanned := 0
	for _, b := range blobs {
		if scanned >= maxBlobs {
			break
		}
		if pathExcluded(b.Path, options.ExcludePaths) {
			continue
		}
		if b.Size > maxSize {
			continue
		}
		// Skip the trivial cases that working-tree secrets_scan also skips:
		// an empty blob can't contain a secret.
		if b.Size == 0 {
			continue
		}
		data, err := readBlob(ctx, root, b.Hash)
		if err != nil {
			continue
		}
		if isBinary(data) {
			continue
		}
		scanned++
		out = append(out, scanHistoryBlob(b, data)...)
	}
	return out, nil
}

// scanHistoryBlob runs the same regex pattern set as the working-tree
// gate, but mints findings keyed by blob hash so duplicates across
// commits collapse to a single row in the Problems pane.
func scanHistoryBlob(b blobInfo, data []byte) []Finding {
	out := []Finding{}
	line := 1
	start := 0
	emit := func(content []byte, lineNum int) {
		for _, p := range secretPatterns {
			if p.re.Match(content) {
				out = append(out, Finding{
					Severity: SeverityWarning, // never error — already in history, requires filter-repo
					Title:    p.title + " in git history",
					Message: fmt.Sprintf(
						"Possible %s in blob %s (path %s, line %d). The secret is in repo history even if removed from the working tree — rotate the credential, then run `git filter-repo --invert-paths --path %s` (or BFG) to scrub it.",
						p.title, shortHash(b.Hash), b.Path, lineNum, b.Path,
					),
					// FilePath embeds the blob hash so each unique blob × line
					// × pattern is its own finding (and survives upserts).
					FilePath: fmt.Sprintf("history:%s:%d:%s", shortHash(b.Hash), lineNum, p.id),
				})
			}
		}
	}
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' {
			emit(data[start:i], line)
			line++
			start = i + 1
		}
	}
	if start < len(data) {
		emit(data[start:], line)
	}
	return out
}

// pathExcluded already exists in scan_options.go; this is a defensive
// stub in case the helper moves.
var _ = filepath.Match // keep filepath import alive if pathExcluded migrates

func parseSecretsHistoryOptions(opts json.RawMessage) secretsHistoryOptions {
	if len(opts) == 0 {
		return secretsHistoryOptions{}
	}
	var o secretsHistoryOptions
	_ = json.Unmarshal(opts, &o)
	return o
}

// _ keeps strings import busy if future iterations need it.
var _ = strings.TrimSpace
