package main

import "testing"

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
