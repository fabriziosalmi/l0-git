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
// CI — a suite whose result depends on whose laptop runs it.
//
// GIT_CONFIG_GLOBAL needs git 2.32+; XDG_CONFIG_HOME also covers the implicit
// ~/.config/git/ignore on older ones.
func TestMain(m *testing.M) {
	cfgDir, err := os.MkdirTemp("", "lgit-test-gitcfg-")
	if err != nil {
		panic(err)
	}
	os.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	os.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	os.Setenv("XDG_CONFIG_HOME", cfgDir)
	code := m.Run()
	os.RemoveAll(cfgDir)
	os.Exit(code)
}
