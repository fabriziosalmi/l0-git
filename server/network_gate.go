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

	scan := parseScanOptions(opts)
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
		// A file whose lines are overwhelmingly bare IP/CIDR literals is an
		// address list (blocklist, Tor exit dump, cache of resolved hosts) —
		// the addresses ARE the payload, so every line is a self-evident FP.
		// This catches the .txt/line-oriented lists that the extension-based
		// isDefaultDataFile (.csv/.jsonl/…) does not. Honours the same knob.
		if skipEnabled(scan.SkipDefaultDataFiles) && looksLikeAddressList(data) {
			continue
		}

		line := 1
		start := 0
		emit := func(content []byte, lineNum int) {
			out = append(out, scanNetworkLine(rel, lineNum, content)...)
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
func scanNetworkLine(rel string, lineNum int, content []byte) []Finding {
	out := []Finding{}
	cidrSpans := map[string]bool{}

	for _, m := range cidrRe.FindAll(content, -1) {
		text := string(m)
		cidrSpans[text] = true
		ipPart := text[:strings.Index(text, "/")]
		ip := net.ParseIP(ipPart)
		if ip == nil || ip.To4() == nil {
			continue
		}
		sev, cat := classifyIPv4(ip)
		if cat == "doc-range" {
			continue
		}
		out = append(out, networkFinding(rel, lineNum, "cidr", text, sev, cat))
	}

	for _, m := range ipv4Re.FindAll(content, -1) {
		text := string(m)
		// Skip if this match is the IP-portion of a CIDR we already
		// emitted (e.g. "10.0.0.0/8" → don't also flag "10.0.0.0").
		if cidrAlreadyCoveredAtSameLine(text, cidrSpans) {
			continue
		}
		ip := net.ParseIP(text)
		if ip == nil || ip.To4() == nil {
			continue
		}
		sev, cat := classifyIPv4(ip)
		if cat == "doc-range" {
			continue
		}
		out = append(out, networkFinding(rel, lineNum, "ipv4", text, sev, cat))
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
	}
	return ""
}

