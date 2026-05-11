# Getting Started

Learn how to install and set up l0-git in your workspace.

## Installation

### Pre-built Binaries

The recommended way to install l0-git is to download the latest release for your platform from the [GitHub Releases](https://github.com/fabriziosalmi/l0-git/releases) page.

1. Download the archive for your OS and architecture.
2. Extract the `lgit` binary.
3. Move it to a directory in your `PATH` (e.g., `/usr/local/bin` or `~/.local/bin`).

### From Source

If you have Go installed, you can build l0-git from source:

```bash
git clone https://github.com/fabriziosalmi/l0-git.git
cd l0-git
make build
```

This will produce the `lgit` binary in the `server/` directory.

## VS Code Extension

For the best experience, install the l0-git VS Code extension:

1. Open VS Code.
2. Go to the Extensions view (`Ctrl+Shift+X`).
3. Search for `l0-git` and install it.

Alternatively, you can install the `.vsix` file from the releases page:

```bash
code --install-extension l0-git-<version>.vsix
```

## Running Your First Scan

Once installed, you can run a scan against your project:

```bash
lgit check .
```

To see the findings:

```bash
lgit list
```

## Configuration

l0-git works out of the box, but you can customize it by creating a `.l0git.json` file in your project root. See the [Configuration](./configuration) guide for details.
