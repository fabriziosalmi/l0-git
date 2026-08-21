package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// connectionPattern is one rule for the connection_strings gate. We tier
// by what's actually risky:
//   - credsInline   — error: any scheme with user:pass@ host
//   - legacy/cleartext schemes (ftp, telnet, smb, nfs, rsync) — warning
//   - DB schemes (mongodb, postgres, …) — info
//   - http://non-local, ldap://, imap:// (vs encrypted variants) — info
type connectionPattern struct {
	id       string
	severity string
	title    string
	advice   string
	re       *regexp.Regexp
}

// Important: the credentials-in-URL regex must run first so it claims the
// match before the "plain scheme" patterns flag the same line a second
// time at lower severity.
var connectionPatterns = []connectionPattern{
	{
		id:       "creds_in_url",
		severity: SeverityError,
		title:    "Credentials in connection URL",
		advice:   "Remove the inline user:password from the URL — read it from a vault, env var, or secret manager instead. Also rotate, since the URL has been committed.",
		re:       regexp.MustCompile(`\b[a-zA-Z][a-zA-Z0-9+\-.]*://[^\s/@:"']+:[^\s/@"']+@[^\s"']+`),
	},
	{
		id:       "ftp",
		severity: SeverityWarning,
		title:    "Cleartext FTP URL",
		advice:   "FTP is unauthenticated and unencrypted. Switch to SFTP (over SSH) or HTTPS.",
		re:       regexp.MustCompile(`\bftp://[^\s"'<>]+`),
	},
	{
		id:       "telnet",
		severity: SeverityWarning,
		title:    "Telnet URL",
		advice:   "Telnet sends everything (including credentials) in cleartext. Use SSH instead.",
		re:       regexp.MustCompile(`\btelnet://[^\s"'<>]+`),
	},
	{
		id:       "smb",
		severity: SeverityWarning,
		title:    "SMB URL",
		advice:   "SMB shares in source code usually mean a hardcoded share path; review whether it should be config-driven.",
		re:       regexp.MustCompile(`\bsmb://[^\s"'<>]+`),
	},
	{
		id:       "nfs",
		severity: SeverityWarning,
		title:    "NFS URL",
		advice:   "NFS exports embedded in code tie the project to specific infrastructure.",
		re:       regexp.MustCompile(`\bnfs://[^\s"'<>]+`),
	},
	{
		id:       "rsync",
		severity: SeverityWarning,
		title:    "rsync URL",
		advice:   "rsync:// is plain TCP. Prefer rsync over SSH (`rsync user@host:`).",
		re:       regexp.MustCompile(`\brsync://[^\s"'<>]+`),
	},
	{
		id:       "ldap_unencrypted",
		severity: SeverityInfo,
		title:    "Unencrypted LDAP URL",
		advice:   "ldap:// is unencrypted; ldaps:// or StartTLS is the modern default.",
		re:       regexp.MustCompile(`\bldap://[^\s"'<>]+`),
	},
	// jdbc must run before db_uri: a JDBC URL like
	// `jdbc:postgresql://host/db` contains a substring that db_uri would
	// otherwise claim first, leaving the more specific finding squashed.
	{
		id:       "jdbc",
		severity: SeverityInfo,
		title:    "JDBC connection string",
		advice:   "JDBC URLs sometimes embed credentials inline — double-check this one isn't doing that.",
		re:       regexp.MustCompile(`\bjdbc:[a-z0-9]+:[^\s"'<>]+`),
	},
	{
		id:       "db_uri",
		severity: SeverityInfo,
		title:    "Database connection URI",
		advice:   "Database URIs in source are usually fine when the host/credentials come from env, but worth checking.",
		re:       regexp.MustCompile(`\b(?:mongodb(?:\+srv)?|postgres(?:ql)?|mysql|mariadb|redis|amqp|kafka|sqlserver|mssql|couchdb|cassandra|cql):\/\/[^\s"'<>]+`),
	},
	{
		id:       "http_remote",
		severity: SeverityInfo,
		title:    "Cleartext HTTP URL (non-local)",
		advice:   "Plain http:// to a real host means man-in-the-middle exposure. Use https:// unless this is intentional (talking to a captive portal, an embedded device, …).",
		// RE2 has no lookarounds, so we match all http:// URLs and filter
		// out local/doc hosts in scanConnectionLine.
		re: regexp.MustCompile(`\bhttp://[^\s"'<>]+`),
	},
}

