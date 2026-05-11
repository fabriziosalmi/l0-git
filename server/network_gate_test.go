package main

import (
	"context"
	"strings"
	"testing"
)

// Each row plants one address shape and asserts the (severity, FilePath
// suffix) pair the gate is supposed to emit.
func TestNetworkScan_Classification(t *testing.T) {
	cases := []struct {
		name        string
		content     string
		wantSeverity string
		wantSuffix  string // suffix of FilePath (after the line number)
	}{
		{"public_ipv4", "host = 8.8.8.8\n", SeverityWarning, "ipv4_public"},
		{"private_192", "addr = 192.168.1.10\n", SeverityInfo, "ipv4_private"},
		{"private_10", "addr = 10.0.0.5\n", SeverityInfo, "ipv4_private"},
		{"loopback", "addr = 127.0.0.1\n", SeverityInfo, "ipv4_loopback"},
		{"unspecified", "bind = 0.0.0.0\n", SeverityInfo, "ipv4_unspecified"},
		{"public_cidr", "deny 1.1.1.0/24\n", SeverityWarning, "cidr_public"},
		{"private_cidr", "allow 10.0.0.0/8\n", SeverityInfo, "cidr_private"},
		{"asn", "AS15169 routes everything\n", SeverityInfo, "asn"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := initRepoWithFiles(t, map[string]string{"net.txt": tc.content})
			fs, err := checkNetworkScan(context.Background(), root, nil)
			if err != nil {
				t.Fatal(err)
			}
			matched := false
			for _, f := range fs {
				if f.Severity == tc.wantSeverity && strings.HasSuffix(f.FilePath, ":"+tc.wantSuffix) {
					matched = true
					break
				}
			}
			if !matched {
				t.Fatalf("no finding matched (severity=%s, suffix=%s); got: %+v",
					tc.wantSeverity, tc.wantSuffix, fs)
			}
		})
	}
}

// Doc-range addresses (RFC 5737 / 2544 / 6598, MCAST-TEST-NET) are intended
// for documentation/testing — emitting a finding only adds noise.
func TestNetworkScan_DocRangeSuppressed(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"test_net_1", "addr = 192.0.2.42\n"},
		{"test_net_3", "addr = 203.0.113.7\n"},
		{"cgnat_cidr", "range = 100.64.0.0/10\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := initRepoWithFiles(t, map[string]string{"net.txt": tc.content})
			fs, err := checkNetworkScan(context.Background(), root, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(fs) != 0 {
				t.Fatalf("doc-range must produce no findings; got: %+v", fs)
			}
		})
	}
}

// Changelog / release-notes files reference IPs descriptively, not wired
// addresses — every match is a self-referential FP. Skip by basename.
func TestNetworkScan_SkipsChangelogFiles(t *testing.T) {
	for _, name := range []string{"CHANGELOG.md", "HISTORY.md", "RELEASES.md", "CHANGES.md"} {
		t.Run(name, func(t *testing.T) {
			root := initRepoWithFiles(t, map[string]string{name: "added 8.8.8.8 to docs\n"})
			fs, err := checkNetworkScan(context.Background(), root, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(fs) != 0 {
				t.Fatalf("changelog must be skipped; got: %+v", fs)
			}
		})
	}
}

// CIDR matches must shadow the bare IPv4 inside them — otherwise we'd emit
// "10.0.0.0/8 (cidr)" AND "10.0.0.0 (ipv4)" for the same span.
func TestNetworkScan_CIDRDoesNotDoubleFire(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{"net.txt": "block 10.0.0.0/8 here\n"})
	fs, err := checkNetworkScan(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(fs), fs)
	}
	if !strings.HasSuffix(fs[0].FilePath, ":cidr_private") {
		t.Errorf("expected cidr finding, got: %+v", fs[0])
	}
}

// Octet > 255 must not pass the parse step — protects against false hits
// like "256.300.0.1".
// Extended documentation ranges (TEST-NET-2, CGNAT, MCAST-TEST-NET) must
// be classified as doc-range (info), not flagged as public (warning).
func TestNetworkScan_ExtendedDocRanges(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"test_net_2_198.18", "addr = 198.18.0.1\n"},
		{"test_net_2_198.19", "addr = 198.19.255.254\n"},
		{"cgnat_100.64", "addr = 100.64.0.1\n"},
		{"cgnat_100.127", "addr = 100.127.255.254\n"},
		{"mcast_test_net", "addr = 233.252.0.42\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := initRepoWithFiles(t, map[string]string{"net.txt": tc.content})
			fs, err := checkNetworkScan(context.Background(), root, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range fs {
				if f.Severity == SeverityWarning {
					t.Errorf("%s: doc-range IP must not be a warning; got: %+v", tc.name, f)
				}
			}
		})
	}
}

func TestNetworkScan_RejectsInvalidOctets(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{"net.txt": "bogus 999.999.999.999\n"})
	fs, err := checkNetworkScan(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 0 {
		t.Errorf("expected no findings for invalid octet, got: %+v", fs)
	}
}

func TestNetworkScan_NotGitRepo(t *testing.T) {
	fs, err := checkNetworkScan(context.Background(), t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 || fs[0].Severity != SeverityInfo {
		t.Errorf("expected one info skip, got: %+v", fs)
	}
}
