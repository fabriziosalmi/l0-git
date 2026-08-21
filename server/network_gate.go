package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// network_scan ranks each match by what it actually is, not by raw regex
// hit, so the warning/info split stays meaningful even when the same regex
// matches several flavours of address.

// IPv4 literal: four 1-3-digit octets, dots between. We validate octet
// ranges in classifyIPv4 — the regex itself is intentionally loose so
// "999.0.0.0" becomes a non-match downstream rather than a match nobody
// asked for.
var ipv4Re = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)

// IPv4 CIDR: same regex with /N appended (N up to 32).
var cidrRe = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}/(?:3[0-2]|[12]?\d)\b`)

// ASN reference: "AS" + up to 7 digits. Bare numbers are too noisy.
var asnRe = regexp.MustCompile(`\bAS[0-9]{1,7}\b`)

// Documentation ranges that should not trigger a warning:
//   - RFC 5737 TEST-NET-1/2/3: 192.0.2.0/24, 198.51.100.0/24, 203.0.113.0/24
//   - RFC 2544 benchmarking: 198.18.0.0/15 (TEST-NET-2 per IANA)
//   - IANA MCAST-TEST-NET: 233.252.0.0/24
//   - RFC 6598 shared address space (CGNAT): 100.64.0.0/10
var docNets = mustParseNets(
	"192.0.2.0/24",
	"198.51.100.0/24",
	"203.0.113.0/24",
	"198.18.0.0/15",
	"233.252.0.0/24",
	"100.64.0.0/10",
)

func mustParseNets(cidrs ...string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic(err)
		}
		out = append(out, n)
	}
	return out
}

// networkScanOptions extends the shared scan options with network_scan knobs.
type networkScanOptions struct {
	scanOptions
	// ReportLoopback opts IN to loopback findings (127.0.0.0/8, ::1). Off by
	// default: a loopback literal is a local-dev default, never a security
	// concern, and was the single largest info-noise category. The
	// unspecified address (0.0.0.0) is reported only under ReportUnspecified.
	ReportLoopback *bool `json:"report_loopback,omitempty"`

	// ReportUnspecified opts IN to findings for the unspecified address
	// (0.0.0.0, ::). Off by default for the same reason loopback is: inside
	// a container, `--host 0.0.0.0` is the ONLY correct bind, so the literal
	// is a deployment fact rather than a violation, and the gate cannot see
	// enough to tell the two apart. It was the second-largest info-noise
	// category on a 220-repository sweep (1,146 findings, none actionable).
	// Turn it on for projects that run services directly on a public host.
	ReportUnspecified *bool `json:"report_unspecified,omitempty"`
}

func parseNetworkOptions(opts json.RawMessage) networkScanOptions {
	var o networkScanOptions
	if len(opts) > 0 {
		_ = json.Unmarshal(opts, &o)
	}
	o.scanOptions = parseScanOptions(opts)
	return o
}

func checkNetworkScan(ctx context.Context, root string, opts json.RawMessage) ([]Finding, error) {
	if skip, stop := requireGitRepo(root, "network_scan",
		"Initialize git or run gates from inside a clone — this gate scans tracked files only."); stop {
		return skip, nil
	}
	files, err := gitLsFiles(ctx, root)
	if err != nil {
		return []Finding{{
			Severity: SeverityWarning,
			Title:    "network_scan failed",
			Message:  fmt.Sprintf("Could not enumerate tracked files: %v", err),
			FilePath: ".git",
		}}, nil
	}

	scan := parseNetworkOptions(opts)
	reportLoopback := scan.ReportLoopback != nil && *scan.ReportLoopback
	reportUnspecified := scan.ReportUnspecified != nil && *scan.ReportUnspecified
	out := []Finding{}
	for _, rel := range files {
		if scan.shouldSkipContent(rel) {
			continue
		}
		// Changelog / release-note files routinely describe IP-related behaviour
		// of the project itself ("added RFC 6598 100.64.0.0/10 to docNets") so
		// every entry would be a self-referential FP. Skip the canonical names.
		if isChangelogBasename(filepath.Base(rel)) {
			continue
		}
		// Licence texts are verbatim boilerplate nobody edits.
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
		// A file whose lines are overwhelmingly bare literals is a list whose
		// payload IS those literals — a blocklist, a Tor exit dump, a cache of
		// resolved hosts, a scan-target dump — so every line is a self-evident
		// FP. This catches the .txt/line-oriented lists that the
		// extension-based isDefaultDataFile (.csv/.jsonl/…) does not.
		//
		// Both item shapes count: a bare address per line, and a bare
		// scheme-and-host token per line. connection_strings already
		// recognised the second form via the same exact per-line test;
		// network_scan saw only the first and reported one finding per line of
		// the same file. Honours the same knob.
		if skipEnabled(scan.SkipDefaultDataFiles) &&
			(looksLikeAddressList(data) || looksLikeURLList(data)) {
			continue
		}

		prose := isProseFile(rel)
		line := 1
		start := 0
		emit := func(content []byte, lineNum int) {
			out = append(out, scanNetworkLine(rel, lineNum, content, reportLoopback, reportUnspecified, prose)...)
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

// scanNetworkLine runs every regex against one line and turns matches into
// findings. CIDR is checked first so "10.0.0.0/8" doesn't double-fire as a
// CIDR + bare IPv4.
//
// Doc-range hits (RFC 5737/2544/6598, MCAST-TEST-NET) are suppressed: the
// category itself means "intended for documentation/testing", so emitting a
// finding only generates noise — the maintainer already declared this is
// not a real address by picking that range.
func scanNetworkLine(rel string, lineNum int, content []byte, reportLoopback, reportUnspecified, prose bool) []Finding {
	out := []Finding{}
	// Inline SVG geometry attributes carry packed decimal coordinates
	// ("d=\"M8 0C3.58 0 0 3.58 0 8c0 3.54 …\"") in which `1.23.82.72` is
	// three numbers, not an address. Blank the attribute values before any
	// regex runs — this was, by a wide margin, the single largest source of
	// false "public IPv4" warnings across real repositories.
	content = stripSvgGeometry(content)
	cidrSpans := map[string]bool{}
	// suppressed reports whether a classified category should be dropped:
	// doc-ranges are always noise, and loopback is opt-in.
	suppressed := func(cat string) bool {
		switch cat {
		case "doc-range":
			return true
		case "loopback":
			return !reportLoopback
		case "unspecified":
			return !reportUnspecified
		}
		return false
	}

	for _, m := range cidrRe.FindAll(content, -1) {
		text := string(m)
		cidrSpans[text] = true
		ipPart := text[:strings.Index(text, "/")]
		ip := net.ParseIP(ipPart)
		if ip == nil || ip.To4() == nil {
			continue
		}
		sev, cat := classifyIPv4(ip)
		if suppressed(cat) {
			continue
		}
		out = append(out, networkFinding(rel, lineNum, "cidr", text, sev, cat))
	}

	for _, idx := range ipv4Re.FindAllIndex(content, -1) {
		text := string(content[idx[0]:idx[1]])
		// Skip if this match is the IP-portion of a CIDR we already
		// emitted (e.g. "10.0.0.0/8" → don't also flag "10.0.0.0").
		if cidrAlreadyCoveredAtSameLine(text, cidrSpans) {
			continue
		}
		// A four-component version is byte-identical to a dotted quad, so
		// only the surrounding text can tell them apart: `Chrome/120.0.0.0`,
		// `brotlicffi==1.2.0.1`, `version="0.0.1.0"`.
		if looksLikeVersionLiteral(content, idx[0], idx[1]) {
			continue
		}
		ip := net.ParseIP(text)
		if ip == nil || ip.To4() == nil {
			continue
		}
		sev, cat := classifyIPv4(ip)
		if suppressed(cat) {
			continue
		}
		out = append(out, networkFinding(rel, lineNum, "ipv4", text, sev, cat))
	}

	// An ASN written in prose ("| Cloudflare | ~19% | 310+ | AS13335 |",
	// "traffic flows from Verizon (AS701) to China Telecom (AS4134)") is
	// describing routing, not wiring it. Only a config or source file can
	// actually pin a project to an ASN.
	if prose {
		return out
	}
	for _, m := range asnRe.FindAll(content, -1) {
		text := string(m)
		// Trim "AS" prefix and validate.
		num, err := strconv.Atoi(text[2:])
		if err != nil || num <= 0 {
			continue
		}
		out = append(out, Finding{
			Severity: SeverityInfo,
			Title:    "Hardcoded ASN reference",
			Message:  fmt.Sprintf("ASN %s referenced in %s:%d. Routing-policy literals are usually fine but worth knowing.", text, rel, lineNum),
			FilePath: fmt.Sprintf("%s:%d:asn", rel, lineNum),
		})
	}
	return out
}

// looksLikeAddressList reports whether data is a line-oriented list of bare
// IP / CIDR literals (a blocklist, allowlist, resolver cache, …) rather than
// source that happens to mention an address. Detection is exact: a line
// qualifies only when, after stripping an inline comment and surrounding
// whitespace, it parses as a single IP or CIDR — so "server 1.2.3.4:80;" or
// "1.2.3.4 hostname" (multi-token) and "999.0.0.0" (invalid) never count.
func looksLikeAddressList(data []byte) bool {
	return looksLikeListFile(data, isBareAddress)
}

// stripInlineComment removes a trailing comment introduced by '#' or ';'
// (the comment markers blocklist formats use). IP/CIDR literals contain
// neither character, so this never truncates a real address.
func stripInlineComment(line string) string {
	if i := strings.IndexAny(line, "#;"); i >= 0 {
		return line[:i]
	}
	return line
}

// isBareAddress reports whether s (after stripping an inline comment) is
// exactly one IP (v4 or v6) or CIDR.
func isBareAddress(s string) bool {
	s = strings.TrimSpace(stripInlineComment(s))
	if net.ParseIP(s) != nil {
		return true
	}
	if _, _, err := net.ParseCIDR(s); err == nil {
		return true
	}
	return false
}

func cidrAlreadyCoveredAtSameLine(ipText string, cidrs map[string]bool) bool {
	for c := range cidrs {
		if strings.HasPrefix(c, ipText+"/") {
			return true
		}
	}
	return false
}

// classifyIPv4 returns (severity, category) for a parsed IPv4 address.
func classifyIPv4(ip net.IP) (string, string) {
	if ip.IsLoopback() {
		return SeverityInfo, "loopback"
	}
	if ip.IsUnspecified() {
		return SeverityInfo, "unspecified"
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return SeverityInfo, "link-local"
	}
	for _, n := range docNets {
		if n.Contains(ip) {
			return SeverityInfo, "doc-range"
		}
	}
	if ip.IsPrivate() {
		return SeverityInfo, "private"
	}
	// Octets that count 1,2,3,4 or 4,3,2,1 are the universal stand-in
	// address of technical writing. No allocation is ever laid out that way.
	//
	// Its own category, NOT "doc-range": doc-range is dropped outright by
	// scanNetworkLine, and unlike 192.0.2.0/24 these addresses are really
	// allocated and routable — the author only conventionally treats them as
	// fictional. Reporting at info says exactly that, and matches what the
	// changelog promises.
	if isSequentialOctets(ip) {
		return SeverityInfo, "doc-placeholder"
	}
	// A public resolver constant (8.8.8.8, 1.1.1.1, 9.9.9.9, …) is a
	// deliberate, globally-anycast choice — not "infrastructure this project
	// accidentally pinned itself to". Worth seeing, never worth a warning.
	if publicResolvers[ip.String()] {
		return SeverityInfo, "public-resolver"
	}
	if ip.Equal(net.IPv4bcast) {
		return SeverityInfo, "broadcast"
	}
	if ip.IsMulticast() {
		return SeverityInfo, "multicast"
	}
	if v4 := ip.To4(); v4 != nil && v4[0] >= 240 {
		// 240.0.0.0/4 — reserved for future use, never routed.
		return SeverityInfo, "reserved"
	}
	return SeverityWarning, "public"
}

func networkFinding(rel string, lineNum int, kind, text, severity, category string) Finding {
	return Finding{
		Severity: severity,
		Title:    fmt.Sprintf("%s address (%s)", kindLabel(kind), category),
		Message:  fmt.Sprintf("%s %s found in %s:%d (%s). %s", kindLabel(kind), text, rel, lineNum, category, networkAdvice(category)),
		FilePath: fmt.Sprintf("%s:%d:%s_%s", rel, lineNum, kind, category),
	}
}

func kindLabel(kind string) string {
	switch kind {
	case "cidr":
		return "CIDR"
	default:
		return "IPv4"
	}
}

// isChangelogBasename returns true for the canonical release-notes filenames
// (any case). These files describe what the project does, including network
// behaviour, so addresses listed inside are descriptive rather than wired.
func isChangelogBasename(name string) bool {
	switch strings.ToLower(name) {
	case "changelog.md", "changelog", "changelog.txt", "changelog.rst",
		"history.md", "history", "history.txt", "history.rst",
		"releases.md", "releases", "releases.txt",
		"release-notes.md", "release_notes.md", "releasenotes.md",
		"changes.md", "changes", "changes.txt", "news.md", "news":
		return true
	}
	return false
}

func networkAdvice(category string) string {
	switch category {
	case "public":
		return "Hardcoding a public address into source ties the project to fixed infrastructure — consider config/env."
	case "private":
		return "Private RFC1918 ranges in source are usually intentional but easy to leak into production by accident."
	case "loopback":
		return "Loopback literals are typical of local-dev defaults; flag is informational."
	case "doc-range":
		return "RFC 5737 documentation range — fine if used in examples."
	case "link-local":
		return "Link-local address — usually a transient identifier; review the context."
	case "unspecified":
		return "0.0.0.0 / similar — review the surrounding bind/listen logic."
	case "public-resolver":
		return "Well-known public DNS resolver — an intentional constant, not accidental infrastructure coupling."
	case "doc-placeholder":
		return "Consecutive octets (1.2.3.4 / 4.3.2.1) are the conventional stand-in address — but the range is really allocated, so double-check it is an example."
	case "broadcast":
		return "Limited-broadcast address — a protocol constant, not a host."
	case "multicast":
		return "Multicast group address — a protocol constant, not a host."
	case "reserved":
		return "240.0.0.0/4 is reserved and never routed — almost certainly a sentinel value."
	}
	return ""
}

// =============================================================================
// false-positive suppression helpers
// =============================================================================

// svgGeometryRe matches an SVG geometry attribute together with its
// double-quoted value. These attributes hold packed coordinate data — SVG
// lets `1.5.5` mean "1.5 then 0.5", so a path routinely contains runs that
// are byte-for-byte valid dotted quads (`2.2.82.64`, `1.23.82.72`).
//
// Deliberately requires the HTML/JSX attribute form with no spaces around
// `=`, so a Python assignment `d = "10.0.0.1"` is untouched.
var svgGeometryRe = regexp.MustCompile(`(?i)\b(?:d|viewBox|points|transform|patternTransform|gradientTransform|preserveAspectRatio)="[^"]*"`)

