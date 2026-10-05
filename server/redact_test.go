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
		// the URL grammar cannot carry: the creds_in_url rule refuses it too.
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

// An independent review found each of these by construction; every case below
// leaked or corrupted text in the version that first shipped the redaction.

// A list of URLs with no path between them: the authority of the first ran into
// the scheme of the second, so only the first password was masked.
func TestRedactSecrets_EveryURLInAList(t *testing.T) {
	cases := []struct{ in, want string }{
		{"BROKERS=amqp://svcuser:pw1Secret@h1.acme.io:5672,amqp://svcuser2:pw2Secret@h2.acme.io:5672",
			"BROKERS=amqp://svcuser:***@h1.acme.io:5672,amqp://svcuser2:***@h2.acme.io:5672"},
		{"a://u:p1x@h1;a://u2:p2x@h2", "a://u:***@h1;a://u2:***@h2"},
		{"a://u:p1x@h1|a://u2:p2x@h2", "a://u:***@h1|a://u2:***@h2"},
		// A first URL with no userinfo must not hide the second.
		{"redis://h1:6379,redis://:pw9Zq@h2:6379", "redis://h1:6379,redis://:***@h2:6379"},
		// A URL inside another URL's query.
		{"https://u:pw1@host.io?next=http://v:pw2@other.io/x", "https://u:***@host.io?next=http://v:***@other.io/x"},
	}
	for _, tc := range cases {
		if got := redactSecrets(tc.in); got != tc.want {
			t.Errorf("\n in   %q\n got  %q\n want %q", tc.in, got, tc.want)
		}
	}
}

// The gate's password class accepts `<` and `>`, so the redactor's authority
// must too: otherwise the authority ends before the `@` and nothing is masked.
func TestRedactSecrets_AngleBracketsInAPassword(t *testing.T) {
	in := "u = postgres://user:a<b>c9Zz@host.acme.io/db"
	got := redactSecrets(in)
	if strings.Contains(got, "a<b>c9Zz") || strings.Contains(got, "c9Zz") {
		t.Errorf("password with angle brackets survived: %q", got)
	}
}

// Oracle thin JDBC has no `://` and no `=`, so a fast path returned it untouched.
func TestRedactSecrets_OracleThinJDBC(t *testing.T) {
	for _, in := range []string{
		"jdbc:oracle:thin:scott/tiger9Zq@host.acme.io:1521:orcl",
		"url=jdbc:oracle:thin:scott/tiger9Zq@//host.acme.io:1521/orcl",
		"JDBC:ORACLE:OCI:scott/tiger9Zq@host",
	} {
		got := redactSecrets(in)
		if strings.Contains(got, "tiger9Zq") {
			t.Errorf("Oracle JDBC password survived: %q -> %q", in, got)
		}
		if !strings.Contains(got, "scott/"+redactedMark+"@") {
			t.Errorf("the user should stay and the password be masked: %q -> %q", in, got)
		}
	}
	// No credentials in the URL: untouched.
	if in := "jdbc:oracle:thin:@//host.acme.io:1521/orcl"; redactSecrets(in) != in {
		t.Errorf("a credential-free Oracle URL must not change: %q", redactSecrets(in))
	}
}

// Credential spellings the first vocabulary missed, and delimiters it did not
// know. Each used to come out unchanged from a db_uri / http_remote / jdbc message.
func TestRedactSecrets_ParameterSpellingsAndDelimiters(t *testing.T) {
	for _, in := range []string{
		"mongodb://db.acme.io/db?user=a&pass=hunter2Zq",
		"mongodb://db.acme.io/db?user=a&pw=hunter2Zq",
		"mongodb://db.acme.io/db?user=a&pswd=hunter2Zq",
		"mongodb://db.acme.io/db?user=a&psw=hunter2Zq",
		"ldap://ldap.acme.io/?bindpw=hunter2Zq",
		"x://h/?authpass=hunter2Zq",
		"x://h/?userpass=hunter2Zq",
		"http://host.io/cb#access_token=hunter2Zq",
		"http://host.io/cb#password=hunter2Zq",
		"x://h/?user=a,password=hunter2Zq",
		"x://h/?a=b/password=hunter2Zq",
		"password=hunter2Zq at the start of a string",
	} {
		if got := redactSecrets(in); strings.Contains(got, "hunter2Zq") {
			t.Errorf("credential parameter survived: %q -> %q", in, got)
		}
	}
	// A value that itself contains a comma, or braces (JDBC escapes `;` with them).
	for in, leak := range map[string]string{
		"x://h/?password=ab,cd9Zq&u=1":                                "cd9Zq",
		"jdbc:sqlserver://h;user=sa;password={ab;cd9Zq};encrypt=true": "cd9Zq",
	} {
		if got := redactSecrets(in); strings.Contains(got, leak) {
			t.Errorf("part of the value survived: %q -> %q", in, got)
		}
	}
	// The comma still ends a value when a NEW parameter follows it.
	if got := redactSecrets("x://h/?password=zz9Qx,foo=bar"); got != "x://h/?password=***,foo=bar" {
		t.Errorf("a comma before another parameter must end the value: %q", got)
	}
}

// An `@` in a key=value QUERY of a URL that has no path must not be taken for the
// end of the userinfo: that masked the host and rewrote stored rows with the
// damage. (`https://host.io:8080?e=a@b.c`, no credential at all, is masked as
// `host.io:***@b.c`: with nothing before the `?` to end a userinfo, the password
// reading wins, because the other one hides `8675309#Secret`.)
func TestRedactSecrets_AtSignInTheQueryDoesNotCorruptTheHost(t *testing.T) {
	for in, want := range map[string]string{
		"https://user:pw@host.acme.io?x=a@b": "https://user:***@host.acme.io?x=a@b",
		"https://host.io?e=a@b.c":            "https://host.io?e=a@b.c",
		"https://user:p#ss@host.acme.io/db":  "https://user:***@host.acme.io/db",
		"https://user:p?ss@host.acme.io/db":  "https://user:***@host.acme.io/db",
		"https://user:p?ss@host.acme.io":     "https://user:***@host.acme.io",
	} {
		if got := redactSecrets(in); got != want {
			t.Errorf("\n in   %q\n got  %q\n want %q", in, got, want)
		}
	}
}
