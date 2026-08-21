package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The space-separated spelling used to be silently dropped: `-project` with no
// inline `=` yielded an empty value, which Stats reads as "every project in the
// store". The command then printed another repo's totals as if they were yours.
func TestParseStatsFlags_BothSpellings(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no args", nil, ""},
		{"inline", []string{"-project=/tmp/x"}, "/tmp/x"},
		{"inline double dash", []string{"--project=/tmp/x"}, "/tmp/x"},
		{"space separated", []string{"-project", "/tmp/x"}, "/tmp/x"},
		{"space separated double dash", []string{"--project", "/tmp/x"}, "/tmp/x"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseStatsFlags(c.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Errorf("project = %q, want %q", got, c.want)
			}
		})
	}
}

// Anything the command cannot honour must fail loudly rather than fall back to
// store-wide totals, which is the failure mode this whole parser exists to fix.
func TestParseStatsFlags_RejectsGarbage(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"bare positional", []string{"."}},
		{"positional after flag", []string{"-project=/tmp/x", "extra"}},
		{"unknown flag", []string{"-severity=error"}},
		{"project without value", []string{"-project"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := parseStatsFlags(c.args); err == nil {
				t.Errorf("expected an error for %v, got none", c.args)
			}
		})
	}
}

func TestParseFixFlags(t *testing.T) {
	ok := []struct {
		name string
		args []string
		want bool
	}{
		{"none", nil, false},
		{"single dash", []string{"-json"}, true},
		{"double dash", []string{"--json"}, true},
		{"explicit true", []string{"--json=true"}, true},
	}
	for _, c := range ok {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseFixFlags(c.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Errorf("asJSON = %v, want %v", got, c.want)
			}
		})
	}

	// A typo used to fall through as "no --json", so a caller piping the output
	// into a JSON parser got prose and a confusing parse error instead.
	bad := [][]string{
		{"--jsno"},
		{"text"},
		{"--json=nope"},
	}
	for _, args := range bad {
		if _, err := parseFixFlags(args); err == nil {
			t.Errorf("expected an error for %v, got none", args)
		}
	}
}

// captureStreams runs fn with os.Stdout and os.Stderr redirected, and returns
// what each received.
func captureStreams(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	origOut, origErr := os.Stdout, os.Stderr
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = wOut, wErr
	done := make(chan [2]string, 1)
	go func() {
		o, _ := io.ReadAll(rOut)
		e, _ := io.ReadAll(rErr)
		done <- [2]string{string(o), string(e)}
	}()
	fn()
	os.Stdout, os.Stderr = origOut, origErr
	_ = wOut.Close()
	_ = wErr.Close()
	got := <-done
	return got[0], got[1]
}

// The contract `lgit check . | jq '.findings | length'` depends on: warnings on
// stderr, a parseable JSON document on stdout, exit 0. A regression that put
// the config warning on stdout would break every such pipeline while the rest
// of the suite stayed green.
func TestCheckCLI_ConfigWarningGoesToStderrNotStdout(t *testing.T) {
	root := initRepoWithCommit(t, map[string]string{
		"README.md": "# x\n",
		// A key that does not exist on this gate: reported, non-fatal.
		".l0git.json": `{"gate_options":{"large_file_tracked":{"treshold_mb":20}}}`,
	})
	t.Setenv("LGIT_DB", filepath.Join(t.TempDir(), "findings.db"))

	var runErr error
	stdout, stderr := captureStreams(t, func() {
		runErr = runCLI([]string{"check", root})
	})
	if runErr != nil {
		t.Fatalf("a bad config must not fail the run: %v", runErr)
	}
	if !strings.Contains(stderr, "treshold_mb") {
		t.Errorf("the config warning did not reach stderr, got: %q", stderr)
	}
	if strings.Contains(stdout, "warning:") {
		t.Errorf("the warning leaked into stdout, which breaks `| jq`:\n%s", stdout)
	}
	var res CheckResult
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatalf("stdout is not a single JSON document: %v\n%s", err, stdout)
	}
	if res.ConfigError == "" {
		t.Error("config_error is empty in the JSON; it should carry the same text")
	}
}

// The other half of the contract: a clean config must say nothing at all, or
// every well-configured project would grow a warning in its build log.
func TestCheckCLI_CleanConfigIsSilentOnStderr(t *testing.T) {
	root := initRepoWithCommit(t, map[string]string{
		"README.md":   "# x\n",
		".l0git.json": `{"gate_options":{"large_file_tracked":{"threshold_mb":20}}}`,
	})
	t.Setenv("LGIT_DB", filepath.Join(t.TempDir(), "findings.db"))

	var runErr error
	stdout, stderr := captureStreams(t, func() {
		runErr = runCLI([]string{"check", root})
	})
	if runErr != nil {
		t.Fatal(runErr)
	}
	if strings.TrimSpace(stderr) != "" {
		t.Errorf("a valid config wrote to stderr: %q", stderr)
	}
	var res CheckResult
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatalf("stdout is not valid JSON: %v", err)
	}
	if res.ConfigError != "" {
		t.Errorf("config_error should be empty, got %q", res.ConfigError)
	}
}
