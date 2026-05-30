# mcp-launcher

A secure, cross-platform launcher for MCP (Model Context Protocol) servers that keeps your API keys out of config files.

---

## The Problem

When using MCP servers with AI tools like Claude Desktop or TypingMind, you're typically required to store API keys directly in config files:

```json
{
  "mcpServers": {
    "github": {
      "env": {
        "GITHUB_PERSONAL_ACCESS_TOKEN": "ghp_xxxxxxxxxxxx"
      }
    }
  }
}
```

This means:
- API keys are stored in **plain text**
- Keys are scattered across **multiple config files** per tool
- Risk of **accidentally committing keys** to Git
- Keys are often **never rotated** because updating them manually is tedious
- Keys appear in **chat logs** if you ever paste them for troubleshooting

---

## The Solution

`mcp-launcher` acts as a secure wrapper — a launcher — that sits between your AI tool and the MCP server.

```
AI Tool (Claude Desktop / TypingMind / etc.)
    ↓
mcp-launcher  ← fetches token from OS keystore, injects into env
    ↓
MCP Server (github-mcp-server, aws-mcp-server, etc.)
```

Your config files contain **no secrets**. The MCP server receives its token via environment variables, just as before — it never knows the difference.

---

## Features

### Phase 1 (Current)
- ✅ Launch any MCP server with secrets injected from the OS keystore
- ✅ No secrets in config files
- ✅ No secrets in chat logs
- ✅ Works with any MCP server — no modifications required
- ✅ Centralized key management across all AI tools
- ✅ Cross-platform: Windows, macOS, Linux

### Phase 2 (Planned)
- 🔄 Automatic token rotation via GitHub App
- 🔄 Short-lived tokens (minutes, not forever)
- 🔄 Background refresh without restarting your AI tool

### Phase 3 (Planned)
- 🔐 Passkey / FIDO2 authentication before unlocking secrets
- 🔐 Smartphone authentication via CTAP2 hybrid (for PCs without cameras)
- 🔐 Windows Hello, Touch ID, Face ID support

---

## How It Works

### At launch time

```
mcp-launcher github
    ↓
Reads launcher.json → finds "github" config
    ↓
Fetches GITHUB_PERSONAL_ACCESS_TOKEN from OS keystore (Credential Manager / Keychain / libsecret)
    ↓
Sets env variables
    ↓
Starts github-mcp-server as a child process
```

The token lives only in the child process's environment. It never touches a file or a chat message.

### Config file (no secrets)

```json
{
  "github": {
    "command": "C:\\path\\to\\github-mcp-server.exe",
    "args": ["stdio"],
    "env_keys": {
      "GITHUB_PERSONAL_ACCESS_TOKEN": "mcp-launcher/github/GITHUB_PERSONAL_ACCESS_TOKEN"
    }
  },
  "aws": {
    "command": "aws-mcp-server",
    "env_keys": {
      "AWS_ACCESS_KEY_ID": "mcp-launcher/aws/AWS_ACCESS_KEY_ID",
      "AWS_SECRET_ACCESS_KEY": "mcp-launcher/aws/AWS_SECRET_ACCESS_KEY"
    }
  }
}
```

### Claude Desktop integration

```json
{
  "mcpServers": {
    "github": {
      "command": "mcp-launcher",
      "args": ["github"]
    }
  }
}
```

---

## Installation

> 📦 Pre-built binaries coming soon.

### Build from source

```bash
git clone https://github.com/masuda-masuo/mcp-launcher
cd mcp-launcher
go build -o mcp-launcher ./cmd/launcher
```

### Set up the GitHub MCP Server

Download the official GitHub MCP Server binary from:
https://github.com/github/github-mcp-server/releases

> **Note**: The npm package `@modelcontextprotocol/server-github` is deprecated as of April 2025. Use the official binary above instead.

> **Note (Windows)**: Claude Desktop does not inherit the system PATH, so you must use the full absolute path to `github-mcp-server.exe` in your `launcher.json`.

### Register a secret

```bash
mcp-launcher register github GITHUB_PERSONAL_ACCESS_TOKEN ghp_yourtoken
```

This stores the token in your OS keystore. You can then delete it from any config files.

---

## Supported Platforms

| Platform | Keystore Backend | Passkey (Phase 3) |
|---|---|---|
| Windows | Credential Manager (DPAPI) | Windows Hello / FIDO2 |
| macOS | Keychain | Touch ID / Face ID / FIDO2 |
| Linux | libsecret / kwallet | FIDO2 |

---

## Threat Model

### What this tool protects against

- ✅ API keys stored in plain text config files
- ✅ API keys accidentally committed to Git
- ✅ API keys leaking into AI chat logs
- ✅ Key sprawl across multiple tools and config locations
- ✅ Long-lived static tokens (Phase 2: automatic rotation)

### What this tool does NOT protect against

- ❌ An attacker who already has access to your logged-in OS session
- ❌ Malware with the ability to read process environment variables
- ❌ Malicious MCP servers that exfiltrate tokens through tool responses
- ❌ You manually pasting a token into a chat window

**Think of it like a bank vault door on your house.** It won't help if someone is already inside, but it prevents you from leaving your keys on the front porch.

---

## Security Notes for Contributors

- Never commit real tokens or credentials, even in tests
- Keep `launcher.json` in `.gitignore` — only `launcher.example.json` belongs in the repo
- The GitHub App private key (Phase 2) must never be stored as a file; use the keystore
- Please report security issues via GitHub Security Advisories, not public Issues

---

## Roadmap

| Phase | Status | Description |
|---|---|---|
| 1 | 🚧 In Progress | Secure launcher with OS keystore integration |
| 2 | 📋 Planned | Automatic token rotation via GitHub App |
| 3 | 📋 Planned | FIDO2 / passkey authentication |

---

## License

MIT
