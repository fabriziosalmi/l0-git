package main

import (
	"regexp"
	"strings"
)

// redactedMark stands in for the secret part of a value. It is deliberately
// the same shape as the `***` placeholder the credential gates already treat
// as a stand-in, so a redacted message can never be re-read as a real secret.
const redactedMark = "***"

// redactSecrets masks the credential material inside a string that is about
// to be shown, stored or sent: the password of a `scheme://user:password@host`
// URL, a token used as the whole user (`https://<token>@host`), the password of
// an Oracle thin-driver JDBC URL (`user/password@host` after the driver prefix), and the value of a
// credential-looking parameter (`?password=…`, `;pwd=…`, `#access_token=…`).
//
// Findings are persisted in a SQLite file, printed by the CLI, returned
// verbatim over MCP and quoted by the editor (Problems pane, hover, the
// "ask Claude" prompt). A finding exists to say WHERE a secret is, never WHAT
// it is: echoing the value turned every credential the scanner found into a
// second copy, kept even after the original was removed from the repository.
//
// The function is idempotent and leaves text without a credential unchanged.
//
// Two known limits, both deliberate: a password that contains a `/` is not
// masked on the patterns that do not themselves reject it (a URL cannot carry
// one, so no driver accepts it), and a bare user that is short or has no digit
// (`ssh://git@host`) is treated as a username, not a token.
func redactSecrets(s string) string {
	if s == "" {
		return s
	}
	lower := strings.ToLower(s)
	if !strings.Contains(s, "://") && !strings.Contains(s, "=") && !strings.Contains(lower, "jdbc:") {
		return s
	}
	s = redactURLUserinfo(s)
	s = redactOracleJDBC(s)
	return redactSecretParams(s)
}

// schemeRe finds the `scheme://` that opens a URL.
var schemeRe = regexp.MustCompile(`\b[a-zA-Z][a-zA-Z0-9+\-.]*://`)

// redactURLUserinfo masks the password in every URL of s.
//
// Every `scheme://` is handled on its own, and its authority ends at the first
// `/`, whitespace or quote. In a list with no path between its members
// (`amqp://u:p@h1:5672,amqp://u2:p2@h2:5672`) the first authority therefore ends
// at the `//` of the second URL, and the second is found by its own scheme — a
// single pass that let one match swallow the next left every password after the
// first in the clear. Angle brackets are NOT a boundary, because the
// creds_in_url rule accepts them inside a password.
func redactURLUserinfo(s string) string {
	locs := schemeRe.FindAllStringIndex(s, -1)
	if len(locs) == 0 {
		return s
	}
	var b strings.Builder
	last := 0
	for _, loc := range locs {
		start := loc[1]
		if start < last {
			continue
		}
		end := start
		for end < len(s) && !isAuthorityEnd(s[end]) {
			end++
		}
		b.WriteString(s[last:start])
		b.WriteString(redactAuthority(s[start:end]))
		last = end
	}
	b.WriteString(s[last:])
	return b.String()
}

func isAuthorityEnd(c byte) bool {
	return c == '/' || c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '"' || c == '\''
}

// redactAuthority masks the secret in one authority (`user:pass@host:port`).
func redactAuthority(a string) string {
	at := userinfoEnd(a)
	if at < 0 {
		return a
	}
	userinfo, rest := a[:at], a[at+1:]
	if colon := strings.IndexByte(userinfo, ':'); colon >= 0 {
		return userinfo[:colon] + ":" + redactedMark + "@" + rest
	}
	// A bare userinfo is normally just a username (`ssh://git@host`). It is the
	// credential itself when it is a long token-shaped string.
	if looksLikeBareToken(userinfo) {
		return redactedMark + "@" + rest
	}
	return a
}