// httpHostExempt returns true when the host portion of an http:// URL is
// one we shouldn't bother flagging (local dev, RFC docs, internal
// reserved suffixes).
func httpHostExempt(url string) bool {
	return urlHostExempt(strings.TrimPrefix(url, "http://"))
}

// urlHostExempt takes the post-scheme remainder of a URL and reports whether
// its host is one we shouldn't flag: local dev, RFC1918/link-local, an
// RFC-reserved documentation domain, a single-label container/service name,
// or a standards-body host.
func urlHostExempt(rest string) bool {
	end := len(rest)
	for i, c := range rest {
		if c == '/' || c == ':' || c == '?' || c == '#' {
			end = i
			break
		}
	}
	host := strings.ToLower(rest[:end])
	if host == "" {
		return true
	}
	if host == "localhost" || host == "0.0.0.0" || host == "::1" {
		return true
	}
	// Reserved-range tests run on a PARSED address, never on a string prefix.
	// `strings.HasPrefix(host, "10.")` also accepts `10.example.com`, and
	// `"100."` accepts `100.64.123.evil` — a public hostname whose cleartext
	// URL would then be silently exempt.
	if ip := net.ParseIP(host); ip != nil {
		switch {
		case ip.IsLoopback(), ip.IsPrivate(), ip.IsUnspecified():
			return true
		case ip.IsLinkLocalUnicast():
			// 169.254.0.0/16: the cloud metadata endpoint (169.254.169.254)
			// is http-only by design and unreachable off-host, so "cleartext
			// HTTP MITM" never applies.
			return true
		}
		// 100.64.0.0/10 — RFC 6598 shared address space, which is what
		// Tailscale hands out. Such a URL is reachable only from inside the
		// tailnet, so cleartext there is no more exposed than loopback;
		// network_scan already treats the range the same way.
		if v4 := ip.To4(); v4 != nil && v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
			return true
		}
	}
	if host == "example.com" || strings.HasSuffix(host, ".example.com") ||
		strings.HasSuffix(host, ".test") || strings.HasSuffix(host, ".localhost") ||
		strings.HasSuffix(host, ".invalid") || strings.HasSuffix(host, ".local") ||
		host == "internal" || strings.HasSuffix(host, ".internal") {
		// `.internal` is ICANN-reserved (2024) for private use — a
		// service.internal / pushgateway.internal host resolves only inside
		// the cluster, never on the public internet.
		return true
	}
	// Single-label hostnames (no dot) are never reachable on the public
	// internet — they resolve only in private DNS (Docker service names,
	// Kubernetes cluster-internal names, /etc/hosts entries, …).
	// Flagging http://kafka or http://db-primary as "cleartext HTTP" is
	// pure noise in every containerised project.
	if !strings.Contains(host, ".") {
		return true
	}
	// Well-known specification / standard-body hosts whose URIs appear
	// routinely in documentation, XML namespaces, and MIME type registries.
	// These are never operational URLs — flagging them is pure noise.
	for _, exempt := range httpSpecHosts {
		if host == exempt || strings.HasSuffix(host, "."+exempt) {
			return true
		}
	}
	return false
}

