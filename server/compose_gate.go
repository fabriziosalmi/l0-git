package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// composeLintOptions is the shape of gate_options.compose_lint.
type composeLintOptions struct {
	scanOptions
	DisabledRules      []string `json:"disabled_rules,omitempty"`
	SuggestWhenMissing bool     `json:"suggest_when_missing,omitempty"`
}

// composeFinding is the gate's intermediate structure: a (line, ruleID,
// message) triple resolved into a Finding with severity from the rule
// definition. Line is 1-based, taken from the YAML node where the
// violation lives — that's what makes "deterministic + auditable" stick.
type composeFinding struct {
	ruleID  string
	line    int
	message string
}

type composeRule struct {
	id       string
	severity string
	title    string
	advice   string
}

var composeRules = map[string]composeRule{
	"yaml_invalid": {
		id:       "yaml_invalid",
		severity: SeverityWarning,
		title:    "Compose YAML failed to parse",
		advice:   "The file isn't valid YAML or doesn't decode into the expected service map. Run `docker compose config` (or your editor's YAML linter) for the full diagnostic.",
	},
	"privileged_true": {
		id:       "privileged_true",
		severity: SeverityWarning,
		title:    "Compose service is privileged",
		advice:   "`privileged: true` gives the container near-root access on the host. Drop it unless the workload genuinely needs it (and document why).",
	},
	"network_mode_host": {
		id:       "network_mode_host",
		severity: SeverityWarning,
		title:    "Compose service uses host networking",
		advice:   "`network_mode: host` removes the container's network namespace and exposes host ports directly. Reach for it only when you actually need it.",
	},
	"docker_socket_mount": {
		id:       "docker_socket_mount",
		severity: SeverityWarning,
		title:    "Compose service mounts the Docker socket",
		advice:   "Mounting /var/run/docker.sock into a container effectively grants it root on the host. Tools like Traefik/Portainer/Watchtower do this by design — if so, override with `# l0git: ignore docker_socket_mount reason: …`.",
	},
	"missing_memory_limit": {
		id:       "missing_memory_limit",
		severity: SeverityInfo,
		title:    "Compose service has no memory limit",
		advice:   "Without a memory limit a runaway container can OOM the whole host. Set deploy.resources.limits.memory (or mem_limit on legacy v2 syntax).",
	},
}

func checkComposeLint(ctx context.Context, root string, opts json.RawMessage) ([]Finding, error) {
	options := parseComposeOptions(opts)

	if !isGitRepo(root) {
		return []Finding{{
			Severity: SeverityInfo,
			Title:    "compose_lint skipped (not a git repository)",
			Message:  "Project root has no .git/. compose_lint scans tracked compose files only.",
			FilePath: ".git",
		}}, nil
	}
	files, err := gitLsFiles(ctx, root)
	if err != nil {
		return []Finding{{
			Severity: SeverityWarning,
			Title:    "compose_lint failed",
			Message:  fmt.Sprintf("Could not enumerate tracked files: %v", err),
			FilePath: ".git",
		}}, nil
	}

	composes := []string{}
	for _, rel := range files {
		if options.shouldSkip(rel) {
			continue
		}
		if isComposeBasename(filepath.Base(rel)) {
			composes = append(composes, rel)
		}
	}

	if len(composes) == 0 {
		if options.SuggestWhenMissing {
			return []Finding{{
				Severity: SeverityInfo,
				Title:    "No Docker Compose file detected",
				Message:  "No docker-compose.yml / compose.yml is tracked. If you ship a multi-service stack, adding one helps shipping/adoption parity.",
				FilePath: "compose.yml",
			}}, nil
		}
		return nil, nil
	}

	disabled := map[string]bool{}
	for _, id := range options.DisabledRules {
		disabled[id] = true
	}

	out := []Finding{}
	for _, rel := range composes {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		out = append(out, evaluateComposeFile(rel, data, disabled)...)
	}
	return out, nil
}

// isComposeBasename: docker-compose.yml / compose.yml in their canonical
// spellings (yaml/yml + override variants).
func isComposeBasename(name string) bool {
	switch name {
	case "docker-compose.yml", "docker-compose.yaml",
		"docker-compose.override.yml", "docker-compose.override.yaml",
		"compose.yml", "compose.yaml",
		"compose.override.yml", "compose.override.yaml":
		return true
	}
	return false
}

