package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// statTimeout is how long a stat of a project's directory may take. A dead NFS
// or SMB mount does not fail: it blocks, and a `lgit stats` or `lgit prune` that
// waited for it would hang. A stat that does not answer is treated as "cannot
// tell", never as "gone".
const statTimeout = 3 * time.Second

var errStatTimeout = errors.New("stat timed out")

type statResult struct {
	fi  os.FileInfo
	err error
}

// statWithin is os.Stat that gives up after statTimeout. The abandoned
// goroutine stays blocked in the kernel, which is acceptable in a short-lived
// process and is the only way to stop waiting for it.
func statWithin(path string, lstat bool) (os.FileInfo, error) {
	ch := make(chan statResult, 1)
	go func() {
		var r statResult
		if lstat {
			r.fi, r.err = os.Lstat(path)
		} else {
			r.fi, r.err = os.Stat(path)
		}
		ch <- r
	}()
	select {
	case r := <-ch:
		return r.fi, r.err
	case <-time.After(statTimeout):
		return nil, errStatTimeout
	}
}

// dirExists reports whether path is an existing directory.
func dirExists(path string) bool {
	fi, err := statWithin(path, false)
	return err == nil && fi.IsDir()
}

// projectState is what can be said about a project's directory from here.
type projectState int

const (
	projectPresent     projectState = iota // the directory exists
	projectVanished                        // it is gone, and everything around it is still there
	projectUnreachable                     // it is missing, but that may be a volume that is not mounted
)

// mountRoots are the directories under which removable drives and network shares
// appear. A variable so a test can stand in for /Volumes.
var mountRoots = []string{"/Volumes", "/mnt", "/media", "/run/media", "/net", "/Network"}

// allMountRoots adds the roots that depend on the user: on macOS, cloud storage
// providers are mounted under ~/Library/CloudStorage.
func allMountRoots() []string {
	roots := append([]string{}, mountRoots...)
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, "Library", "CloudStorage"))
	}
	return roots
}

// mountDepth is how many path components a missing project must have a surviving
// ancestor below a mount root before it counts as deleted. udisks mounts at
// /media/USER/LABEL, so the mount point is two components deep and the project
// inside it a third: a missing directory shallower than that may be a drive that
// is not plugged in, and the flat /Volumes/LABEL layout is covered by the same bound.
const mountDepth = 3

// classifyProject decides whether a project that no longer has a directory is
// GONE or merely OFFLINE. The two must not be confused, because pruning a
// project deletes its findings — the ignored ones, which are the user's own
// decisions, included — and deleting them because a drive was unplugged is data
// loss. Whenever it cannot tell, the answer is "unreachable".
//
// A project is unreachable — never pruned — when any of these holds:
//   - something IS at the path, or at an ancestor, but it is not a directory we
//     can enter: a symlink to a drive that is away (`~/proj -> /Volumes/USB/proj`,
//     `~/code -> /Volumes/T7/code`), a file;
//   - a stat fails for any reason other than "not found", or does not answer
//     (permission denied, I/O error, a dead network mount);
//   - on Windows, its drive is missing;
//   - it sits below a mount root (`/Volumes`, `/media`, `/run/media`,
//     `~/Library/CloudStorage`, …) and the nearest ancestor that still exists is
//     fewer than mountDepth components below it — a missing mount point, or a
//     directory directly under one;
//   - the nearest directory above it that still exists is EMPTY: an unmounted
//     volume leaves its mount point behind as an empty directory, where a
//     deleted project leaves its neighbours;
//   - the nearest directory above it that still exists is the filesystem root.
//
// Otherwise it is vanished. An earlier rule — "the parent must exist" — was too
// timid: deleting a whole tree (`work/` and every clone in it) leaves no parent
// either, and those were the largest piles of findings nothing could resolve.
func classifyProject(project string) projectState {
	if project == "" {
		return projectPresent
	}
	if dirExists(project) {
		return projectPresent
	}
	if _, err := statWithin(project, true); err == nil {
		return projectUnreachable // a link that leads nowhere, or a file
	}
	if _, err := statWithin(project, false); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return projectUnreachable
	}
	if vol := filepath.VolumeName(project); vol != "" && !dirExists(vol+string(filepath.Separator)) {
		return projectUnreachable
	}
	anc := filepath.Dir(project)
	for !dirExists(anc) {
		if _, err := statWithin(anc, true); err == nil {
			return projectUnreachable // a dangling link, or a file, in the way
		}
		if _, err := statWithin(anc, false); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return projectUnreachable
		}
		next := filepath.Dir(anc)
		if next == anc {
			return projectUnreachable // nothing above it exists at all
		}
		anc = next
	}
	for _, root := range allMountRoots() {
		rel, err := filepath.Rel(root, anc)
		if err != nil || strings.HasPrefix(rel, "..") {
			// anc is not under this root; the PROJECT may still be (a missing mount point).
			if relP, errP := filepath.Rel(root, project); errP == nil && !strings.HasPrefix(relP, "..") && relP != "." {
				if rel, err = filepath.Rel(root, anc); err != nil || strings.HasPrefix(rel, "..") {
					return projectUnreachable
				}
			} else {
				continue
			}
		}
		if rel == "." || len(strings.Split(filepath.ToSlash(rel), "/")) < mountDepth {
			return projectUnreachable
		}
	}
	if filepath.Dir(anc) == anc {
		return projectUnreachable
	}
	if entries, err := os.ReadDir(anc); err != nil || len(entries) == 0 {
		return projectUnreachable
	}
	return projectVanished
}

