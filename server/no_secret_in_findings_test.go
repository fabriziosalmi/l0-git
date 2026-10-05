package main

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// Secrets the fixtures plant. None of them may appear anywhere in what a
// finding hands out, in any of its fields, and neither may any alphanumeric
// fragment of one, because a redaction that masks the first half of a password
// and leaves the rest is not a redaction.
var plantedSecrets = []string{
	"Zq8!nRt4vLw2", // creds_in_url password
	"Mx7kP3dQ9yBs", // db_uri query password
	"V3cR9tL2hN8w", // jdbc semicolon password
	"Tk5sB1mW7eH4", // ftp password
	"Ab1@xyzQw9Lm", // a password containing an at sign
	"ghp_K8qM2xV9LpR4tZ7wYb3NcD6sFh1JgA5uE0xV", // a token-shaped string for secrets_scan
}

func fixtureWithSecrets() map[string]string {
	return map[string]string{
		"app.env":  "DATABASE_URL=postgres://svc:Zq8!nRt4vLw2@db.prod.acme.io:5432/app\n",
		"conf.ini": "url = mongodb://db.acme.io:27017/app?authSource=admin&password=Mx7kP3dQ9yBs\n",
		"db.conf":  "jdbc:sqlserver://db.acme.io;databaseName=app;password=V3cR9tL2hN8w;encrypt=true\n",
		"sync.sh":  "ftp://deploy:Tk5sB1mW7eH4@files.acme.io/dump.tar\n",
		// The gate reads the password up to the FIRST `@` (`Ab1`, three characters
		//, not shorthand), so this one is reported; the redactor must mask up to
		// the LAST `@` or `xyzQw9Lm` stays behind.
		"odd.txt":  "postgres://app:Ab1@xyzQw9Lm@db.prod.acme.io/app\n",
		"notes.md": "token for CI: ghp_K8qM2xV9LpR4tZ7wYb3NcD6sFh1JgA5uE0xV\n",
	}
}

var alnumFragment = regexp.MustCompile(`[A-Za-z0-9]{6,}`)

func assertNoSecret(t *testing.T, where, blob string) {
	t.Helper()
	for _, sec := range plantedSecrets {
		if strings.Contains(blob, sec) {
			t.Errorf("%s leaks a secret (%d chars, starts %q)", where, len(sec), sec[:3])
		}
		for _, frag := range alnumFragment.FindAllString(sec, -1) {
			if strings.Contains(blob, frag) {
				t.Errorf("%s leaks part of a secret (a %d-character fragment of one that starts %q)", where, len(frag), sec[:3])
			}
		}
	}
}

// Gate level: the finding still says WHERE and keeps its severity and its key,
// but the message no longer carries the value.
func TestConnectionStrings_MessageNeverCarriesTheSecret(t *testing.T) {
	root := initRepoWithFiles(t, fixtureWithSecrets())
	fs, err := checkConnectionStrings(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Every planted file must actually have produced a finding, or the absence
	// of its secret proves nothing about the gate.
	reported := map[string]bool{}
	for _, f := range fs {
		reported[f.FilePath[:strings.Index(f.FilePath, ":")]] = true
	}
	for _, file := range []string{"app.env", "conf.ini", "db.conf", "sync.sh", "odd.txt"} {
		if !reported[file] {
			t.Fatalf("the fixture %s produced no finding, so it cannot show that its secret is masked; got %v", file, reported)
		}
	}
	sawCreds := false
	for _, f := range fs {
		blob, _ := json.Marshal(f)
		assertNoSecret(t, f.FilePath, string(blob))
		if strings.HasSuffix(f.FilePath, ":creds_in_url") {
			sawCreds = true
			if f.Severity != SeverityError && f.Severity != SeverityWarning {
				t.Errorf("redaction must not change severity: %+v", f)
			}
			if !strings.Contains(f.Message, ":"+redactedMark+"@") {
				t.Errorf("message should show the masked credential, got %q", f.Message)
			}
		}
	}
	if !sawCreds {
		t.Fatal("no creds_in_url finding: the fixture is not exercising the rule")
	}
}

// End to end: through RunChecks into the store, the JSON the CLI and MCP print,
// and every column of every row. This is the contract: a finding says where a
// secret is and never what it is, from any gate.
func TestRunChecks_NoSecretReachesOutputOrStore(t *testing.T) {
	root := initRepoWithFiles(t, fixtureWithSecrets())
	s := newTestStore(t)
	ctx := context.Background()

	res, err := RunChecks(ctx, s, root, "")
	if err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	assertNoSecret(t, "CheckResult JSON", string(out))

	// Not vacuous: both credential gates produced something to inspect.
	gates := map[string]int{}
	for _, f := range res.Findings {
		gates[f.GateID]++
	}
	for _, g := range []string{"connection_strings", "secrets_scan"} {
		if gates[g] == 0 {
			t.Fatalf("gate %s reported nothing, so this test proves nothing about it (got %v)", g, gates)
		}
	}

	rows, err := s.db.QueryContext(ctx, `SELECT project, gate_id, severity, title, message, file_path, tags FROM findings`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var p, g, sev, title, msg, fp, tags string
		if err := rows.Scan(&p, &g, &sev, &title, &msg, &fp, &tags); err != nil {
			t.Fatal(err)
		}
		assertNoSecret(t, "store row "+g+" "+fp, strings.Join([]string{p, g, sev, title, msg, fp, tags}, "\x00"))
		n++
	}
	if n == 0 {
		t.Fatal("store is empty")
	}
}