// evaluateComposeFile parses one compose file and applies every enabled
// rule. Pulled out so dockerfile_lint-style direct unit tests can target
// the same code path the runner uses.
func evaluateComposeFile(rel string, data []byte, disabled map[string]bool) []Finding {
	overrides := collectComposeOverrides(string(data))

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		if disabled["yaml_invalid"] {
			return nil
		}
		return []Finding{ruleFinding(rel, 1, composeRules["yaml_invalid"], err.Error(), overrides)}
	}
	doc := mappingNode(&root)
	if doc == nil {
		// File is YAML but not a mapping (e.g. just `null`). Same posture
		// as parse failure for our purposes.
		if disabled["yaml_invalid"] {
			return nil
		}
		return []Finding{ruleFinding(rel, 1, composeRules["yaml_invalid"], "top level is not a mapping", overrides)}
	}

	servicesNode := mappingValue(doc, "services")
	if servicesNode == nil {
		// No services key → nothing to lint per the rules below; return
		// quietly (this is a stack-less compose, e.g. only `name:` or
		// only `volumes:`).
		return nil
	}

	composeViolations := []composeFinding{}
	for i := 0; i < len(servicesNode.Content); i += 2 {
		nameNode := servicesNode.Content[i]
		serviceNode := servicesNode.Content[i+1]
		if serviceNode.Kind != yaml.MappingNode {
			continue
		}
		_ = nameNode // currently kept for future "service: foo" prefixing
		composeViolations = append(composeViolations, scanComposeService(serviceNode)...)
	}

	out := []Finding{}
	for _, v := range composeViolations {
		rule, ok := composeRules[v.ruleID]
		if !ok {
			continue
		}
		if disabled[rule.id] {
			continue
		}
		out = append(out, ruleFinding(rel, v.line, rule, v.message, overrides))
	}
	return out
}

// scanComposeService walks one service mapping and emits violations for
// every rule that applies. Each violation carries the line of the
// offending YAML node so findings can pin to the right place.
func scanComposeService(svc *yaml.Node) []composeFinding {
	out := []composeFinding{}

	// privileged: true
	if v := mappingValue(svc, "privileged"); v != nil && isTrueScalar(v) {
		out = append(out, composeFinding{
			ruleID:  "privileged_true",
			line:    v.Line,
			message: "privileged: true",
		})
	}

	// network_mode: host
	if v := mappingValue(svc, "network_mode"); v != nil && v.Kind == yaml.ScalarNode && v.Value == "host" {
		out = append(out, composeFinding{
			ruleID:  "network_mode_host",
			line:    v.Line,
			message: "network_mode: host",
		})
	}

	// volumes contains /var/run/docker.sock as source
	if v := mappingValue(svc, "volumes"); v != nil && v.Kind == yaml.SequenceNode {
		for _, item := range v.Content {
			if isDockerSocketMount(item) {
				out = append(out, composeFinding{
					ruleID:  "docker_socket_mount",
					line:    item.Line,
					message: "volumes mount /var/run/docker.sock",
				})
				break // one finding per service is enough
			}
		}
	}

	// missing memory limit. Skip services that are explicitly build-only
	// (no `image`/`command`/`entrypoint`) — they're build contexts, not
	// runtime services, and OOM doesn't apply to them.
	if isRuntimeService(svc) {
		if !hasMemoryLimit(svc) {
			out = append(out, composeFinding{
				ruleID: "missing_memory_limit",
				// Pin to the service's own line so the finding lands at
				// the service definition, not somewhere arbitrary.
				line:    svc.Line,
				message: "deploy.resources.limits.memory not set (and no mem_limit fallback)",
			})
		}
	}

	return out
}

func isRuntimeService(svc *yaml.Node) bool {
	for _, key := range []string{"image", "command", "entrypoint"} {
		if mappingValue(svc, key) != nil {
			return true
		}
	}
	return false
}

