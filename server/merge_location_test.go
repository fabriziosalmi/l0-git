package main

import (
	"context"
	"strings"
	"testing"
)

func TestMergeSameLocation_Unit(t *testing.T) {
	fs := []Finding{
		{FilePath: "a.md:3:ipv4_public-resolver", Severity: SeverityInfo, Message: "IPv4 1.1.1.3 found"},
		{FilePath: "a.md:3:ipv4_public-resolver", Severity: SeverityInfo, Message: "IPv4 9.9.9.9 found"},
		{FilePath: "b.md:1:link_local_broken", Severity: SeverityWarning, Message: "only one"},
		{FilePath: "a.md:3:ipv4_public-resolver", Severity: SeverityInfo, Message: "IPv4 1.1.1.3 found"}, // repeated literal
	}
	got := mergeSameLocation(fs)
	if len(got) != 2 {
		t.Fatalf("expected 2 locations, got %d: %+v", len(got), got)
	}
	if got[0].FilePath != "a.md:3:ipv4_public-resolver" || got[1].FilePath != "b.md:1:link_local_broken" {
		t.Errorf("first-seen order not preserved: %+v", got)
	}
	m := got[0].Message
	if !strings.Contains(m, "2 findings at this location") || !strings.Contains(m, "1.1.1.3") || !strings.Contains(m, "9.9.9.9") {
		t.Errorf("merged message must list every distinct finding once: %s", m)
	}
	if strings.Count(m, "1.1.1.3") != 1 {
		t.Errorf("a repeated literal must be listed once: %s", m)
	}
	if got[1].Message != "only one" {
		t.Errorf("a lone finding must be left untouched: %q", got[1].Message)
	}
}

// Identical findings are not a group: no wrapper, no count.
func TestMergeSameLocation_IdenticalDuplicatesStayPlain(t *testing.T) {
	got := mergeSameLocation([]Finding{
		{FilePath: "x:1:r", Severity: SeverityInfo, Message: "same"},
		{FilePath: "x:1:r", Severity: SeverityInfo, Message: "same"},
	})
	if len(got) != 1 || got[0].Message != "same" {
		t.Errorf("identical duplicates should fold to the plain message, got %+v", got)
	}
}

// Last write used to win on severity too. With credential severity depending
// on the host, a local and a remote credential on one line could be stored as
// a warning — losing the error.
func TestMergeSameLocation_KeepsHighestSeverity(t *testing.T) {
	got := mergeSameLocation([]Finding{
		{FilePath: "x:1:creds_in_url", Severity: SeverityError, Title: "remote", Message: "remote cred"},
		{FilePath: "x:1:creds_in_url", Severity: SeverityWarning, Title: "local", Message: "local cred"},
	})
	if got[0].Severity != SeverityError {
		t.Fatalf("severity = %s, want error", got[0].Severity)
	}
	if !strings.HasPrefix(got[0].Message, "2 findings") || strings.Index(got[0].Message, "remote cred") > strings.Index(got[0].Message, "local cred") {
		t.Errorf("the most severe finding must be listed first: %s", got[0].Message)
	}
	// Order of arrival must not matter.
	got = mergeSameLocation([]Finding{
		{FilePath: "x:1:creds_in_url", Severity: SeverityWarning, Message: "local cred"},
		{FilePath: "x:1:creds_in_url", Severity: SeverityError, Message: "remote cred"},
	})
	if got[0].Severity != SeverityError {
		t.Errorf("severity = %s with the error arriving second, want error", got[0].Severity)
	}
}

// End to end through SQLite, where the bug lived: three addresses on one line
// must all be in the stored row, not just the last one written.
func TestRunChecks_SameLineFindingsAllReachTheStore(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		"docs/dns.md": "Upstream resolvers default to `1.1.1.3`, `9.9.9.9`, `8.8.8.8`.\n",
	})
	store := newTestStore(t)
	ctx := context.Background()
	if _, err := RunChecks(ctx, store, root, "network_scan"); err != nil {
		t.Fatal(err)
	}
	rows, err := store.List(ctx, FindingFilter{Project: root, GateID: "network_scan"})
	if err != nil {
		t.Fatal(err)
	}
	var row *Finding
	for i := range rows {
		if strings.HasPrefix(rows[i].FilePath, "docs/dns.md:1:") {
			row = &rows[i]
		}
	}
	if row == nil {
		t.Fatalf("no stored finding for docs/dns.md:1, rows: %+v", rows)
	}
	for _, ip := range []string{"1.1.1.3", "9.9.9.9", "8.8.8.8"} {
		if !strings.Contains(row.Message, ip) {
			t.Errorf("stored row lost %s: %s", ip, row.Message)
		}
	}
}

// The reason for folding instead of re-keying: an ignored finding must stay
// ignored across runs, with no migration.
func TestRunChecks_MergedFindingStaysIgnored(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		"docs/dns.md": "Upstream resolvers default to `1.1.1.3`, `9.9.9.9`, `8.8.8.8`.\n",
	})
	store := newTestStore(t)
	ctx := context.Background()
	res, err := RunChecks(ctx, store, root, "network_scan")
	if err != nil {
		t.Fatal(err)
	}
	var id int64
	for _, f := range res.Findings {
		if strings.HasPrefix(f.FilePath, "docs/dns.md:1:") {
			id = f.ID
		}
	}
	if id == 0 {
		t.Fatal("no finding to ignore")
	}
	if _, err := store.Ignore(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := RunChecks(ctx, store, root, "network_scan"); err != nil {
		t.Fatal(err)
	}
	f, err := store.GetByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if f.Status != "ignored" {
		t.Errorf("status after re-run = %q, want ignored", f.Status)
	}
}
