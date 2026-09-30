package main

import (
	"strings"
	"testing"
)

func TestRedactSecrets_MasksTheCredential(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"userinfo password", "postgres://admin:S3cr3tPw@db.prod.io:5432/app", "postgres://admin:***@db.prod.io:5432/app"},
		{"empty user (redis)", "redis://:hunter2x@cache:6379/0", "redis://:***@cache:6379/0"},
		{"at sign in password", "https://user:p@ss@host.io/x", "https://user:***@host.io/x"},
		{"hash in password", "postgres://user:p#ssw0rd@host/db", "postgres://user:***@host/db"},
		{"question mark in password", "postgres://user:p?ss9@host/db", "postgres://user:***@host/db"},
		{"token as the whole user", "https://ghp_Ab12Cd34Ef56Gh78Ij90Kl12@github.com/o/r.git", "https://***@github.com/o/r.git"},
		{"jdbc query password", "jdbc:mysql://h/db?user=a&password=Zz9x&useSSL=true", "jdbc:mysql://h/db?user=a&password=***&useSSL=true"},
		{"jdbc semicolon pwd", "jdbc:sqlserver://h;databaseName=x;pwd=Zz9x;encrypt=true", "jdbc:sqlserver://h;databaseName=x;pwd=***;encrypt=true"},
		{"api key query", "https://api.acme.io/v1?api_key=AKfoo123&x=1", "https://api.acme.io/v1?api_key=***&x=1"},
		{"prefixed param name", "https://x.io/?db_password=abc123&a=b", "https://x.io/?db_password=***&a=b"},
		{"inside a sentence", "postgres://a:b9b9b9@h in f.sh:3. Remove the inline user:password.", "postgres://a:***@h in f.sh:3. Remove the inline user:password."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := redactSecrets(tc.in); got != tc.want {
				t.Errorf("redactSecrets(%q)\n got  %q\n want %q", tc.in, got, tc.want)
			}
			// Idempotent: redacting twice changes nothing more.
			once := redactSecrets(tc.in)
			if twice := redactSecrets(once); twice != once {
				t.Errorf("not idempotent: %q -> %q", once, twice)
			}
		})
	}
}

// The property the feature exists for: whatever the password looks like, it
// does not survive. A fixed-output table cannot prove that for the awkward
// shapes, so every secret here is checked by absence.
func TestRedactSecrets_NoSecretSurvives(t *testing.T) {
	secrets := []string{
		"hunter2", "p@ss", "p@ss@more", "a:b", "p#ss", "p?ss", "pa$$w0rd", "x%40y", "Xk9$mQ2!vL",
		"пароль9", "…tail9", "Tr0ub4dor&3", "s3cr3t.with.dots", "__init9__", "a+b=c9", "p;q9",
	}
	shapes := []string{
		"postgres://admin:%s@db.prod.io:5432/app",
		"redis://:%s@cache:6379/0",
		"https://api:%s@api.example.com/v1/x",
		"mongodb://root:%s@10.0.0.5:27017",
		"amqp://guest:%s@mq.internal",
		"ftp://u:%s@files.acme.io/dump.tar",
		"see https://u:%s@host.io/path and more text",
	}
	for _, sec := range secrets {
		// The authority ends at `/`, so a slash in a password is the one shape
		// the URL grammar cannot carry — the creds_in_url rule refuses it too.
		for _, sh := range shapes {
			in := strings.Replace(sh, "%s", sec, 1)
			got := redactSecrets(in)
			if strings.Contains(got, sec) {
				t.Errorf("secret %q survived in %q -> %q", sec, in, got)
			}
			if !strings.Contains(got, redactedMark) {
				t.Errorf("nothing was masked in %q -> %q", in, got)
			}
		}
	}
}

// The other axis: text that holds no credential must come back byte for byte.
// A redactor that mangles ordinary URLs would be switched off by the first
// person it annoyed.
func TestRedactSecrets_LeavesOrdinaryTextAlone(t *testing.T) {
	unchanged := []string{
		"",
		"no url and no equals sign here",
		"ssh://git@github.com/o/r.git",
		"ssh://administrator_account_name@host.io", // long, but no digit: a username
		"postgres://user@host:5432/db",             // user without a password
		"http://localhost:8080/health",
		"https://example.com/docs?page=2&sort=asc",
		"https://example.com/contact/someone@example.com", // `@` in the PATH, not the userinfo
		"git@github.com:o/r.git",
		"mailto:someone@example.com",
		"`SECRET_KEY=` has no comment",
		"DEBUG=1 and LOG_LEVEL=info",
		"postgres://admin:***@db.prod.io:5432/app", // already redacted
		"https://a.b/c?d=e#frag",
		"foo.go:12 uses a == b",
	}
	for _, in := range unchanged {
		if got := redactSecrets(in); got != in {
			t.Errorf("ordinary text was altered:\n in  %q\n out %q", in, got)
		}
	}
}
