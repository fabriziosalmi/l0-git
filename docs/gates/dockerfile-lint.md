# Dockerfile Lint

The `dockerfile_lint` gate performs a deterministic AST-based analysis of your Dockerfiles to ensure best practices and security compliance.

## Details

- **Gate ID**: `dockerfile_lint`
- **Severity**: Warning
- **Tags**: `containers`, `security`, `build`

## Rules

This gate checks for several common Dockerfile issues:

| Rule ID | Description |
|---------|-------------|
| `from_untagged` | Using a base image without a specific tag (e.g., `FROM node`). |
| `from_latest` | Using the `:latest` tag, which leads to non-deterministic builds. |
| `add_instruction` | Using `ADD` instead of `COPY`. `COPY` is preferred for clarity. |
| `missing_user` | The Dockerfile does not define a `USER`. |
| `user_root` | The Dockerfile explicitly sets `USER root`. |

## Inline Overrides

You can ignore specific rules using comments:

```dockerfile
# l0git: ignore from_latest reason: dev-only image
FROM node:latest
```

## Configuration

You can opt-in to suggestions even when a Dockerfile is missing:

```json
{
  "gate_options": {
    "dockerfile_lint": {
      "suggest_when_missing": true,
      "disabled_rules": ["add_instruction"]
    }
  }
}
```
