package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// Secrets the fixtures plant. None of them may appear anywhere in what a
// finding hands out, in any of its fields.
var plantedSecrets = []string{
	"Zq8!nRt4vLw2",  // creds_in_url password
	"Mx7&kP3dQ9yB",  // db_uri query password
	"V3cR9tL2hN8w",  // jdbc semicolon password
	"Tk5sB1mW7eH4",  // ftp password
	"p@ssW0rd@tail", // password containing an at sign
	"ghp_K8qM2xV9LpR4tZ7wYb3NcD6sFh1JgA5uE0xV", // a token-shaped string for secrets_scan
}

func fixtureWithSecrets() map[string]string {
	return map[string]string{
		"app.env":  "DATABASE_URL=postgres://svc:Zq8!nRt4vLw2@db.prod.acme.io:5432/app\n",
		"conf.ini": "url = mongodb://db.acme.io:27017/app?authSource=admin&password=Mx7&kP3dQ9yB\n",
		"db.conf":  "jdbc:sqlserver://db.acme.io;databaseName=app;password=V3cR9tL2hN8w;encrypt=true\n",
		"sync.sh":  "ftp://deploy:Tk5sB1mW7eH4@files.acme.io/dump.tar\n",
		"odd.txt":  "postgres://app:p@ssW0rd@tail@db.prod.acme.io/app\n",
		"notes.md": "token for CI: ghp_K8qM2xV9LpR4tZ7wYb3NcD6sFh1JgA5uE0xV\n",
	}
}

func assertNoSecret(t *testing.T, where, blob string) {
	t.Helper()
	for _, sec := range plantedSecrets {
		if strings.Contains(blob, sec) {
			t.Errorf("%s leaks a secret (%d chars, starts %q)", where, len(sec), sec[:3])
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
	if len(fs) < 4 {
		t.Fatalf("expected the fixtures to be reported, got %d findings: %+v", len(fs), fs)
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
// and every column of every row. This is the contract — a finding says where a
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
