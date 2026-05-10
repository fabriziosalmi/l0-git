package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// manifestVersion records what one manifest declared its version as. The
// path is relative to the project root for use in finding messages.
type manifestVersion struct {
	path    string
	version string
	source  string // human-readable origin ("package.json", "Cargo.toml [package].version", …)
}

// versionExtractor pulls a version from one specific manifest format.
// Each returns ("", false) if it can't find a usable version.
type versionExtractor struct {
	relPath string
	parse   func(content string) (string, bool)
	source  string
}

var versionExtractors = []versionExtractor{
	{relPath: "package.json", source: "package.json:version", parse: extractJSONVersion},
	{relPath: "Cargo.toml", source: "Cargo.toml [package].version", parse: extractCargoVersion},
	{relPath: "pyproject.toml", source: "pyproject.toml version", parse: extractPyprojectVersion},
	{relPath: "mix.exs", source: "mix.exs version", parse: extractMixVersion},
	{relPath: "pom.xml", source: "pom.xml first <version>", parse: extractPomVersion},
	{relPath: "VERSION", source: "VERSION file", parse: extractPlainVersion},
	{relPath: "version.txt", source: "version.txt", parse: extractPlainVersion},
}

// versionShape filters out manifest values like git SHAs or computed
// strings ("0.0.0-dev", "{{VERSION}}"). We accept anything that vaguely
// looks semver-ish; bumping later is cheap.
var versionShape = regexp.MustCompile(`^v?\d+(?:\.\d+){1,3}(?:[-+][\w.\-]+)?$`)

func checkVersionDrift(_ context.Context, root string, _ json.RawMessage) ([]Finding, error) {
	// In monorepo layouts the root package.json is a workspace container —
	// its version field is often a placeholder (0.0.0) and should not be
	// compared against the other manifests' real versions.
	skipPackageJSON := isMonorepoRoot(root)

	declared := []manifestVersion{}
	for _, ex := range versionExtractors {
		if skipPackageJSON && ex.relPath == "package.json" {
			continue
		}
		full := filepath.Join(root, ex.relPath)
		data, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		v, ok := ex.parse(string(data))
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		v = strings.TrimPrefix(v, "v")
		if !versionShape.MatchString("v" + v) { // re-add v for the regex anchor
			continue
		}
		declared = append(declared, manifestVersion{
			path:    ex.relPath,
			version: v,
			source:  ex.source,
		})
	}

	if len(declared) < 2 {
		return nil, nil
	}

	// All-equal? No drift.
	first := declared[0].version
	mismatch := false
	for _, m := range declared[1:] {
		if m.version != first {
			mismatch = true
			break
		}
	}
	if !mismatch {
		return nil, nil
	}

	// Pick the "leader" deterministically — alphabetical by path — and
	// emit one finding per non-matching manifest pinned at its own file
	// path. That way the Problems pane shows one mark per file.
	sort.SliceStable(declared, func(i, j int) bool { return declared[i].path < declared[j].path })
	leader := declared[0]
	out := []Finding{}
	for _, m := range declared[1:] {
		if m.version == leader.version {
			continue
		}
		out = append(out, Finding{
			Severity: SeverityWarning,
			Title:    "Version mismatch across manifests",
			Message: fmt.Sprintf(
				"%s declares version %s but %s declares %s. Pick a single source of truth or wire a build-time sync.",
				m.source, m.version, leader.source, leader.version,
			),
			FilePath: m.path,
		})
	}
	return out, nil
}

// isMonorepoRoot returns true when the project root contains well-known
// monorepo tooling markers: pnpm-workspace.yaml, lerna.json, nx.json,
// turbo.json, or a package.json that declares a "workspaces" field.
// In these setups the root package.json version is a placeholder and must
// not be cross-checked against other manifests.
func isMonorepoRoot(root string) bool {
	markers := []string{
		"pnpm-workspace.yaml",
		"pnpm-workspace.yml",
		"lerna.json",
		"nx.json",
		"turbo.json",
	}
	for _, m := range markers {
		if _, err := os.Stat(filepath.Join(root, m)); err == nil {
			return true
		}
	}
	// package.json#workspaces field is the npm/Yarn monorepo convention.
	pkgData, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return false
	}
	return strings.Contains(string(pkgData), `"workspaces"`)
}

// extractJSONVersion finds the top-level "version": "..." key in a JSON
// document. Avoids a full json.Unmarshal so weird package.json files
// (comments, trailing commas in tooling files) don't break the gate.
var jsonVersionRe = regexp.MustCompile(`"version"\s*:\s*"([^"]+)"`)

func extractJSONVersion(content string) (string, bool) {
	m := jsonVersionRe.FindStringSubmatch(content)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// extractCargoVersion finds version inside [package] (or [workspace.package]).
// Cargo allows the key elsewhere; we only care about the package one.
func extractCargoVersion(content string) (string, bool) {
	return tomlSectionVersion(content, []string{"[package]", "[workspace.package]"})
}

// extractPyprojectVersion finds version under [project] or [tool.poetry].
func extractPyprojectVersion(content string) (string, bool) {
	return tomlSectionVersion(content, []string{"[project]", "[tool.poetry]"})
}

// tomlSectionVersion walks a TOML document line-by-line, tracks which
// section we're in, and returns the first `version = "..."` we hit inside
// any of the requested section headers.
func tomlSectionVersion(content string, sections []string) (string, bool) {
	versionLine := regexp.MustCompile(`^\s*version\s*=\s*"([^"]+)"`)
	currentSection := ""
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			currentSection = trimmed
			continue
		}
		if !sectionMatches(currentSection, sections) {
			continue
		}
		if m := versionLine.FindStringSubmatch(line); m != nil {
			return m[1], true
		}
	}
	return "", false
}

func sectionMatches(current string, wanted []string) bool {
	for _, w := range wanted {
		if current == w {
			return true
		}
	}
	return false
}

// extractMixVersion finds @version in an Elixir mix.exs (the canonical
// idiom is `@version "1.2.3"` referenced from `def project`).
var mixVersionRe = regexp.MustCompile(`@version\s+"([^"]+)"`)

func extractMixVersion(content string) (string, bool) {
	m := mixVersionRe.FindStringSubmatch(content)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// extractPomVersion grabs the first <version> tag — for most pom.xml files
// that's the artifact's own version (parent versions tend to come later).
var pomVersionRe = regexp.MustCompile(`<version>\s*([^<\s]+)\s*</version>`)

func extractPomVersion(content string) (string, bool) {
	m := pomVersionRe.FindStringSubmatch(content)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// extractPlainVersion uses the entire trimmed content. A VERSION file with
// extra noise (multiple lines, blank lines, comments) is rejected; we
// expect "1.2.3" or "v1.2.3" on its own.
func extractPlainVersion(content string) (string, bool) {
	v := strings.TrimSpace(content)
	if strings.ContainsAny(v, "\n#") {
		return "", false
	}
	if v == "" {
		return "", false
	}
	return v, true
}
