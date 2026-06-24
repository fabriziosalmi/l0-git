package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// config_parse_error flags tracked JSON/YAML config files that fail to parse —
// a broken package.json, CI workflow, or k8s manifest is a deterministic
// defect that breaks downstream tooling the moment it lands.
//
// Zero-FP is the bar. The risk with scanning arbitrary repo files is the
// formats that LOOK like JSON/YAML but legally aren't strict JSON/YAML:
//   - JSONC: tsconfig.json, .vscode/*.json, *.jsonc allow comments + trailing
//     commas. Skipped by path; any other .json that uses them is rescued by a
//     tolerant re-parse before we'd ever flag it.
//   - Templates: Helm/Jinja `{{ }}` and ERB `<% %>` files are rendered into
//     YAML elsewhere — not standalone YAML. Skipped on marker detection.
//   - Custom YAML tags (CloudFormation `!Ref`/`!GetAtt`, …): we decode into a
//     yaml.Node, which is tag-agnostic and errors only on real syntax faults.
// TOML and INI are intentionally out of scope: no parser ships in go.mod
// without a new dependency, and INI has no single grammar (zero-FP would be
// impossible across its dialects).

// configParseOptions is the shape of gate_options.config_parse_error. It only
// carries the shared scan skips (fixtures/data/backup/exclude_paths).
type configParseOptions struct {
	scanOptions
}

func parseConfigParseOptions(opts json.RawMessage) configParseOptions {
	return configParseOptions{scanOptions: parseScanOptions(opts)}
}

// maxConfigParseBytes bounds per-file work. A config bigger than this is
// almost certainly generated/data (a giant lockfile or a bundled dataset),
// not a hand-edited config, and parsing it every scan isn't worth it.
const maxConfigParseBytes = 5 << 20 // 5 MiB

func checkConfigParse(ctx context.Context, root string, opts json.RawMessage) ([]Finding, error) {
	options := parseConfigParseOptions(opts)

	if !isGitRepo(root) {
		return []Finding{{
			Severity: SeverityInfo,
			Title:    "config_parse_error skipped (not a git repository)",
			Message:  "Project root has no .git/. config_parse_error scans tracked config files only.",
			FilePath: ".git",
		}}, nil
	}
	files, err := gitLsFiles(ctx, root)
	if err != nil {
		return []Finding{{
			Severity: SeverityWarning,
			Title:    "config_parse_error failed",
			Message:  fmt.Sprintf("Could not enumerate tracked files: %v", err),
			FilePath: ".git",
		}}, nil
	}

	out := []Finding{}
	for _, rel := range files {
		if options.shouldSkipContent(rel) {
			continue
		}
		kind := configKind(rel)
		if kind == cfgNone {
			continue
		}
		full := filepath.Join(root, rel)
		info, statErr := os.Stat(full)
		if statErr != nil || info.IsDir() || info.Size() == 0 || info.Size() > maxConfigParseBytes {
			// Unreadable, a directory, empty (an empty config is an
			// intentional placeholder, not a parse fault), or oversized.
			continue
		}
		data, readErr := os.ReadFile(full)
		if readErr != nil {
			continue
		}
		if msg, bad := configParseError(kind, rel, data); bad {
			out = append(out, Finding{Message: msg, FilePath: rel})
		}
	}
	return out, nil
}

type cfgKind int

const (
	cfgNone cfgKind = iota
	cfgJSON
	cfgYAML
)

// configKind classifies a tracked file by extension, returning cfgNone for
// anything we don't (or deliberately won't) parse.
func configKind(rel string) cfgKind {
	switch strings.ToLower(filepath.Ext(rel)) {
	case ".json":
		if isJSONCPath(rel) {
			return cfgNone // comments + trailing commas are legal here
		}
		return cfgJSON
	case ".yaml", ".yml":
		return cfgYAML
	}
	return cfgNone
}

// isJSONCPath reports paths where // and /* */ comments plus trailing commas
// are legal and idiomatic, so a strict-JSON failure is NOT a defect.
func isJSONCPath(rel string) bool {
	base := strings.ToLower(filepath.Base(rel))
	if strings.HasSuffix(base, ".jsonc") {
		return true
	}
	switch base {
	case "devcontainer.json", ".devcontainer.json":
		return true
	}
	// tsconfig.json / tsconfig.<env>.json / jsconfig.json / jsconfig.<env>.json
	if (strings.HasPrefix(base, "tsconfig.") || strings.HasPrefix(base, "jsconfig.")) &&
		strings.HasSuffix(base, ".json") {
		return true
	}
	// VS Code and devcontainer directories use JSONC for all their .json.
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		switch strings.ToLower(part) {
		case ".vscode", ".devcontainer":
			return true
		}
	}
	return false
}