// httpSpecHosts is the closed list of standard-body and well-known
// documentation hosts whose http:// URIs should never trigger a finding.
var httpSpecHosts = []string{
	"www.w3.org",
	"w3.org",
	"www.ietf.org",
	"ietf.org",
	"tools.ietf.org",
	"datatracker.ietf.org",
	"www.rfc-editor.org",
	"rfc-editor.org",
	"schemas.xmlsoap.org",
	"schemas.microsoft.com",
	"schemas.openxmlformats.org",
	"xmlns.jcp.org",
	"java.sun.com",
	"purl.org",
	"dublincore.org",
	"www.dublincore.org",
	"docs.oasis-open.org",
	"www.oasis-open.org",
	"ogc.org",
	"www.ogc.org",
	"opengis.net",
	"www.opengis.net",
	// DOI resolvers: a `http://dx.doi.org/10.18653/...` in a bibliography is
	// a persistent identifier, not a service the project talks to.
	"doi.org",
	"dx.doi.org",
}

// bareURLRe matches a line that is exactly one URL: a scheme, "://", then a
// run of non-space characters to end of line. Whitespace anywhere disqualifies
// it (that would be a sentence mentioning a URL, not a list entry).
var bareURLRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+\-.]*://\S+$`)

// isBareURL reports whether s is a single URL token with no surrounding text.
func isBareURL(s string) bool {
	return bareURLRe.MatchString(strings.TrimSpace(s))
}

// looksLikeURLList reports whether data is a line-oriented list of bare URLs
// (a feed dump, seed list, crawl frontier) rather than source that embeds the
// occasional link.
func looksLikeURLList(data []byte) bool {
	return looksLikeListFile(data, isBareURL)
}