// userinfoEnd returns the index of the `@` that ends the userinfo of the
// authority that starts rest — the text after `scheme://` — or -1 when there is
// none. It is the one place that decides where a password stops, used by the
// scanner (to read the password) and by the redactor (to mask it), because two
// definitions disagree exactly on the URLs that matter.
//
// The userinfo ends at the LAST `@`: the first one is wrong for a password that
// itself contains an `@` (`user:p@ss@host`), and stopping there both leaves the
// tail of the password in the clear after masking and, in the scanner, reads the
// password as `p` and drops the URL as two-character prose shorthand.
//
// But a `?` or `#` may start a QUERY, and an `@` in a query
// (`https://host.io?e=a@b.c`) is not the end of any userinfo. So the last `@`
// BEFORE the first `?`/`#` wins when there is one. Only when there is none is
// the whole authority read as userinfo — the creds_in_url rule does accept `?`
// and `#` inside a password (`user:p?ss@host`) — and then only when the part
// before the `?` has a colon that is not a port (`host.io:8080?e=a@b.c`).
func userinfoEnd(rest string) int {
	a := rest
	if i := strings.IndexByte(a, '/'); i >= 0 {
		a = a[:i]
	}
	head := a
	q := strings.IndexAny(a, "?#")
	if q >= 0 {
		head = a[:q]
	}
	at := strings.LastIndexByte(head, '@')
	if at < 0 && q >= 0 {
		colon := strings.LastIndexByte(head, ':')
		if colon < 0 || isAllDigits(head[colon+1:]) {
			return -1
		}
		at = strings.LastIndexByte(a, '@')
	}
	return at
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

var (
	bareTokenRe   = regexp.MustCompile(`^[A-Za-z0-9_\-.~%]+$`)
	hasLetterRe   = regexp.MustCompile(`[A-Za-z]`)
	hasDigitRe    = regexp.MustCompile(`[0-9]`)
	bareTokenMinN = 20
)

func looksLikeBareToken(s string) bool {
	return len(s) >= bareTokenMinN && bareTokenRe.MatchString(s) &&
		hasLetterRe.MatchString(s) && hasDigitRe.MatchString(s)
}

// oracleJDBCRe matches the thin/OCI JDBC form that carries its credentials as
// `user/password@host` — no `://` and no `=`, which is why nothing else sees it.
var oracleJDBCRe = regexp.MustCompile(`(?i)(jdbc:[a-z0-9]+:(?:thin|oci8?):)([^/\s@:]+)/([^@\s]+)@`)

func redactOracleJDBC(s string) string {
	return oracleJDBCRe.ReplaceAllString(s, "${1}${2}/"+redactedMark+"@")
}

// secretParamRe matches the start of a `name=` parameter whose name says it is a
// credential: the delimiter before it (a separator, a fragment mark, a path or
// query delimiter, whitespace, or the start of the text) and the name itself.
//
// Over-matching is harmless here — this only ever rewrites message text, never
// detection — and it is the safe direction: `primary_key=` being masked costs
// nothing, `db_password=` being missed leaks. A prefix is allowed before the
// keyword so that `bindpw=`, `authpass=` and `X-Amz-Signature=` are caught.
var secretParamRe = regexp.MustCompile(
	`(?i)(^|[?&;#,:/(]|\s)` +
		`([A-Za-z0-9_.\-]*` +
		`(?:password|passwd|pwd|passphrase|pass|pswd|psw|pw|secret|token|apikey|api_key|api-key|` +
		`signature|credentials?|authorization|auth|sig|sas|key)=)`)

// anotherParamRe recognises `,name=` — a comma that ends a value because a NEW
// parameter follows it, as opposed to a comma inside the value itself.
var anotherParamRe = regexp.MustCompile(`^,[A-Za-z0-9_.\-]+=`)

// redactSecretParams masks the VALUE of every credential-looking parameter. The
// value runs to whatever ends a URL parameter — `&`, `;`, whitespace, a quote, a
// bracket or backtick — but a comma only ends it when another parameter
// follows, and a `{…}` value (JDBC's way of writing a value that contains `;`)
// is taken whole.
func redactSecretParams(s string) string {
	locs := secretParamRe.FindAllStringSubmatchIndex(s, -1)
	if len(locs) == 0 {
		return s
	}
	var b strings.Builder
	last := 0
	for _, loc := range locs {
		valueStart := loc[1] // just after the `=`
		if valueStart < last {
			continue
		}
		valueEnd := paramValueEnd(s, valueStart)
		if valueEnd == valueStart {
			continue
		}
		b.WriteString(s[last:valueStart])
		b.WriteString(redactedMark)
		last = valueEnd
	}
	b.WriteString(s[last:])
	return b.String()
}

func paramValueEnd(s string, i int) int {
	if i < len(s) && s[i] == '{' {
		if end := strings.IndexByte(s[i:], '}'); end >= 0 {
			return i + end + 1
		}
	}
	j := i
	for j < len(s) {
		c := s[j]
		if c == ',' {
			if anotherParamRe.MatchString(s[j:]) {
				break
			}
			j++
			continue
		}
		switch c {
		case '&', ';', ' ', '\t', '\n', '\r', '"', '\'', '<', '>', ')', ']', '`':
			return j
		}
		j++
	}
	return j
}