func configParseError(kind cfgKind, rel string, data []byte) (string, bool) {
	switch kind {
	case cfgJSON:
		return jsonParseError(rel, data)
	case cfgYAML:
		return yamlParseError(rel, data)
	}
	return "", false
}

// jsonParseError flags genuinely-broken JSON. A plain .json that merely uses
// JSONC niceties (comments, trailing commas) is rescued by a tolerant re-parse
// and never flagged — strict-JSON pedantry is not a hygiene defect.
func jsonParseError(rel string, data []byte) (string, bool) {
	b := bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")) // tolerate a UTF-8 BOM
	if json.Valid(b) {
		return "", false
	}
	if json.Valid(stripJSONC(b)) {
		return "", false // it was JSONC, not corruption
	}
	detail := "invalid JSON"
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		detail = err.Error()
	}
	return fmt.Sprintf("%s is not valid JSON: %s. Fix the syntax (e.g. `python -m json.tool %s` shows the exact spot).",
		filepath.Base(rel), detail, filepath.Base(rel)), true
}

// yamlParseError flags genuinely-broken YAML. Decoding into a yaml.Node is
// deliberately permissive: it accepts custom tags (!Ref, !GetAtt, …) and any
// document shape, erroring only on real syntax faults (bad indentation, a tab,
// an unterminated quote). Multi-document files (`---` separated) are each
// validated. Template files are skipped — they aren't standalone YAML.
func yamlParseError(rel string, data []byte) (string, bool) {
	if looksLikeTemplate(data) {
		return "", false
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	for {
		var node yaml.Node
		err := dec.Decode(&node)
		if errors.Is(err, io.EOF) {
			return "", false
		}
		if err != nil {
			return fmt.Sprintf("%s is not valid YAML: %s. Fix the syntax (indentation, quoting, or a stray tab).",
				filepath.Base(rel), oneLine(err.Error())), true
		}
	}
}

// looksLikeTemplate detects Go-template/Helm/Jinja `{{ … }}` expression markers,
// Jinja/Salt/Ansible/GitLab-CI `{% … %}` control blocks, or ERB `<% … %>`.
// Such files are rendered into YAML elsewhere, so they aren't valid standalone
// YAML and must never be flagged. Salt states and Ansible vars routinely use
// ONLY `{% if … %}`/`{% for … %}` blocks with no `{{ }}` expression, so the
// `{%` marker is required, not just `{{`.
func looksLikeTemplate(data []byte) bool {
	return bytes.Contains(data, []byte("{{")) ||
		bytes.Contains(data, []byte("{%")) ||
		bytes.Contains(data, []byte("<%"))
}

func oneLine(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
}

var trailingCommaRe = regexp.MustCompile(`,(\s*[}\]])`)

// stripJSONC removes // line comments, /* */ block comments, and trailing
// commas, so a tolerant json.Valid check can tell JSONC apart from genuine
// corruption. String-literal aware — it never edits inside a double-quoted
// string (so a "//" or "," within a value survives untouched).
func stripJSONC(b []byte) []byte {
	var out bytes.Buffer
	inStr, escaped := false, false
	for i := 0; i < len(b); i++ {
		c := b[i]
		if inStr {
			out.WriteByte(c)
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch {
		case c == '"':
			inStr = true
			out.WriteByte(c)
		case c == '/' && i+1 < len(b) && b[i+1] == '/':
			for i < len(b) && b[i] != '\n' {
				i++
			}
			if i < len(b) {
				out.WriteByte('\n')
			}
		case c == '/' && i+1 < len(b) && b[i+1] == '*':
			i += 2
			for i+1 < len(b) {
				if b[i] == '*' && b[i+1] == '/' {
					i++ // land on '/', the loop's i++ steps past it
					break
				}
				i++
			}
		default:
			out.WriteByte(c)
		}
	}
	return trailingCommaRe.ReplaceAll(out.Bytes(), []byte("$1"))
}
