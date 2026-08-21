---
title: "Compose lint"
description: "Deterministic YAML-AST lint of tracked compose files. Fires for: invalid YAML, privileged services, host networking, /var/run/docker.sock mounts…"
---

# Compose lint

A YAML-AST lint over tracked Compose files, aimed at the settings that hand a container the host.

<GateMeta id="compose_lint" severity="warning" tags="containers,security,build" scope="Tracked files (`git ls-files`)" />

## What it checks

Deterministic YAML-AST lint of tracked compose files. Fires for: invalid YAML, privileged services, host networking, /var/run/docker.sock mounts, missing memory limits. Inline override via `# l0git: ignore <rule_id> reason: …`. Silent on repos without a compose file (set gate_options.compose_lint.suggest_when_missing to opt in).

### Rules

| Rule | Severity | Fires when |
|---|---|---|
| `yaml_invalid` | warning | The file is not valid YAML, or does not decode into a service map |
| `privileged_true` | warning | `privileged: true` |
| `network_mode_host` | warning | `network_mode: host` |
| `docker_socket_mount` | warning | `/var/run/docker.sock` is mounted in |
| `docker_socket_mount_orchestrator` | info | …but the image is a known orchestrator |
| `missing_memory_limit` | info | No `deploy.resources.limits.memory` (or `mem_limit`) |

### The orchestrator carve-out

Traefik, Portainer, Watchtower, Caddy, autoheal and friends require the Docker
socket to do their job. Reporting those at warning was noise, so a recognised
orchestrator image downgrades `docker_socket_mount` to an info finding that
says the mount is expected. Add your own images with
`additional_orchestrator_images`.

## What a finding says

```text
docker-compose.yml:4 privileged: true. `privileged: true` gives the container near-root access on the host. Drop it unless the workload genuinely needs it (and document why).
```

## Options

```json
{
  "gate_options": {
    "compose_lint": {
      "disabled_rules": ["missing_memory_limit"],
      "additional_orchestrator_images": ["my-org/deployer"],
      "suggest_when_missing": false
    }
  }
}
```

```yaml
# l0git: ignore docker_socket_mount reason: this IS the deploy agent
volumes:
  - /var/run/docker.sock:/var/run/docker.sock
```

## Turning it off

Silence the gate for the whole project in `.l0git.json`:

```json
{
  "ignore": ["compose_lint"]
}
```

Or keep it running at a lower severity:

```json
{
  "severity": { "compose_lint": "info" }
}
```

For a single occurrence, prefer the inline directive — it records the reason next to the code:

```text
# l0git: ignore <rule_id> reason: …
```

## See also

- [Dockerfile lint](/gates/dockerfile-lint)
- [Config parse error](/gates/config-parse-error)