// stripSvgGeometry blanks the value of every SVG geometry attribute on the
// line, preserving length so byte offsets stay meaningful for callers that
// use them. Returns the input untouched when there is nothing to strip.
func stripSvgGeometry(content []byte) []byte {
	locs := svgGeometryRe.FindAllIndex(content, -1)
	if len(locs) == 0 {
		return content
	}
	out := make([]byte, len(content))
	copy(out, content)
	for _, loc := range locs {
		for i := loc[0]; i < loc[1]; i++ {
			out[i] = ' '
		}
	}
	return out
}

// versionContextRe matches the text that can immediately precede a version
// number. Checked against the (lower-cased) tail of the bytes before a match:
//
//	Chrome/120.0.0.0        product/version in a User-Agent
//	brotlicffi==1.2.0.1     dependency pin
//	version="0.0.1.0"       manifest attribute
//	v1.2.3.4                tag / release name
//
// Every alternative here was narrowed against real false NEGATIVES found on
// a 220-repository sweep:
//
//   - a bare `@` was dropped: `ssh root@192.168.0.136` is far more common
//     than an npm `pkg@1.2.3.4`, and it silenced real hardcoded hosts.
//   - the comparison form forbids a following quote and requires an
//     identifier character before the operator, so JavaScript's
//     `h === '169.254.169.254'` stays a comparison against an address.
//   - the product/version form requires the product token to START at a
//     non-path character, so a URL path segment (`https://api.acme.com/1.2.3.4`)
//     never qualifies.
var versionContextRe = regexp.MustCompile(`(?:version|revision|build|release)\s*[:=]\s*["']?$` +
	`|[a-z0-9_)\]]\s*[=~^<>!]=\s*$` +
	`|[^./a-z0-9][a-z][a-z0-9_.-]*/$` +
	`|[\s"'(\[,=:]v$` +
	`|^v$`)

