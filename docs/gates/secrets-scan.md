# Secrets Scan

The `secrets_scan` gate identifies hardcoded secrets, API keys, and other sensitive information in your tracked files.

## Details

- **Gate ID**: `secrets_scan`
- **Severity**: Error
- **Tags**: `security`, `git-hygiene`

## What it Checks

This gate scans every git-tracked file for well-known secret patterns, including:

- **Cloud Providers**: AWS Access Keys, Google Cloud API Keys.
- **AI Services**: OpenAI, Anthropic, Cohere, HuggingFace API keys.
- **SaaS Platforms**: GitHub Personal Access Tokens, Slack Webhooks, Stripe Secret Keys.
- **Standard Formats**: JSON Web Tokens (JWT), Private Key headers (PEM), SSH keys.
- **Files**: Tracked `.env` files which should typically be ignored.

## How it Works

l0-git uses a high-performance regex engine to scan files returned by `git ls-files`. This ensures that only files intended to be in the repository are scanned, respecting your `.gitignore` rules.

## Remediation

If this gate fires:

1. **Rotate the Secret**: Immediately invalidate the exposed secret at the provider's side.
2. **Remove the Secret**: Delete the secret from the file.
3. **Clean History**: If the secret was committed, consider using `git filter-repo` to remove it from the git history.
4. **Ignore if False Positive**: Use an inline override if the detected string is not actually a secret (e.g., a test fixture).

## Configuration

You can exclude specific paths from the secrets scan in `.l0git.json`:

```json
{
  "gate_options": {
    "secrets_scan": {
      "exclude_paths": ["test/fixtures/**"]
    }
  }
}
```
