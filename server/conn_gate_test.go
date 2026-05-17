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

// Standard-body and spec hosts must never trigger http_remote.
func TestConnectionStrings_SpecHostsExempt(t *testing.T) {
	exemptURLs := []string{
		"http://www.w3.org/1999/xhtml",
		"http://schemas.xmlsoap.org/soap/envelope/",
		"http://schemas.microsoft.com/winfx/2006/xaml",
		"http://purl.org/dc/elements/1.1/",
		"http://www.ietf.org/rfc/rfc2616.txt",
		"http://tools.ietf.org/html/rfc7231",
		"http://xmlns.jcp.org/xml/ns/javaee",
		"http://dublincore.org/elements/1.1/",
		"http://docs.oasis-open.org/wss/2004/01/oasis-wss",
	}
	for _, url := range exemptURLs {
		t.Run(url, func(t *testing.T) {
			root := initRepoWithFiles(t, map[string]string{"schema.xml": url + "\n"})
			fs, err := checkConnectionStrings(context.Background(), root, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range fs {
				if strings.HasSuffix(f.FilePath, ":http_remote") {
					t.Errorf("spec host %q must be exempt, got: %+v", url, f)
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

// Template-style credentials (${VAR}, $VAR, %s, <name>, {{ var }}) are
// placeholders supplied at runtime, not committed secrets. The
// creds_in_url rule must stay quiet whenever the PASSWORD is a
// placeholder — the username is non-sensitive (account name, not a
// secret), so mixed forms like `postgresql://nodeapp:$DBPASS@host`
// also skip. The sensitive value is the password — if it's templated,
// nothing was leaked.
func TestConnectionStrings_CredsArePlaceholder(t *testing.T) {
	cases := []string{
		"postgresql://${PG_DB_USER}:${PG_DB_PASS}@localhost/${PG_DB_NAME}",
		"postgresql://$PG_DB_USER:$PG_DB_PASS@localhost:5432/db",
		"mongodb://%s:%s@host:27017",
		"https://${GITEA_USER}:${GITEA_TOKEN}@git.example.com/x.git",
		"redis://<user>:<pass>@cache.example.com:6379",
		"https://{{ user }}:{{ token }}@api.example.com",
		// Mixed: literal username, placeholder password — still a
		// template (the secret value comes from the environment).
		"postgresql://nodeapp:$DBPASS@localhost/nodeapp",
		"postgresql://alma:${DB_PASSWORD}@postgres:5432/alma",
		"https://admin:${PASS}@example.com",
	}
	for _, url := range cases {
		t.Run(url, func(t *testing.T) {
			if !credsArePlaceholder(url) {
				t.Errorf("expected placeholder, credsArePlaceholder=false: %s", url)
			}
			fs := scanConnectionLine("conf.txt", 1, []byte(url+"\n"))
			for _, f := range fs {
				if strings.HasSuffix(f.FilePath, ":creds_in_url") {
					t.Errorf("placeholder URL must not fire creds_in_url; got: %+v", f)
				}
				// Must not be downgraded to db_uri/http_remote either:
				// the span is claimed by creds_in_url.
				if strings.HasSuffix(f.FilePath, ":db_uri") || strings.HasSuffix(f.FilePath, ":http_remote") {
					t.Errorf("placeholder URL leaked to a lower-severity rule: %+v", f)
				}
			}
		})
	}
}

// A real credential URL (literal password) must still fire — the
// placeholder detection must not over-match. Username being literal
// is fine; the trigger is a literal password.
func TestConnectionStrings_RealCredsStillFire(t *testing.T) {
	cases := []string{
		"postgres://admin:hunter2@db.production.io:5432/app",
		"https://user:s3cret@api.example.com",
		"mongodb://root:passw0rd@10.0.0.5:27017",
		// Literal pass that LOOKS like a token but is not a
		// placeholder pattern.
		"https://api:sk-1234567890abcdef@hooks.example.com",
	}
	for _, url := range cases {
		t.Run(url, func(t *testing.T) {
			if credsArePlaceholder(url) {
				t.Errorf("real creds must NOT be classified as placeholder: %s", url)
			}
			fs := scanConnectionLine("conf.txt", 1, []byte(url+"\n"))
			matched := false
			for _, f := range fs {
				if strings.HasSuffix(f.FilePath, ":creds_in_url") {
					matched = true
				}
			}
			if !matched {
				t.Errorf("real creds_in_url must still fire: %s :: %+v", url, fs)
			}
		})
	}
}

// Data files (.csv/.jsonl/...) are payload-bearing — their addresses
// and URLs ARE the file's content. Default behaviour: skipped by
// content scanners.
func TestConnectionStrings_DataFilesSkippedByDefault(t *testing.T) {
	// Use api.acme.io — example.com is in the docs-host exempt list.
	root := initRepoWithFiles(t, map[string]string{
		"data/urls.csv": "id,url\n1,http://api.acme.io/v1\n2,ftp://leak.acme.io\n",
		"src/main.go":   "url := \"http://api.acme.io/v1\"\n",
	})
	fs, err := checkConnectionStrings(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fs {
		if strings.HasPrefix(f.FilePath, "data/urls.csv") {
			t.Errorf("default scan must not flag .csv content: %+v", f)
		}
	}
	// And the .go file must still produce findings.
	srcHit := false
	for _, f := range fs {
		if strings.HasPrefix(f.FilePath, "src/main.go") {
			srcHit = true
		}
	}
	if !srcHit {
		t.Errorf("expected .go source to still be scanned, got: %+v", fs)
	}
}

// Changelog-style files quote past behaviour, not current
// configuration. The connection_strings gate skips them outright,
// matching the network_scan policy.
func TestConnectionStrings_SkipsChangelogFiles(t *testing.T) {
	for _, name := range []string{"CHANGELOG.md", "HISTORY.md", "RELEASES.md", "CHANGES.md", "NEWS.md"} {
		t.Run(name, func(t *testing.T) {
			root := initRepoWithFiles(t, map[string]string{
				name:      "Switched from http://api.acme.io to https://api.acme.io\n",
				"conf.go": "url := \"http://api.acme.io/v1\"\n",
			})
			fs, err := checkConnectionStrings(context.Background(), root, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range fs {
				if strings.HasPrefix(f.FilePath, name) {
					t.Errorf("%s must be skipped, got: %+v", name, f)
				}
			}
			// Non-changelog files in the same repo must still fire.
			hit := false
			for _, f := range fs {
				if strings.HasPrefix(f.FilePath, "conf.go") {
					hit = true
				}
			}
			if !hit {
				t.Errorf("non-changelog must still fire, got: %+v", fs)
			}
		})
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

// Single-label hostnames (Docker/k8s service names) must never fire.
func TestConnectionStrings_SingleLabelHostExempt(t *testing.T) {
	for _, url := range []string{
		"http://kafka/health",
		"http://redis:6379",
		"http://db-primary/api",
		"http://elasticsearch:9200",
		"http://backend",
	} {
		t.Run(url, func(t *testing.T) {
			fs := scanConnectionLine("config.yml", 1, []byte(url))
			for _, f := range fs {
				if strings.HasSuffix(f.FilePath, ":http_remote") {
					t.Errorf("single-label host %q must be exempt, got finding: %+v", url, f)
				}
			}
		})
	}
}