// versionLookback is how far behind a match we inspect for version context.
// Long enough for `"software_version": "` and short enough that unrelated
// text on the line cannot reach the match.
const versionLookback = 24

// looksLikeVersionLiteral reports whether the dotted quad at [start,end) is a
// four-component version number rather than an address. A version and an
// IPv4 literal are syntactically identical, so context is the only signal
// available — and without it every `Chrome/120.0.0.0` User-Agent string in a
// codebase reports as a hardcoded public address.
func looksLikeVersionLiteral(content []byte, start, end int) bool {
	from := start - versionLookback
	if from < 0 {
		from = 0
	}
	before := strings.ToLower(string(content[from:start]))
	if versionContextRe.MatchString(before) {
		return true
	}
	// A fifth NUMERIC component means the regex clipped a longer dotted run:
	// `1.3.6.1.4.1.311` is an OID, `537.36.1.2.3` a build string. The digit
	// requirement matters — `169.254.169.254.nip.io` is a hostname wrapping
	// a real address, and must stay reported.
	if end+1 < len(content) && content[end] == '.' && content[end+1] >= '0' && content[end+1] <= '9' {
		return true
	}
	return false
}

// publicResolvers is the closed set of well-known public DNS resolver
// addresses. Hardcoding one is a deliberate, documented choice (they are
// globally anycast and outlive every project that references them), so it is
// reported as information rather than as "infrastructure coupling".
var publicResolvers = map[string]bool{
	"8.8.8.8": true, "8.8.4.4": true, // Google
	"1.1.1.1": true, "1.0.0.1": true, "1.1.1.2": true, "1.0.0.2": true, // Cloudflare
	"9.9.9.9": true, "9.9.9.10": true, "9.9.9.11": true, // Quad9
	"149.112.112.112": true, "149.112.112.10": true, "149.112.112.11": true,
	"208.67.222.222": true, "208.67.220.220": true, // OpenDNS
	"208.67.222.220": true, "208.67.220.222": true,
	"94.140.14.14": true, "94.140.15.15": true, // AdGuard
	"76.76.2.0": true, "76.76.10.0": true, // ControlD
	"185.228.168.9": true, "185.228.169.9": true, // CleanBrowsing
	"64.6.64.6": true, "64.6.65.6": true, // Verisign
	"4.2.2.1": true, "4.2.2.2": true, // Level3
	"77.88.8.8": true, "77.88.8.1": true, // Yandex
}

