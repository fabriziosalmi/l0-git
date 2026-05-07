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

// Documentation ranges per RFC 5737 / 3849 + the well-known TEST-NET CIDRs.
var docNets = mustParseNets(
	"192.0.2.0/24",
	"198.51.100.0/24",
	"203.0.113.0/24",
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
		if scan.shouldSkip(rel) {
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

