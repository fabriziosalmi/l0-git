package main

import (
	"bytes"
	"context"
	"os/exec"
	"strconv"
	"strings"
)

// blobInfo describes a single git blob discovered by walking every ref's
// history. Path is the first path the blob was seen under via
// `git rev-list --objects` (a blob can be in many commits / paths; we
// surface one for the user, the rest are reachable via `git log
// --find-object=<hash>`).
type blobInfo struct {
	Hash string
	Path string
	Size int64
}

// enumerateHistoryBlobs returns every blob reachable from any ref. It's
// the workhorse for the history-aware gates. Two git invocations:
//
//  1. `git rev-list --all --objects` — pairs of (sha, path), with path
//     populated for blobs and trees.
//  2. `git cat-file --batch-check` over the candidate shas, returning
//     (objecttype, sha, size); we filter to objecttype == "blob".
//
// The function dedupes blobs by hash; if you want every (commit, path)
// occurrence, walk `git log --find-object=<hash>` per blob.
func enumerateHistoryBlobs(ctx context.Context, root string) ([]blobInfo, error) {
	revCmd := exec.CommandContext(ctx, "git", "-C", root, "rev-list", "--all", "--objects")
	revOut, err := revCmd.Output()
	if err != nil {
		return nil, err
	}

	pathBySha := map[string]string{}
	hashes := make([]string, 0, 256)
	for _, line := range strings.Split(string(revOut), "\n") {
		if line == "" {
			continue
		}
		sp := strings.IndexByte(line, ' ')
		var sha, path string
		if sp < 0 {
			// commit/tree entries with no path — skip.
			continue
		}
		sha = line[:sp]
		path = strings.TrimSpace(line[sp+1:])
		if path == "" {
			continue
		}
		if _, ok := pathBySha[sha]; !ok {
			pathBySha[sha] = path
			hashes = append(hashes, sha)
		}
	}
	if len(hashes) == 0 {
		return nil, nil
	}

	catCmd := exec.CommandContext(ctx, "git", "-C", root, "cat-file", "--batch-check=%(objecttype) %(objectname) %(objectsize)")
	catCmd.Stdin = strings.NewReader(strings.Join(hashes, "\n") + "\n")
	catOut, err := catCmd.Output()
	if err != nil {
		return nil, err
	}

	blobs := make([]blobInfo, 0, len(hashes))
	for _, line := range strings.Split(string(catOut), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != "blob" {
			continue
		}
		size, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil {
			continue
		}
		blobs = append(blobs, blobInfo{
			Hash: fields[1],
			Path: pathBySha[fields[1]],
			Size: size,
		})
	}
	return blobs, nil
}

// readBlob streams the content of a single blob via `git cat-file -p`.
// Used by content-scanning history gates; for size-only checks the
// caller can use blobInfo.Size and skip this.
func readBlob(ctx context.Context, root, hash string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "cat-file", "-p", hash)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// shortHash returns the first 8 hex chars of an OID — convention from
// `git log --abbrev-commit`. Used in finding messages so users can paste
// it into `git show <short>` directly.
func shortHash(h string) string {
	if len(h) < 8 {
		return h
	}
	return h[:8]
}
