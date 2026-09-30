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
// URL, a token used as the whole user (`https://<token>@host`), and the value
// of a credential-looking query or JDBC parameter (`?password=…`, `;pwd=…`).
//
// Findings are persisted in a SQLite file, printed by the CLI, returned
// verbatim over MCP and quoted by the editor (Problems pane, hover, the
// "ask Claude" prompt). A finding exists to say WHERE a secret is, never WHAT
// it is: echoing the value turned every credential the scanner found into a
// second copy, kept even after the original was removed from the repository.
//
// The function is idempotent and leaves text without a credential unchanged.
func redactSecrets(s string) string {
	if s == "" || (!strings.Contains(s, "://") && !strings.Contains(s, "=")) {
		return s
	}
	return redactSecretParams(redactURLUserinfo(s))
}

// urlAuthorityRe finds `scheme://authority`. The authority ends at the first
// `/` or at whitespace / a quote / an angle bracket. It deliberately does NOT
// end at `?` or `#`: RFC 3986 says those must be percent-encoded in a password,
// but the creds_in_url rule accepts them (`user:p#ss@host`), so a redaction
// that stopped there would leave the tail of exactly the passwords the gate
// reported in the clear.
var urlAuthorityRe = regexp.MustCompile(`\b([a-zA-Z][a-zA-Z0-9+\-.]*://)([^\s/"'<>]+)`)

func redactURLUserinfo(s string) string {
	return urlAuthorityRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := urlAuthorityRe.FindStringSubmatch(m)
		scheme, authority := sub[1], sub[2]
		// The userinfo ends at the LAST `@`. The first one is wrong for a
		// password that itself contains an `@` (`user:p@ss@host`): stopping
		// there would leave the tail of the password in the clear and call it
		// the host.
		at := strings.LastIndexByte(authority, '@')
		if at < 0 {
			return m
		}
		userinfo, host := authority[:at], authority[at+1:]
		if colon := strings.IndexByte(userinfo, ':'); colon >= 0 {
			return scheme + userinfo[:colon] + ":" + redactedMark + "@" + host
		}
		// A bare userinfo is normally just a username (`ssh://git@host`). It
		// is the credential itself when it is a long token-shaped string.
		if looksLikeBareToken(userinfo) {
			return scheme + redactedMark + "@" + host
		}
		return m
	})
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

// secretParamRe matches `name=value` where the name says it is a credential.
// The value stops at whatever ends a URL parameter, or at a backtick / bracket
// so that prose like "`SECRET_KEY=` has no comment" is left alone.
//
// Over-matching is harmless here — this only ever rewrites message text, never
// detection — and it is the safe direction: `primary_key=` being masked costs
// nothing, `db_password=` being missed leaks.
var secretParamRe = regexp.MustCompile(
	`(?i)([?&;]|\s)([A-Za-z0-9_.\-]*(?:password|passwd|pwd|passphrase|secret|token|apikey|api_key|api-key|signature|credential|credentials|authorization|auth|sig|sas|key)=)([^&\s"'<>;)\]` + "`" + `,]+)`)

func redactSecretParams(s string) string {
	return secretParamRe.ReplaceAllString(s, "${1}${2}"+redactedMark)
}
