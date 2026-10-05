package main

import (
	"os"
	"testing"
)

// TestMain makes git hermetic for the whole suite.
//
// The fixtures stage files with `git add -A` and the gates read the index. A
// developer's own `~/.gitignore_global` (core.excludesFile) or
// `~/.config/git/ignore` then decides what a fixture repository tracks:
// `.DS_Store` is in nearly every macOS user's global ignore, so
// TestIdeArtifactTracked_FlagsArtefacts failed on those machines and passed on
// CI: a suite whose result depends on whose laptop runs it.
//
// Three things can reach a fixture from outside, and all three are cut:
//   - the global and system configuration (GIT_CONFIG_GLOBAL needs git 2.32+,
//     so HOME is redirected as well, which also covers older git);
//   - the implicit ~/.config/git/ignore (XDG_CONFIG_HOME);
//   - a repository the test process was started from. `go test` run from a git
//     hook inherits GIT_DIR and GIT_INDEX_FILE, and every fixture would then
//     operate on the wrong repository.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "lgit-test-home-")
	if err != nil {
		panic(err)
	}
	for _, k := range []string{
		"GIT_DIR", "GIT_INDEX_FILE", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_PREFIX",
		"GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE",
	} {
		os.Unsetenv(k)
	}
	os.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	os.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	os.Setenv("XDG_CONFIG_HOME", home)
	os.Setenv("HOME", home)
	os.Setenv("USERPROFILE", home)
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}
