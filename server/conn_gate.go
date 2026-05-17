package main

import (
	"context"
	"encoding/json"
	"fmt"
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
	rest := strings.TrimPrefix(url, "http://")
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
	if strings.HasPrefix(host, "127.") || strings.HasPrefix(host, "10.") ||
		strings.HasPrefix(host, "192.168.") {
		return true
	}
	if strings.HasPrefix(host, "172.") {
		// 172.16.0.0/12 — second octet 16..31.
		parts := strings.Split(host, ".")
		if len(parts) >= 2 {
			if n := atoiSafe(parts[1]); n >= 16 && n <= 31 {
				return true
			}
		}
	}
	if host == "example.com" || strings.HasSuffix(host, ".example.com") ||
		strings.HasSuffix(host, ".test") || strings.HasSuffix(host, ".localhost") ||
		strings.HasSuffix(host, ".invalid") || strings.HasSuffix(host, ".local") {
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
			if p.id == "creds_in_url" && credsArePlaceholder(text) {
				// scheme://${USER}:${PASS}@host — the maintainer has
				// written the URL as a template; user/pass come from
				// the environment at runtime. Not a leaked secret.
				// Claim the span so a lower-severity pattern (e.g.
				// db_uri) doesn't re-flag the same range.
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
	rest := url[schemeEnd+3:]
	// User ends at the first ':'; password ends at the first '@'. The
	// creds_in_url regex guarantees both delimiters exist within the
	// captured span.
	colon := strings.Index(rest, ":")
	if colon < 0 {
		return false
	}
	at := strings.Index(rest[colon+1:], "@")
	if at < 0 {
		return false
	}
	pass := rest[colon+1 : colon+1+at]
	return placeholderTokenRe.MatchString(pass)
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