func atoiSafe(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return -1
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func checkConnectionStrings(ctx context.Context, root string, opts json.RawMessage) ([]Finding, error) {
	if skip, stop := requireGitRepo(root, "connection_strings",
		"Initialize git or run gates from inside a clone — this gate scans tracked files only."); stop {
		return skip, nil
	}
	files, err := gitLsFiles(ctx, root)
	if err != nil {
		return []Finding{{
			Severity: SeverityWarning,
			Title:    "connection_strings failed",
			Message:  fmt.Sprintf("Could not enumerate tracked files: %v", err),
			FilePath: ".git",
		}}, nil
	}

	scan := parseScanOptions(opts)
	out := []Finding{}
	for _, rel := range files {
		if scan.shouldSkipContent(rel) {
			continue
		}
		// Changelog / release-note files describe the history of the
		// project — http:// links, FTP mirrors, and connection strings
		// listed there are quotations of past behaviour, not current
		// configuration. Same rationale as network_scan.
		if isChangelogBasename(filepath.Base(rel)) {
			continue
		}
		// Licence texts are verbatim boilerplate. Every Apache-2.0 file in
		// existence carries `http://www.apache.org/licenses/`, and no author
		// may edit it.
		if isLicenseBasename(filepath.Base(rel)) {
			continue
		}
		// Detection-rule files carry the address / URL as the rule's payload.
		if isDetectionRuleFile(rel) {
			continue
		}
		abs := filepath.Join(root, rel)
		info, err := os.Stat(abs)
		if err != nil || info.IsDir() {
			continue
		}
		if info.Size() > secretsMaxFileSize {
			continue
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		if isBinary(data) {
			continue
		}
		// A file whose lines are overwhelmingly bare URLs is a link list
		// (feed dump, seed list, crawl frontier) — the URLs ARE the payload,
		// so every line is a self-evident FP. Same heuristic shape and knob
		// as network_scan's address-list detection.
		if skipEnabled(scan.SkipDefaultDataFiles) && looksLikeURLList(data) {
			continue
		}

		line := 1
		start := 0
		emit := func(content []byte, lineNum int) {
			out = append(out, scanConnectionLine(rel, lineNum, content)...)
		}
		for i := 0; i < len(data); i++ {
			if data[i] == '\n' {
				emit(data[start:i], line)
				line++
				start = i + 1
			}
		}
		if start < len(data) {
			emit(data[start:], line)
		}
	}
	return out, nil
}

// scanConnectionLine runs each pattern in declaration order and dedupes:
// a single byte range can only produce one finding, claimed by the first
// matching pattern (which by ordering is the highest-severity one).
func scanConnectionLine(rel string, lineNum int, content []byte) []Finding {
	out := []Finding{}
	claimed := []claimedSpan{}
	for _, p := range connectionPatterns {
		for _, idx := range p.re.FindAllIndex(content, -1) {
			start, end := idx[0], idx[1]
			if overlapsClaimed(start, end, claimed) {
				continue
			}
			text := strings.TrimSpace(string(content[start:end]))
			if p.id == "http_remote" && httpHostExempt(text) {
				continue
			}
			// An XML/SGML system identifier or namespace URI is an
			// identifier, not an endpoint: nothing is fetched over it, and
			// the string is fixed by the format's own specification.
			//   <!DOCTYPE plist PUBLIC "…" "http://www.apple.com/DTDs/…">
			//   <svg xmlns="http://www.w3.org/2000/svg">
			if p.id == "http_remote" && isMarkupIdentifierURL(content, start) {
				continue
			}
			// A database URI naming a local or container-internal host with
			// no credentials (`redis://redis:6379/0`,
			// `postgres://localhost:5432/app`) states only that the project
			// uses a database. It carries no secret and no coupling to
			// external infrastructure — the same reasoning that already
			// exempts http://kafka for http_remote.
			if (p.id == "db_uri" || p.id == "jdbc") && dbURIHostExempt(text) {
				continue
			}
			if p.id == "creds_in_url" && credsAreNonSecret(text) {
				// Not a leaked secret: the password is a runtime template
				// placeholder (${PASS}), or BOTH user and password are
				// canonical service defaults (postgres:postgres, guest:guest).
				// These are docker-compose / quickstart URLs. Claim the span
				// so a lower-severity pattern (e.g. db_uri) doesn't re-flag
				// the same range.
				claimed = append(claimed, claimedSpan{start, end})
				continue
			}
			claimed = append(claimed, claimedSpan{start, end})
			out = append(out, Finding{
				Severity: p.severity,
				Title:    p.title,
				Message:  fmt.Sprintf("%s in %s:%d. %s", text, rel, lineNum, p.advice),
				FilePath: fmt.Sprintf("%s:%d:%s", rel, lineNum, p.id),
			})
		}
	}
	return out
}

// placeholderTokenRe matches a single template-placeholder token used in
// install scripts, CI workflows, and docs to stand in for credentials
// supplied at runtime: ${VAR} / $VAR / %s / <name> / {{ var }}.
var placeholderTokenRe = regexp.MustCompile(
	`^(?:` +
		`\$\{[^}]+\}` + // ${VAR}, ${VAR:-default}, ${PG_DB_PASS}
		`|\$[A-Za-z_][A-Za-z0-9_]*` + // $VAR, $GITEA_TOKEN
		`|%[sdvqxX]` + // printf verbs: %s %d %v %q %x %X
		`|<[A-Za-z_][A-Za-z0-9_-]*>` + // <user>, <DB_PASS>
		`|\{\{\s*[A-Za-z_][A-Za-z0-9_.-]*\s*\}\}` + // {{ user }}, {{var}}
		`)$`,
)

// splitUserInfo splits the post-scheme remainder of a URL into its username
// and password. It skips over `${…}` and `{{…}}` template groups when looking
// for the `user:pass` separator, because a compose-style URL puts a colon
// INSIDE the placeholder:
//
//	${POSTGRES_USER:-secdata}:${POSTGRES_PASSWORD:?err}@postgres:5432/db
//
// A naive IndexByte(':') lands inside `${POSTGRES_USER:-…}` and yields a
// nonsense password, which is why every such compose URL used to be reported
// as a committed credential.
func splitUserInfo(rest string) (user, pass string, ok bool) {
	colon := -1
	for i := 0; i < len(rest); i++ {
		if strings.HasPrefix(rest[i:], "${") {
			if j := strings.Index(rest[i:], "}"); j >= 0 {
				i += j
				continue
			}
		}
		if strings.HasPrefix(rest[i:], "{{") {
			if j := strings.Index(rest[i:], "}}"); j >= 0 {
				i += j + 1
				continue
			}
		}
		switch rest[i] {
		case ':':
			if colon < 0 {
				colon = i
			}
		case '@':
			if colon < 0 {
				return "", "", false
			}
			return rest[:colon], rest[colon+1 : i], true
		}
	}
	return "", "", false
}

// credsArePlaceholder returns true when the password segment of a URL
// is entirely a single template placeholder. The username is treated
// as non-sensitive: account names rarely qualify as secrets, and
// patterns like `postgresql://nodeapp:$DBPASS@host` are templates
// where only the password is supplied at runtime. The sensitive value
// is the password — if it's a placeholder, nothing real was committed.
// Required input shape matches creds_in_url's regex:
// scheme://USER:PASS@rest.
func credsArePlaceholder(url string) bool {
	schemeEnd := strings.Index(url, "://")
	if schemeEnd < 0 {
		return false
	}
	_, pass, ok := splitUserInfo(url[schemeEnd+3:])
	if !ok {
		return false
	}
	return placeholderTokenRe.MatchString(pass)
}

// knownDefaultCredentials are username/password values that are canonical
// docker-compose / quickstart defaults. A URL where BOTH the user and the
// password are drawn from this set (postgres:postgres, guest:guest,
// root:example, …) grants access to nothing real — it's an example, not a leak.
var knownDefaultCredentials = map[string]bool{
	"postgres": true, "root": true, "admin": true, "guest": true,
	"example": true, "password": true, "changeme": true, "test": true,
	"user": true, "username": true, "mysql": true, "redis": true,
	"mongo": true, "mongodb": true, "rabbitmq": true, "secret": true,
}

// credsAreNonSecret extends credsArePlaceholder with a both-sides default-
// credential exemption: a URL where BOTH the user and the password are canonical
// service defaults (postgres:postgres, guest:guest, root:example) is a docker-
// compose / quickstart example, not a committed secret. The HOST is deliberately
// NOT considered — per the gate's contract, real-looking credentials must fire
// even on localhost or an RFC1918 address (committing real creds is a leak
// regardless of host reachability); only a default:default pair is exempt.
func credsAreNonSecret(rawURL string) bool {
	if credsArePlaceholder(rawURL) {
		return true
	}
	schemeEnd := strings.Index(rawURL, "://")
	if schemeEnd < 0 {
		return false
	}
	rawUser, rawPass, ok := splitUserInfo(rawURL[schemeEnd+3:])
	if !ok {
		return false
	}
	user := strings.ToLower(rawUser)
	pass := strings.ToLower(rawPass)
	if knownDefaultCredentials[user] && knownDefaultCredentials[pass] {
		return true
	}
	// `portal:portal`, `hattrick:hattrick` — a user repeated as its own
	// password is a scaffold default nobody chose, the same class as
	// postgres:postgres but with a project-specific name.
	if user != "" && user == pass {
		return true
	}
	// Structural markers disqualify the whole URL: a regex metacharacter
	// means this is a detection rule, and a one- or two-character segment is
	// prose shorthand in a doc comment (`scheme://u:p@host`).
	if isStructuralNonCredential(rawUser) || isStructuralNonCredential(rawPass) {
		return true
	}
	// Vocabulary rules apply to the PASSWORD only. The username is not the
	// secret, and a real password may well contain a word from the list —
	// `minerva_ro:readonly_dev_pass@…` is a credential, not a placeholder.
	return isPlaceholderPassword(rawPass)
}

// regexMetaRe matches regex SYNTAX — an escape sequence, a character class,
// a group, or an alternation — rather than individual metacharacters. Their
// presence means the "URL" is a detection rule or a format string:
//
//	(?i)(mongodb(\+srv)?://|postgres(ql)?://)(\S+:)?\S+@
//
// Deliberately structural. Matching bare `$ ^ + * ?` would have been simpler
// and wrong: a strong password contains those constantly (`Xk9$mQ2!vL`), and
// silencing one is a leak this gate exists to catch. A backslash, a bracket
// class, a parenthesised group, or a pipe never appear in a credential a
// server would accept inside a URL.
var regexMetaRe = regexp.MustCompile(`\\[A-Za-z\\.]|\[[^\]]*\]|\([^)]*\)|\|`)

// placeholderWordRe matches a credential segment that is a stand-in word
// rather than a value: `pass`, `password`, `secret`, `token`, `xxx`, `***`,
// `changeme`, `your_password`, `CHANGE_DATABASE_PASSWORD`, `REPLACE_ME`.
var placeholderWordRe = regexp.MustCompile(`^(?:` +
	`pass|passwd|pwd|password|secret|token|apikey|api_key|key|` +
	`changeme|change_me|changethis|dummy|fake|example|placeholder|redacted|` +
	`hidden|value|string|somepassword|supersecret|` +
	`x{2,}|\*{2,}|\.{2,}|_{2,}|-{2,}` +
	`)$`)

// placeholderPrefixes start a credential the author is telling you to
// replace: `your-token`, `my_password`, `change_this`, `replace-me`,
// `insert_key`, `enter-password`, `add_your_...`.
//
// Deliberately prefix-only. A SUFFIX rule (`*_pass`, `*_password`) was tried
// and rejected: it swallowed `readonly_dev_pass`, a real credential. Every
// env-var-shaped stand-in it caught (`CHANGE_DATABASE_PASSWORD`,
// `YOUR_DB_PASSWORD`) is already covered by the prefix and
// SCREAMING_SNAKE_CASE rules below.
var placeholderPrefixes = []string{
	"your", "my", "change", "replace", "insert", "enter", "add", "todo",
}

// placeholderNouns are the words a stand-in prefix is glued to when there is
// no separator: `changeme`, `yourpassword`, `replaceme`, `insertkey`.
var placeholderNouns = map[string]bool{
	"me": true, "this": true, "it": true, "here": true,
	"pass": true, "password": true, "passwd": true, "pwd": true,
	"secret": true, "token": true, "key": true, "apikey": true,
	"value": true, "name": true, "user": true, "username": true,
	"db": true, "dbpass": true, "dbpassword": true, "credentials": true,
}

// hasPlaceholderPrefix reports whether seg is a stand-in the author is telling
// you to replace.
//
// A bare prefix test is not enough: `my` alone would classify
// `svc:mySecretValue@db` as a placeholder and silently drop a real credential.
// The prefix must therefore be followed by a separator (`my_password`,
// `your-token`, `CHANGE_ME`) or by a placeholder noun with nothing after it
// (`changeme`, `yourpassword`) — never by arbitrary text.
func hasPlaceholderPrefix(lower string) bool {
	for _, p := range placeholderPrefixes {
		if !strings.HasPrefix(lower, p) {
			continue
		}
		rest := lower[len(p):]
		if rest == "" {
			return true
		}
		if rest[0] == '_' || rest[0] == '-' || rest[0] == '.' {
			return true
		}
		if placeholderNouns[rest] {
			return true
		}
	}
	return false
}

// isStructuralNonCredential reports whether a userinfo segment cannot be a
// credential at all, judged by shape alone and applied to BOTH the username
// and the password.
func isStructuralNonCredential(seg string) bool {
	if seg == "" {
		return false
	}
	// Regex metacharacters: this is a pattern, not a URL.
	if regexMetaRe.MatchString(seg) {
		return true
	}
	// `u:p`, `x:ghp_abc` — one- and two-character segments are prose
	// shorthand in a doc comment, never an issued credential.
	return len(seg) <= 2
}

// isPlaceholderPassword reports whether the password segment is documentation
// scaffolding. Every rule is about the SHAPE of the value, so a real password
// — high-entropy and carrying none of these markers — is never silenced.
func isPlaceholderPassword(seg string) bool {
	if seg == "" {
		return false
	}
	lower := strings.ToLower(seg)
	if placeholderWordRe.MatchString(lower) {
		return true
	}
	if hasPlaceholderPrefix(lower) {
		return true
	}
	// SCREAMING_SNAKE_CASE is env-var notation, i.e. a slot to fill, not a
	// value: CHANGE_DATABASE_PASSWORD, DB_PASS, SUPABASE_SERVICE_KEY.
	if seg == strings.ToUpper(seg) && strings.ContainsAny(seg, "_-") &&
		!strings.ContainsAny(seg, "abcdefghijklmnopqrstuvwxyz") {
		return true
	}
	return false
}

type claimedSpan struct{ start, end int }

func overlapsClaimed(a, b int, spans []claimedSpan) bool {
	for _, s := range spans {
		if a < s.end && b > s.start {
			return true
		}
	}
	return false
}

// dbURIHostExempt reports whether a database/JDBC URI points at a local or
// container-internal host and carries no inline credentials. Such a URI is
// the docker-compose default every project ships; flagging it is a statement
// that the project uses a database, not a finding.
//
// A URI with inline credentials is never exempt here — creds_in_url claims
// that span first, at error severity, and must keep doing so.
func dbURIHostExempt(rawURL string) bool {
	schemeEnd := strings.Index(rawURL, "://")
	if schemeEnd < 0 {
		// jdbc:postgresql://host/db — strip the outer `jdbc:` and retry.
		if rest := strings.TrimPrefix(rawURL, "jdbc:"); rest != rawURL {
			return dbURIHostExempt(rest)
		}
		return false
	}
	rest := rawURL[schemeEnd+3:]
	// Strip a userinfo section without a password (`user@host`); a
	// `user:pass@host` is handled by creds_in_url and must not land here.
	if at := strings.IndexByte(rest, '@'); at >= 0 {
		if strings.ContainsRune(rest[:at], ':') {
			return false
		}
		rest = rest[at+1:]
	}
	return urlHostExempt(rest)
}

// markupAttrRe matches an XML/SGML attribute whose value IS a name rather than
// an endpoint, anchored so it must sit IMMEDIATELY before the URL:
//
//	<svg xmlns="http://www.w3.org/2000/svg">
//
// The anchor is the point. Matching anywhere in the line prefix meant that one
// `xmlns` earlier on the line suppressed every later URL too, hiding a real
// endpoint in, say, `<image href="http://api.example.com/x"/>` on the same line.
var markupAttrRe = regexp.MustCompile(`(?i)\b(?:xmlns(?::[a-z0-9_.-]+)?|schemaLocation|xsi:schemaLocation|systemId|namespace)\s*=\s*["']$`)

// doctypeOpenRe matches an unclosed DOCTYPE / ENTITY declaration. Inside one,
// the quoted URI is a system identifier naming a DTD — nothing is fetched:
//
//	<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://…/PropertyList-1.0.dtd">
var doctypeOpenRe = regexp.MustCompile(`(?i)<!(?:DOCTYPE|ENTITY)\b[^>]*$`)

// isMarkupIdentifierURL reports whether the URL starting at offset `at` is an
// XML/SGML system identifier or namespace URI — a NAME, not an address.
// Nothing connects over either, so "cleartext MITM" cannot apply, yet every
// .plist, .svg, and XML document in existence carries one.
func isMarkupIdentifierURL(content []byte, at int) bool {
	lineStart := 0
	if i := bytes.LastIndexByte(content[:at], '\n'); i >= 0 {
		lineStart = i + 1
	}
	prefix := content[lineStart:at]
	return markupAttrRe.Match(prefix) || doctypeOpenRe.Match(prefix)
}
