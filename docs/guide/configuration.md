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

## Scan options

Every gate that reads file *contents* accepts the same three options, on top of
its own. They exist because a scanner that cannot be told where not to look
becomes noise, and noise gets switched off entirely.

| Option | Default | What it does |
|---|---|---|
| `exclude_paths` | `[]` | Glob patterns (`filepath.Match` semantics) matched against the path relative to the project root. A match is skipped before the file is read. |
| `skip_default_fixture_paths` | `true` | Skip well-known test and fixture locations. |
| `skip_default_data_files` | `true` | Skip tabular and line-oriented data files. |

### Why fixtures are skipped by default

Test fixtures legitimately contain mock secrets, fake IP addresses and
placeholder URLs — that is what makes them fixtures. Scanning them produces
findings that are correct about the bytes and wrong about the meaning.

Covered: `*_test.go`, `test_*.py` / `*_test.py`, `*.test.{ts,tsx,js,jsx}`,
`*.spec.{ts,tsx,js,jsx}`, `*_test.rs`, `*Test.{java,kt}`, `*_spec.rb`,
`*_test.rb`, `conftest.py`, plus any path traversing `test/`, `tests/`,
`__tests__/`, `spec/`, `testdata/`, `fixtures/` or `__fixtures__/`.

Set it to `false` to scan them anyway:

```json
{
  "gate_options": {
    "secrets_scan": { "skip_default_fixture_paths": false }
  }
}
```

### Why data files are skipped by default

In a `.csv` of network ranges or a `.jsonl` of records, the addresses and keys
*are* the payload of the file, not literals embedded in source. Covered:
`.csv`, `.tsv`, `.jsonl`, `.ndjson`, `.parquet`, `.arrow`, `.feather`.

This applies to content-scanning gates only. Metadata gates —
`large_file_tracked`, `vendored_dir_tracked` and friends — still see these
files, because for them the size and the path are the point.

Some gates additionally detect address lists by content: `network_scan` treats
a `.txt` whose lines are overwhelmingly bare IP or CIDR literals as a data file
and gates that on this same flag.

## Where the config is read from

`.l0git.json` is read from the project root you pass to `lgit check`. There is
no user-level or global config file, and no merging: one project, one file.

An unparseable `.l0git.json` is itself reported, by the
[config parse error](/gates/config-parse-error) gate.

### Unknown keys are a hard error

The config is decoded with unknown fields disallowed, and severity values are
validated against `error` / `warning` / `info`. A typo does not fall back to the
default — it fails loudly:

```json
{ "ignroe": ["changelog_present"] }
```

```text
parse .l0git.json: json: unknown field "ignroe"
```

That is deliberate. A config that silently does nothing is worse than one that
refuses to load.
