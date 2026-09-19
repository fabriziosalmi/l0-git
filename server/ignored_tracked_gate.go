package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path"
	"sort"
	"strings"
)

// ignored_file_tracked reports files that are in the index even though the
// repository's own .gitignore says they should not be.
//
// The index and the ignore rules disagree, and one of them is wrong. Either the
// file was committed before the rule existed or was force-added by mistake —
// then it should be untracked — or it belongs in the repository and the
// exception was never written down, in which case the fix is a `!` negation in
// .gitignore. The gate cannot tell which, so it reports the contradiction and
// leaves the choice to the reader.
//
// The public-repo sweep that motivated it found node identity keys tracked
// under a `data/` the .gitignore excluded, a committed .env, __pycache__, a
// coverage report, and 260 cache and report files in a single repository.
//
// Only COMMITTED .gitignore files are consulted
// (`--exclude-per-directory=.gitignore`). `--exclude-standard` would also read
// .git/info/exclude and the user's global excludes file, which differ from one
// machine to the next — the same repository would then produce different
// findings on two computers, which is exactly what a gate may not do.
func checkIgnoredFileTracked(ctx context.Context, root string, opts json.RawMessage) ([]Finding, error) {
	if skip, stop := requireGitRepo(root, "ignored_file_tracked",
		"This gate compares the git index against the repository's .gitignore files."); stop {
		return skip, nil
	}
	cmd := exec.CommandContext(ctx, "git", "-C", root, "ls-files", "-z",
		"--cached", "--ignored", "--exclude-per-directory=.gitignore")
	stdout, err := cmd.Output()
	if err != nil {
		return []Finding{{
			Severity: SeverityWarning,
			Title:    "ignored_file_tracked failed",
			Message:  fmt.Sprintf("Could not list tracked files matching .gitignore: %v", err),
			FilePath: ".git",
		}}, nil
	}
	scan := parseScanOptions(opts)

	groups := map[string][]string{}
	for _, p := range bytes.Split(stdout, []byte{0}) {
		rel := string(p)
		if rel == "" || scan.shouldSkip(rel) || ignoredTrackedExempt(rel) {
			continue
		}
		// Root-level files share one group: only the root .gitignore can have
		// excluded them, so that is the one thing they have in common. Keyed
		// per file, `test_*.sh` in the root produced a finding per script.
		top := ""
		if i := strings.IndexByte(rel, '/'); i >= 0 {
			top = rel[:i]
		}
		groups[top] = append(groups[top], rel)
	}

	tops := make([]string, 0, len(groups))
	for t := range groups {
		tops = append(tops, t)
	}
	sort.Strings(tops)

	out := make([]Finding, 0, len(tops))
	for _, t := range tops {
		files := groups[t]
		sort.Strings(files)
		key := commonDirPrefix(files)
		if t == "" && len(files) > 1 {
			key = ".gitignore"
		}
		out = append(out, Finding{
			Title:    "Tracked file matches .gitignore",
			Message:  ignoredTrackedMessage(key, files),
			FilePath: key,
		})
	}
	return out, nil
}

// ignoredTrackedExempt reports whether a tracked-but-ignored file is one that
// only exists to be force-added, or that another gate already reports.
func ignoredTrackedExempt(rel string) bool {
	base := path.Base(rel)
	// A .gitkeep has no content and no purpose other than being force-added
	// into a directory whose contents are ignored.
	if base == ".gitkeep" || base == ".keep" {
		return true
	}
	// Committed env templates are routinely caught by a broad `.env*` rule.
	// secrets_scan still reads them; this gate has nothing to add.
	for _, n := range envExampleNames {
		if base == n {
			return true
		}
	}
	// vendored_dir_tracked already makes the one actionable statement about a
	// committed dependency tree; repeating it here per directory is noise.
	for _, prefix := range vendoredDirPrefixes {
		if dirMatchesAtAnyDepth(rel, prefix) {
			return true
		}
	}
	return false
}

// commonDirPrefix returns the deepest directory containing every file, with a
// trailing slash, or the single file itself when there is only one.
func commonDirPrefix(files []string) string {
	if len(files) == 1 {
		return files[0]
	}
	prefix := path.Dir(files[0])
	for _, f := range files[1:] {
		for prefix != "." && !strings.HasPrefix(f, prefix+"/") {
			prefix = path.Dir(prefix)
		}
	}
	if prefix == "." {
		return files[0][:strings.IndexByte(files[0], '/')+1]
	}
	return prefix + "/"
}

func ignoredTrackedMessage(key string, files []string) string {
	const show = 3
	examples := files
	if len(examples) > show {
		examples = examples[:show]
	}
	list := strings.Join(examples, ", ")
	if len(files) > show {
		list += fmt.Sprintf(", … (%d more)", len(files)-show)
	}
	noun := "file"
	if len(files) != 1 {
		noun = "files"
	}
	where := "under " + key
	target := key
	if key == ".gitignore" {
		where = "at the repository root"
		target = strings.Join(examples, " ")
		if len(files) > show {
			target += " …"
		}
	}
	return fmt.Sprintf("%d tracked %s %s match the repository's .gitignore: %s. "+
		"The index and the ignore rules disagree. If the files should not be in the "+
		"repository, untrack them with `git rm -r --cached %s`. If they belong there, add "+
		"a `!` negation to .gitignore so the exception is written down instead of implied.",
		len(files), noun, where, list, target)
}
