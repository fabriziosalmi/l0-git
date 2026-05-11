# Configuration

l0-git can be configured via a `.l0git.json` file in the project root. This file allows you to ignore specific gates, override their severity, and pass gate-specific options.

## Schema

The configuration file is a JSON object with three primary top-level fields:

| Field | Type | Description |
|-------|------|-------------|
| `ignore` | `string[]` | List of gate IDs to skip entirely. |
| `severity` | `Record<string, string>` | Override the default severity for specific gates (`error`, `warning`, `info`). |
| `gate_options` | `Record<string, object>` | Per-gate options (e.g., `exclude_paths`, `threshold_mb`). |

## Example

```json
{
  "ignore": ["changelog_present", "pr_template_present"],
  "severity": {
    "readme_present": "info",
    "secrets_scan": "warning"
  },
  "gate_options": {
    "large_file_tracked": { 
      "threshold_mb": 10, 
      "exclude_paths": ["dist/**"] 
    },
    "secrets_scan": { 
      "exclude_paths": ["test/fixtures/**"] 
    },
    "secrets_scan_history": { 
      "enabled": true, 
      "max_blobs": 10000 
    }
  }
}
```

## Inline Overrides

For language-specific gates (Dockerfile, Compose, HTML, Markdown, CSS), you can use inline comments to ignore a rule for a specific line or block.

### Dockerfile
```dockerfile
# l0git: ignore from_latest reason: dev base image
FROM node:latest
```

### Docker Compose
```yaml
services:
  proxy:
    image: traefik:v3
    # l0git: ignore docker_socket_mount reason: required for routing
    volumes:
      - "/var/run/docker.sock:/var/run/docker.sock:ro"
```

### HTML
```html
<!-- l0git: ignore viewport_no_zoom reason: legacy app requirement -->
<meta name="viewport" content="width=device-width, user-scalable=no">
```

### Markdown
```markdown
<!-- l0git: ignore image_no_alt reason: decorative image -->
![](./logo.png)
```

### CSS
```css
/* l0git: ignore thin_font_weight reason: brand identity */
body {
  font-weight: 100;
}
```
