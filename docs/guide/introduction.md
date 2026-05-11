# Introduction

l0-git is a deterministic project-hygiene quality gate system for the open workspace. It provides a set of automated checks (gates) that ensure your repository adheres to high standards of hygiene, security, and governance.

## Core Philosophy

The project is built around a single principle: **a gate fires if and only if the violation can be expressed as a binary, mathematically unambiguous condition over the file system or an AST**. 

Unlike many tools that rely on probabilistic signals or "AI-powered" context understanding for basic hygiene, l0-git focuses on ground truth. This ensures that:

1. **No False Positives**: If a gate fires, there is a clear violation.
2. **Speed**: Checks are extremely fast, implemented in pure Go.
3. **Reproducibility**: Results are deterministic across different environments.

## Architecture

l0-git consists of three main components:

- **Core Engine**: A single Go binary (`lgit`) that performs the scans and manages findings in a SQLite database.
- **MCP Server**: The binary can speak the [Model Context Protocol](https://modelcontextprotocol.io/) over stdio, allowing integration with LLM agents like Claude Code.
- **VS Code Extension**: A companion extension that watches your workspace and surfaces findings in real-time.

## Key Features

- **34 Built-in Gates**: Comprehensive coverage across project hygiene, security, git hygiene, accessibility, frontend quality, containers, governance, and documentation.
- **Configurable**: Fine-tune gates per-project via `.l0git.json`.
- **Inline Overrides**: Opt-out of specific rules with adjacent comments in your source files.
- **Findings Persistence**: Findings are stored and managed, allowing for history tracking and resolution workflows.
