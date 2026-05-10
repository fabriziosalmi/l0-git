package main

import (
	"context"
	"strings"
	"testing"
)

func runComposeRules(t *testing.T, src string) []Finding {
	t.Helper()
	return evaluateComposeFile("compose.yml", []byte(src), nil)
}

func TestCompose_PrivilegedTrue(t *testing.T) {
	src := `services:
  web:
    image: nginx:1.27
    privileged: true
    deploy:
      resources:
        limits:
          memory: 256M
`
	fs := runComposeRules(t, src)
	if findFindingByRule(fs, "privileged_true") == nil {
		t.Fatalf("expected privileged_true, got: %+v", fs)
	}
}

func TestCompose_NetworkModeHost(t *testing.T) {
	src := `services:
  web:
    image: nginx:1.27
    network_mode: host
    deploy:
      resources:
        limits:
          memory: 256M
`
	fs := runComposeRules(t, src)
	if findFindingByRule(fs, "network_mode_host") == nil {
		t.Fatalf("expected network_mode_host, got: %+v", fs)
	}
}

func TestCompose_DockerSocketShortAndLongForm(t *testing.T) {
	short := `services:
  proxy:
    image: traefik:v3
    volumes:
      - "/var/run/docker.sock:/var/run/docker.sock:ro"
    deploy:
      resources:
        limits:
          memory: 128M
`
	long := `services:
  proxy:
    image: traefik:v3
    volumes:
      - type: bind
        source: /var/run/docker.sock
        target: /var/run/docker.sock
    deploy:
      resources:
        limits:
          memory: 128M
`
	for name, src := range map[string]string{"short": short, "long": long} {
		t.Run(name, func(t *testing.T) {
			fs := runComposeRules(t, src)
			// Traefik is a known orchestrator image — finding must be demoted to info.
			f := findFindingByRule(fs, "docker_socket_mount_orchestrator")
			if f == nil {
				t.Errorf("expected docker_socket_mount_orchestrator (info) for Traefik %s form, got: %+v", name, fs)
			} else if f.Severity != SeverityInfo {
				t.Errorf("Traefik socket mount must be info, got: %s", f.Severity)
			}
			// Must NOT produce the warning-level variant.
			if findFindingByRule(fs, "docker_socket_mount") != nil {
				t.Errorf("Traefik must not produce warning-level docker_socket_mount")
			}
		})
	}
}

// A non-orchestrator image mounting the socket keeps the warning.
func TestCompose_DockerSocketNonOrchestratorWarns(t *testing.T) {
	src := `services:
  app:
    image: myapp:latest
    volumes:
      - "/var/run/docker.sock:/var/run/docker.sock"
    deploy:
      resources:
        limits:
          memory: 128M
`
	fs := runComposeRules(t, src)
	if findFindingByRule(fs, "docker_socket_mount") == nil {
		t.Errorf("non-orchestrator image must produce docker_socket_mount warning, got: %+v", fs)
	}
}

func TestCompose_MissingMemoryLimitV3(t *testing.T) {
	src := `services:
  web:
    image: nginx:1.27
`
	fs := runComposeRules(t, src)
	if findFindingByRule(fs, "missing_memory_limit") == nil {
		t.Fatalf("expected missing_memory_limit, got: %+v", fs)
	}
}

// Legacy v2 mem_limit satisfies the rule.
func TestCompose_MemLimitV2OK(t *testing.T) {
	src := `services:
  web:
    image: nginx:1.27
    mem_limit: 256m
`
	fs := runComposeRules(t, src)
	if findFindingByRule(fs, "missing_memory_limit") != nil {
		t.Errorf("mem_limit must satisfy missing_memory_limit, got: %+v", fs)
	}
}

// Build-only services (no image/command/entrypoint) shouldn't trip the
// memory rule — they're not runtime workloads.
func TestCompose_BuildOnlyServiceSkipsMemoryRule(t *testing.T) {
	src := `services:
  builder:
    build:
      context: .
`
	fs := runComposeRules(t, src)
	if findFindingByRule(fs, "missing_memory_limit") != nil {
		t.Errorf("build-only service must skip memory rule, got: %+v", fs)
	}
}

func TestCompose_YAMLInvalid(t *testing.T) {
	src := "services: { unbalanced: [\n"
	fs := runComposeRules(t, src)
	if findFindingByRule(fs, "yaml_invalid") == nil {
		t.Fatalf("expected yaml_invalid, got: %+v", fs)
	}
}

// Valid YAML with no `services:` key is silent.
func TestCompose_NoServicesNoFinding(t *testing.T) {
	src := `version: "3.9"
volumes:
  data: {}
`
	fs := runComposeRules(t, src)
	if len(fs) != 0 {
		t.Errorf("services-less compose: expected no findings, got: %+v", fs)
	}
}

// Inline override on the line above the offending key silences the rule
// and emits an audit-trail info finding instead.
func TestCompose_InlineOverride(t *testing.T) {
	src := `services:
  proxy:
    image: traefik:v3
    # l0git: ignore docker_socket_mount reason: traefik label-based routing
    volumes:
      - "/var/run/docker.sock:/var/run/docker.sock:ro"
    deploy:
      resources:
        limits:
          memory: 128M
`
	fs := runComposeRules(t, src)
	// Neither the warning nor the orchestrator-info variant must survive the override.
	if findFindingByRule(fs, "docker_socket_mount") != nil {
		t.Errorf("override must silence docker_socket_mount warning: %+v", fs)
	}
	if findFindingByRule(fs, "docker_socket_mount_orchestrator") != nil {
		t.Errorf("override must also silence docker_socket_mount_orchestrator: %+v", fs)
	}
	// An audit-trail override_accepted finding must appear.
	found := false
	for _, f := range fs {
		if strings.HasSuffix(f.FilePath, ":override_docker_socket_mount") ||
			strings.HasSuffix(f.FilePath, ":override_docker_socket_mount_orchestrator") {
			found = true
			if f.Severity != SeverityInfo {
				t.Errorf("override severity = %q, want info", f.Severity)
			}
			if !strings.Contains(f.Message, "traefik label-based routing") {
				t.Errorf("audit message must include the reason; got %q", f.Message)
			}
		}
	}
	if !found {
		t.Errorf("expected override audit finding, got: %+v", fs)
	}
}

// disabled_rules suppresses everything for that rule, including the
// override_accepted info — it's a stronger silence than the inline form.
func TestCompose_DisabledRules(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		"compose.yml": `services:
  web:
    image: nginx:1.27
    privileged: true
`,
	})
	opts := []byte(`{"disabled_rules": ["privileged_true", "missing_memory_limit"]}`)
	fs, err := checkComposeLint(context.Background(), root, opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fs {
		if strings.Contains(f.FilePath, "privileged_true") ||
			strings.Contains(f.FilePath, "missing_memory_limit") {
			t.Errorf("disabled rule must not appear: %+v", f)
		}
	}
}

func TestCompose_SuggestWhenMissing(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)
	runGit(t, root, "config", "user.email", "t@t")
	runGit(t, root, "config", "user.name", "t")
	runGit(t, root, "commit", "--allow-empty", "-q", "-m", "init")

	// Default: silent.
	fs, err := checkComposeLint(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 0 {
		t.Errorf("default must be silent, got: %+v", fs)
	}

	// Opt-in: one info finding.
	fs2, err := checkComposeLint(context.Background(), root, []byte(`{"suggest_when_missing": true}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(fs2) != 1 || fs2[0].Severity != SeverityInfo {
		t.Errorf("expected 1 info finding, got: %+v", fs2)
	}
}
