//go:build windows

package main

import "testing"

// A project on a drive letter that is not there is offline, not deleted. The
// Unix tests cannot exercise this rule, and CI has a Windows leg.
func TestClassifyProject_MissingWindowsDriveIsUnreachable(t *testing.T) {
	for _, drive := range []string{`Q:`, `Y:`, `Z:`} {
		if dirExists(drive + `\`) {
			continue // this runner happens to have it
		}
		if got := classifyProject(drive + `\work\proj`); got != projectUnreachable {
			t.Errorf("%s: a project on a missing drive = %v, want unreachable", drive, got)
		}
		return
	}
	t.Skip("every probe drive letter exists on this runner")
}