func hasMemoryLimit(svc *yaml.Node) bool {
	// Legacy v2 syntax: top-level mem_limit on the service.
	if v := mappingValue(svc, "mem_limit"); v != nil {
		return true
	}
	// v3+ syntax: deploy.resources.limits.memory
	deploy := mappingValue(svc, "deploy")
	if deploy == nil {
		return false
	}
	resources := mappingValue(deploy, "resources")
	if resources == nil {
		return false
	}
	limits := mappingValue(resources, "limits")
	if limits == nil {
		return false
	}
	return mappingValue(limits, "memory") != nil
}

func isDockerSocketMount(node *yaml.Node) bool {
	switch node.Kind {
	case yaml.ScalarNode:
		// Short form: "/var/run/docker.sock:/var/run/docker.sock"
		return strings.Contains(node.Value, "/var/run/docker.sock")
	case yaml.MappingNode:
		// Long form: { type: bind, source: /var/run/docker.sock, target: ... }
		if v := mappingValue(node, "source"); v != nil && v.Kind == yaml.ScalarNode {
			if strings.Contains(v.Value, "/var/run/docker.sock") {
				return true
			}
		}
	}
	return false
}

// collectComposeOverrides scans the raw file content for `# l0git: ignore`
// directives. Each override is keyed by its line; when a rule fires on
// line N, we look up overrides on line N and N-1 (allowing the directive
// either inline or on the line above the YAML node).
func collectComposeOverrides(content string) map[int]*gateOverride {
	out := map[int]*gateOverride{}
	for i, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		// In YAML the comment can start mid-line; chop everything before #.
		if idx := strings.Index(line, "#"); idx >= 0 {
			if ov := parseGateOverride(line[idx:]); ov != nil {
				ov.Line = i + 1
				out[ov.Line] = ov
			}
		}
	}
	return out
}

func ruleFinding(rel string, line int, rule composeRule, msg string, overrides map[int]*gateOverride) Finding {
	if ov := lookupOverrideForRule(line, overrides, rule.id); ov != nil {
		// The override's own .Line was set during collection; embed it
		// into a synthetic instruction so we can reuse the shared
		// audit-trail formatter.
		return overrideAcceptedFinding("compose_lint", rel, rule.id, dockerfileInstr{
			Line:     line,
			Override: ov,
		})
	}
	return Finding{
		Severity: rule.severity,
		Title:    rule.title,
		Message:  fmt.Sprintf("%s:%d %s. %s", rel, line, msg, rule.advice),
		FilePath: fmt.Sprintf("%s:%d:%s", rel, line, rule.id),
	}
}

// overrideLookbackLines is how far above a violation we scan for an
// override directive. Six lines covers the common YAML shape where the
// directive sits above a parent key (e.g. `volumes:`) rather than the
// nested item that actually trips the rule. Picked as a deterministic
// bound rather than "look until you hit something" so the override
// contract stays auditable line-by-line.
const overrideLookbackLines = 6

// lookupOverrideForRule scans upward from `line` and returns the first
// override directive that matches ruleID. Returns nil when none applies.
func lookupOverrideForRule(line int, overrides map[int]*gateOverride, ruleID string) *gateOverride {
	for delta := 0; delta <= overrideLookbackLines; delta++ {
		if ov, ok := overrides[line-delta]; ok && ov.matches(ruleID) {
			return ov
		}
	}
	return nil
}

func mappingNode(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.DocumentNode {
		if len(n.Content) == 0 {
			return nil
		}
		return mappingNode(n.Content[0])
	}
	if n.Kind == yaml.MappingNode {
		return n
	}
	return nil
}

func mappingValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(m.Content); i += 2 {
		k := m.Content[i]
		v := m.Content[i+1]
		if k.Value == key {
			return v
		}
	}
	return nil
}

func isTrueScalar(n *yaml.Node) bool {
	if n.Kind != yaml.ScalarNode {
		return false
	}
	v := strings.ToLower(strings.TrimSpace(n.Value))
	return v == "true" || v == "yes" || v == "on"
}

func parseComposeOptions(opts json.RawMessage) composeLintOptions {
	if len(opts) == 0 {
		return composeLintOptions{}
	}
	var o composeLintOptions
	_ = json.Unmarshal(opts, &o)
	return o
}