func isVanished(project string) bool { return classifyProject(project) == projectVanished }

// PruneOptions says what Prune may remove.
type PruneOptions struct {
	// Apply performs the deletion. Without it Prune only reports what it would do.
	Apply bool
	// KeepResolvedDays keeps resolved findings updated within this many days.
	// 0 removes every resolved finding. At most maxKeepResolvedDays.
	KeepResolvedDays int
}

// maxKeepResolvedDays is a century. The cutoff is computed in a time.Duration,
// which overflows int64 at 106,752 days — and a natural way of writing "keep
// everything" (-keep-resolved-days=999999) then put the cutoff in the FUTURE and
// deleted every resolved finding, while the dry run inverted the same way and so
// did not warn.
const maxKeepResolvedDays = 36500

// PruneReport is what Prune found and, with Apply, removed.
type PruneReport struct {
	DryRun bool `json:"dry_run"`
	// Projects whose directory no longer exists, and the findings recorded for them.
	VanishedProjects []string `json:"vanished_projects"`
	VanishedFindings int      `json:"vanished_findings"`
	// Missing projects that MAY be offline (an unmounted volume) and were
	// therefore left alone. `lgit clear <project>` removes one by hand.
	UnreachableProjects []string `json:"unreachable_projects"`
	// Resolved findings of projects that still exist, older than the window.
	ResolvedFindings int `json:"resolved_findings"`
	// Findings the user chose to ignore, in projects that still exist. Never removed.
	IgnoredKept  int   `json:"ignored_kept"`
	BytesBefore  int64 `json:"bytes_before"`
	BytesAfter   int64 `json:"bytes_after,omitempty"`
	Compacted    bool  `json:"compacted"`
	KeepResolved int   `json:"keep_resolved_days"`
}

// Prune removes what can no longer be acted on: every finding of a project whose
// directory is gone (nothing can ever re-check it, so nothing can ever resolve
// it), and resolved findings older than the window.
//
// It NEVER removes an open or ignored finding of a project that still exists.
// Ignored findings are the user's own decisions and cannot be regenerated by
// re-running a check.
func (s *Store) Prune(ctx context.Context, path string, opts PruneOptions) (*PruneReport, error) {
	if opts.KeepResolvedDays < 0 || opts.KeepResolvedDays > maxKeepResolvedDays {
		return nil, fmt.Errorf("keep-resolved-days must be between 0 and %d, got %d", maxKeepResolvedDays, opts.KeepResolvedDays)
	}
	rep := &PruneReport{DryRun: !opts.Apply, VanishedProjects: []string{}, UnreachableProjects: []string{}, KeepResolved: opts.KeepResolvedDays}
	if fi, err := os.Stat(path); err == nil {
		rep.BytesBefore = fi.Size()
	}

	projects, err := s.knownProjects(ctx)
	if err != nil {
		return nil, err
	}
	vanished := map[string]bool{}
	for _, p := range projects {
		switch classifyProject(p) {
		case projectVanished:
			vanished[p] = true
			rep.VanishedProjects = append(rep.VanishedProjects, p)
		case projectUnreachable:
			rep.UnreachableProjects = append(rep.UnreachableProjects, p)
		}
	}
	sort.Strings(rep.VanishedProjects)
	sort.Strings(rep.UnreachableProjects)

	var cutoffMs int64
	if opts.KeepResolvedDays > 0 {
		cutoffMs = time.Now().Add(-time.Duration(opts.KeepResolvedDays) * 24 * time.Hour).UnixMilli()
	}

	rows, err := s.db.QueryContext(ctx, `SELECT project, status, updated_at FROM findings`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var p, st string
		var upd int64
		if err := rows.Scan(&p, &st, &upd); err != nil {
			_ = rows.Close()
			return nil, err
		}
		switch {
		case vanished[p]:
			rep.VanishedFindings++
		case st == StatusResolved && (opts.KeepResolvedDays == 0 || upd < cutoffMs):
			rep.ResolvedFindings++
		case st == StatusIgnored:
			rep.IgnoredKept++
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()

	if !opts.Apply {
		return rep, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	for p := range vanished {
		// Re-checked now, not trusted from the scan above: a project re-created
		// in the meantime has fresh findings of its own.
		if classifyProject(p) != projectVanished {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM findings WHERE project = ?`, p); err != nil {
			_ = tx.Rollback()
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM project_checks WHERE project = ?`, p); err != nil {
			_ = tx.Rollback()
			return nil, err
		}
	}
	// Only findings of projects that still exist reach this line (the vanished
	// ones are already gone), and only those the user has not ignored or left open.
	var delErr error
	if opts.KeepResolvedDays == 0 {
		_, delErr = tx.ExecContext(ctx, `DELETE FROM findings WHERE status = 'resolved'`)
	} else {
		_, delErr = tx.ExecContext(ctx, `DELETE FROM findings WHERE status = 'resolved' AND updated_at < ?`, cutoffMs)
	}
	if delErr != nil {
		_ = tx.Rollback()
		return nil, delErr
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	// Rebuild the file so the removed rows do not linger in free pages, and so
	// the disk space is actually returned.
	if _, err := s.db.ExecContext(ctx, `VACUUM`); err == nil {
		rep.Compacted = checkpointWithRetry(s.db)
	}
	if fi, err := os.Stat(path); err == nil {
		rep.BytesAfter = fi.Size()
	}
	return rep, nil
}
