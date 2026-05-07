package main

import (
	"context"
	"strings"
	"testing"
)

func TestConnectionStrings_PerScheme(t *testing.T) {
	cases := []struct {
		name      string
		content   string
		patternID string
		severity  string
	}{
		{"creds_in_url", "DSN = postgres://user:pass@db.example.com:5432/app\n", "creds_in_url", SeverityError},
		{"ftp", "url = ftp://files.example.com/dump.tar\n", "ftp", SeverityWarning},
		{"telnet", "url = telnet://router.example.com\n", "telnet", SeverityWarning},
		{"smb", "share = smb://fileserver/public\n", "smb", SeverityWarning},
		{"nfs", "mount = nfs://nas.example.com/exports/data\n", "nfs", SeverityWarning},
		{"rsync", "src = rsync://mirror.example.com/pub\n", "rsync", SeverityWarning},
		{"ldap_unencrypted", "url = ldap://dc.corp.local\n", "ldap_unencrypted", SeverityInfo},
		{"db_uri", "url = mongodb://db.internal:27017\n", "db_uri", SeverityInfo},
		{"jdbc", "url = jdbc:postgresql://db.internal/app\n", "jdbc", SeverityInfo},
		{"http_remote", "see http://api.production.acme.io/v1\n", "http_remote", SeverityInfo},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := initRepoWithFiles(t, map[string]string{"conf.txt": tc.content})
			fs, err := checkConnectionStrings(context.Background(), root, nil)
			if err != nil {
				t.Fatal(err)
			}
			matched := false
			for _, f := range fs {
				if f.Severity == tc.severity && strings.HasSuffix(f.FilePath, ":"+tc.patternID) {
					matched = true
					break
				}
			}
			if !matched {
				t.Fatalf("no finding matched (severity=%s, pattern=%s); got: %+v",
					tc.severity, tc.patternID, fs)
			}
		})
	}
}

// http://localhost / http://127.0.0.1 / http://example.com etc. must not
// fire — these are dev-environment / RFC-doc hosts, expected literal noise.
func TestConnectionStrings_HTTPLocalExempt(t *testing.T) {
	exempt := []string{
		"http://localhost:8080/",
		"http://127.0.0.1:5000",
		"http://0.0.0.0:9000",
		"http://10.0.0.5/",
		"http://192.168.1.1:80",
		"http://172.16.0.1/",
		"http://example.com",
		"http://api.example.com",
		"http://thing.test",
		"http://ext.localhost",
		"http://router.local/admin",
	}
	for _, url := range exempt {
		t.Run(url, func(t *testing.T) {
			root := initRepoWithFiles(t, map[string]string{"conf.txt": url + "\n"})
			fs, err := checkConnectionStrings(context.Background(), root, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range fs {
				if strings.HasSuffix(f.FilePath, ":http_remote") {
					t.Errorf("URL %q should be exempt, but produced %+v", url, f)
				}
			}
		})
	}
}

// creds_in_url claims its span first; the lower-severity rule that would
// otherwise also match (db_uri / http_remote) must NOT emit a duplicate.
func TestConnectionStrings_CredsInURLDeduplicates(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		"conf.txt": "DSN = postgres://user:hunter2@db.production.io:5432/app\n",
	})
	fs, err := checkConnectionStrings(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(fs), fs)
	}
	if !strings.HasSuffix(fs[0].FilePath, ":creds_in_url") {
		t.Errorf("expected creds_in_url, got: %+v", fs[0])
	}
}

func TestConnectionStrings_NotGitRepo(t *testing.T) {
	fs, err := checkConnectionStrings(context.Background(), t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 || fs[0].Severity != SeverityInfo {
		t.Errorf("expected one info skip, got: %+v", fs)
	}
}
