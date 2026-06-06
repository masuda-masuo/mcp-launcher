# mcp-launcher

A secure, cross-platform launcher for MCP (Model Context Protocol) servers that keeps your API keys out of config files.

---

## The Problem

When using MCP servers with AI tools like Claude Desktop, you're typically required to store API keys directly in config files — in plain text, scattered across multiple locations, at risk of being committed to Git or leaking into chat logs.

## The Solution

`mcp-launcher` acts as a secure wrapper between your AI tool and the MCP server:

```
AI Tool (Claude Desktop / etc.)
    ↓↑  (JSON-RPC over stdio)
mcp-launcher  ← fetches token from OS keystore, injects into env,
                and proxies the MCP stream for transparent token rotation
    ↓↑
MCP Server (github-mcp-server, etc.)
```

Your config files contain **no secrets**. In Phase 2, mcp-launcher also acts as a JSON-RPC proxy so it can swap in a fresh token without the AI tool ever noticing.

---

## Features

| Phase | Status | Description |
|---|---|---|
| 1 | ✅ Released | Secure launcher with OS keystore integration |
| 2 | ✅ Released | Automatic token rotation via GitHub App and AWS STS |
| 3 | 📋 Planned | FIDO2 / passkey authentication |

**Phase 1** — No secrets in config files or chat logs. Works with any MCP server, no modifications required.

**Phase 2** — Short-lived tokens (max 1 hour). Transparent refresh: mcp-launcher proxies the MCP stream and restarts the server with a fresh token without the AI tool noticing.

---

## Installation

Pre-built binaries are on the [Releases](https://github.com/masuda-masuo/mcp-launcher/releases) page, or build from source:

```bash
git clone https://github.com/masuda-masuo/mcp-launcher
cd mcp-launcher
go build -o mcp-launcher ./cmd/launcher
```

**Linux only** — install a keystore backend:

```bash
# Debian / Ubuntu
sudo apt install libsecret-1-0 gnome-keyring
```

> Windows (Credential Manager) and macOS (Keychain) require no extra packages.

---

## Quick Start: Phase 1 (Static PAT)

> Copy [`launcher.example.json`](launcher.example.json) to `launcher.json` and adjust paths. Keep `launcher.json` out of version control.

**1. Register your token**

```bash
mcp-launcher register github GITHUB_PERSONAL_ACCESS_TOKEN ghp_yourtoken
```

**2. Create `launcher.json`**

```json
{
  "github": {
    "command": "C:\\path\\to\\github-mcp-server.exe",
    "args": ["stdio"],
    "env_keys": {
      "GITHUB_PERSONAL_ACCESS_TOKEN": "mcp-launcher/github/GITHUB_PERSONAL_ACCESS_TOKEN"
    }
  }
}
```

**3. Configure Claude Desktop**

```json
{
  "mcpServers": {
    "github": {
      "command": "C:\\path\\to\\mcp-launcher.exe",
      "args": ["github"]
    }
  }
}
```

> **Note (Windows)**: Claude Desktop does not inherit the system PATH, so use the full absolute path to executables.
>
> **Note**: The npm package `@modelcontextprotocol/server-github` is deprecated as of April 2025. Use the [official binary](https://github.com/github/github-mcp-server/releases) instead.

---

## Phase 2: Short-lived Tokens

Phase 2 replaces long-lived PATs with tokens that expire in at most 1 hour and are refreshed automatically.

- **[GitHub App setup →](docs/github-app-setup.md)**
- **[AWS STS setup →](docs/aws-sts-setup.md)**

---

## Further Reading

- [Security Model](docs/security-model.md) — what is stored where, threat model, prompt injection analysis
- [Configuration Reference](docs/configuration-reference.md) — all `launcher.json` fields
- [WSL Setup](docs/wsl-setup.md) — how to use Windows Credential Manager from WSL
- [Adding a Token Source](docs/adding-a-token-source.md) — guide for contributors

---

## Community

Discussed in the MCP community:

- 💡 [Ideas - Security: mcp-launcher](https://github.com/modelcontextprotocol/modelcontextprotocol/discussions/2849) _(modelcontextprotocol/modelcontextprotocol)_

---

## License

MIT
