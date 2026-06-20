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

**Phase 2** ⭐ **Recommended** — Short-lived tokens (max 1 hour). Transparent refresh: mcp-launcher proxies the MCP stream and restarts the server with a fresh token without the AI tool noticing. Long-lived credentials never need to be registered in the keystore.

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

## Quick Start: Phase 2 (Recommended)

Phase 2 uses short-lived tokens that expire in at most 1 hour and are refreshed automatically. Long-lived credentials stay in the standard credential chain and are never stored in the launcher keystore.

- **[GitHub App setup →](docs/setup/github-app-setup.md)**
- **[AWS STS setup →](docs/setup/aws-sts-setup.md)**

### Example: AWS STS

**1. Register the IAM Role ARN**

```bash
mcp-launcher register my-aws-mcp ROLE_ARN arn:aws:iam::123456789012:role/MyMCPRole
```

**2. Create `launcher.json`** (place in the same directory as `mcp-launcher.exe`)

```json
{
  "my-aws-mcp": {
    "command": "C:\\path\\to\\aws-mcp-server.exe",
    "args": [],
    "env_keys": {
      "AWS":                   "AWS_ACCESS_KEY_ID",
      "AWS_ACCESS_KEY_ID":     "AWS_ACCESS_KEY_ID",
      "AWS_SECRET_ACCESS_KEY": "AWS_SECRET_ACCESS_KEY",
      "AWS_SESSION_TOKEN":     "AWS_SESSION_TOKEN"
    },
    "token_source": {
      "type": "aws_sts",
      "role_arn_key": "mcp-launcher/my-aws-mcp/ROLE_ARN",
      "role_session_name": "mcp-launcher-my-aws-mcp",
      "duration_seconds": 3600,
      "target_env_key": "AWS",
      "refresh_before_seconds": 600
    },
    "check_interval_seconds": 60,
    "drain_timeout_seconds": 30
  }
}
```

**3. Configure Claude Desktop**

```json
{
  "mcpServers": {
    "my-aws-mcp": {
      "command": "C:\\path\\to\\mcp-launcher.exe",
      "args": ["my-aws-mcp"]
    }
  }
}
```

> **Note (Windows)**: Claude Desktop does not inherit the system PATH, so use the full absolute path to executables.
>
> **Note**: `launcher.json` is automatically loaded from the same directory as `mcp-launcher.exe`. No extra configuration needed.

### Example: GitHub App

**1. Register GitHub App credentials**

```bash
mcp-launcher register github APP_ID 123456
mcp-launcher register github PRIVATE_KEY "$(cat private-key.pem)"
mcp-launcher register github INSTALLATION_ID 78901234
```

**2. Create `launcher.json`**

```json
{
  "github": {
    "command": "C:\\path\\to\\github-mcp-server.exe",
    "args": ["stdio"],
    "env_keys": {
      "GITHUB_PERSONAL_ACCESS_TOKEN": "mcp-launcher/github/github-app"
    },
    "token_source": {
      "type": "github_app",
      "app_id_key": "mcp-launcher/github/APP_ID",
      "private_key_key": "mcp-launcher/github/PRIVATE_KEY",
      "installation_id_key": "mcp-launcher/github/INSTALLATION_ID",
      "target_env_key": "GITHUB_PERSONAL_ACCESS_TOKEN",
      "refresh_before_seconds": 600
    },
    "check_interval_seconds": 60
  }
}
```

---

## Phase 1: Static Token (Reference)

Phase 1 stores a long-lived token in the OS keystore and injects it at launch. No rotation. Useful for MCP servers that don't support short-lived tokens.

See [Phase 1 reference →](docs/setup/phase1-static-token.md)

---

## Further Reading

- [Security Model](docs/architecture/security-model.md) — what is stored where, threat model, prompt injection analysis
- [Configuration Reference](docs/architecture/configuration-reference.md) — all `launcher.json` fields
- [WSL Setup](docs/setup/wsl-setup.md) — how to use Windows Credential Manager from WSL
- [Adding a Token Source](docs/architecture/adding-a-token-source.md) — guide for contributors

---

## License

MIT