// isSequentialOctets reports whether the four octets form a strictly
// consecutive run, ascending or descending: 1.2.3.4, 5.6.7.8, 4.3.2.1.
// Real allocations are never laid out that way, so such a literal is a
// documentation stand-in by construction — the same reasoning RFC 5737
// applies to 192.0.2.0/24, just without the RFC.
func isSequentialOctets(ip net.IP) bool {
	v4 := ip.To4()
	if v4 == nil {
		return false
	}
	asc, desc := true, true
	for i := 1; i < 4; i++ {
		if int(v4[i]) != int(v4[i-1])+1 {
			asc = false
		}
		if int(v4[i]) != int(v4[i-1])-1 {
			desc = false
		}
	}
	return asc || desc
}

// proseExtensions are the documentation formats where an address or an ASN is
// being described to a reader rather than configured for a machine.
var proseExtensions = map[string]bool{
	".md": true, ".markdown": true, ".mdx": true,
	".rst": true, ".adoc": true, ".asciidoc": true,
	".txt": true, ".tex": true, ".org": true,
}

// isProseFile reports whether rel is a documentation file.
func isProseFile(rel string) bool {
	return proseExtensions[strings.ToLower(filepath.Ext(rel))]
}

// licenseBasenames are the verbatim-boilerplate files every project carries.
// Their http:// URLs and addresses are the licence text itself.
func isLicenseBasename(name string) bool {
	lower := strings.ToLower(name)
	lower = strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(lower, ".md"), ".txt"), ".rst")
	switch lower {
	case "license", "licence", "licenses", "licences", "copying", "copyright",
		"notice", "unlicense", "license-mit", "license-apache":
		return true
	}
	return strings.HasPrefix(lower, "license-") || strings.HasPrefix(lower, "licence-")
}
