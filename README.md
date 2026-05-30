# mcp-launcher

A secure, cross-platform launcher for MCP (Model Context Protocol) servers that keeps your API keys out of config files.

---

## The Problem

When using MCP servers with AI tools like Claude Desktop, you're typically required to store API keys directly in config files:

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

`mcp-launcher` acts as a secure wrapper that sits between your AI tool and the MCP server.

```
AI Tool (Claude Desktop / etc.)
    ↓
mcp-launcher  ← fetches token from OS keystore, injects into env
    ↓
MCP Server (github-mcp-server, etc.)
```

Your config files contain **no secrets**. The MCP server receives its token via environment variables, just as before — it never knows the difference.

---

## Features

### Phase 1 (Released)
- ✅ Launch any MCP server with secrets injected from the OS keystore
- ✅ No secrets in config files
- ✅ No secrets in chat logs
- ✅ Works with any MCP server — no modifications required
- ✅ Centralized key management across all AI tools
- ✅ Cross-platform: Windows, macOS, Linux

### Phase 2 (Released)
- ✅ Automatic token rotation via GitHub App
- ✅ Short-lived tokens (max 1 hour, vs forever for personal access tokens)
- ✅ Automatic refresh on each launch — no need to restart your AI tool manually

### Phase 3 (Planned)
- 🔐 Passkey / FIDO2 authentication before unlocking secrets
- 🔐 Smartphone authentication via CTAP2 hybrid (for PCs without cameras)
- 🔐 Windows Hello, Touch ID, Face ID support

---

## How It Works

### Phase 1: Static token (simple setup)

```
mcp-launcher github
    ↓
Reads launcher.json → finds "github" config
    ↓
Fetches GITHUB_PERSONAL_ACCESS_TOKEN from OS keystore
    ↓
Injects into env → starts github-mcp-server
```

The token lives only in the child process's environment. It never touches a file or a chat message.

### Phase 2: Short-lived token via GitHub App

```
mcp-launcher github
    ↓
Reads launcher.json → finds token_source config
    ↓
Reads EXPIRY from keystore
  ├ No EXPIRY (first run) → fetch new token from GitHub App API
  ├ Expiring soon (within refresh_before_seconds) → fetch new token
  └ Still valid → skip refresh
    ↓
Fetches GITHUB_PERSONAL_ACCESS_TOKEN from keystore
    ↓
Injects into env → starts github-mcp-server
```

**What the MCP server sees:** only the short-lived access token (valid max 1 hour).

**What never reaches the MCP server:** App ID, private key, Installation ID. These stay inside mcp-launcher's process and are used solely to obtain the access token.

---

## Security Model

### What is stored where

| Secret | Storage | Visible to MCP server |
|---|---|---|
| GitHub App ID | OS keystore | ❌ No |
| GitHub App private key (RSA) | OS keystore | ❌ No |
| Installation ID | OS keystore | ❌ No |
| Access token (max 1h) | OS keystore (refreshed automatically) | ✅ Yes |

The App ID, private key, and Installation ID are registered manually once via the CLI. They are never written to any file and never passed to the MCP server process.

### What this protects against

- ✅ API keys stored in plain text config files
- ✅ API keys accidentally committed to Git
- ✅ API keys leaking into AI chat logs
- ✅ Key sprawl across multiple tools and config locations
- ✅ Long-lived token exposure — even if the MCP server leaks the token, it expires within 1 hour

### What this does NOT protect against

- ❌ An attacker who already has access to your logged-in OS session (they can read the keystore too)
- ❌ Malware with the ability to read process environment variables
- ❌ A malicious MCP server that exfiltrates the short-lived token via tool responses — the token is still real and usable for up to 1 hour
- ❌ You manually pasting a token into a chat window
- ❌ Leakage of the GitHub App private key — if that is compromised, revoke it immediately in your GitHub App settings

**Think of it like a bank vault door on your house.** It won't help if someone is already inside, but it prevents you from leaving your keys on the front porch.

---

## Installation

> 📦 Pre-built binaries coming soon.

### Build mcp-launcher from source

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

---

## Quick Start: Phase 1 (Static PAT)

### 1. Register your token

```bash
mcp-launcher register github GITHUB_PERSONAL_ACCESS_TOKEN ghp_yourtoken
```

### 2. Create `launcher.json`

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

### 3. Configure Claude Desktop

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

---

## Phase 2: Short-lived Tokens via GitHub App

Phase 2 replaces the long-lived Personal Access Token with short-lived installation access tokens (max 1 hour) issued by a GitHub App. The token is automatically refreshed on each launch.

### Step 1: Create a GitHub App

1. Go to https://github.com/settings/apps/new
2. Fill in:
   - **App name**: any name (e.g. `mcp-launcher-yourname`)
   - **Homepage URL**: any URL
   - **Webhook**: disable
   - **Permissions**: grant only what you need (e.g. `Contents: Read & Write`, `Issues: Read & Write`)
3. Click **Create GitHub App** — note the **App ID** shown at the top of the settings page

### Step 2: Generate a private key

On the App settings page, scroll to **Private keys** → **Generate a private key**. A `.pem` file will be downloaded.

### Step 3: Install the App

On the App settings page, go to **Install App** → select your account or organization → choose **All repositories** or specific repositories. The URL after installation will be:

```
https://github.com/settings/installations/1234567
```

The number at the end is your **Installation ID**.

> **Multiple organizations**: Each organization requires a separate installation. Create one `launcher.json` entry per organization with its own `installation_id_key`.

### Step 4: Register secrets in the keystore

```bash
mcp-launcher register github APP_ID 123456
mcp-launcher register github PRIVATE_KEY "$(cat path/to/private-key.pem)"
mcp-launcher register github INSTALLATION_ID 7654321
```

### Step 5: Update `launcher.json`

```json
{
  "github": {
    "command": "C:\\path\\to\\github-mcp-server.exe",
    "args": ["stdio"],
    "env_keys": {
      "GITHUB_PERSONAL_ACCESS_TOKEN": "mcp-launcher/github/GITHUB_PERSONAL_ACCESS_TOKEN"
    },
    "token_source": {
      "type": "github_app",
      "app_id_key": "mcp-launcher/github/APP_ID",
      "private_key_key": "mcp-launcher/github/PRIVATE_KEY",
      "installation_id_key": "mcp-launcher/github/INSTALLATION_ID",
      "target_env_key": "GITHUB_PERSONAL_ACCESS_TOKEN",
      "refresh_before_seconds": 600
    }
  }
}
```

`refresh_before_seconds: 600` means the token will be refreshed if it expires within 10 minutes of the next launch.

### How token refresh works

- Token is fetched from GitHub App API and stored in the keystore on first launch
- On each subsequent launch, mcp-launcher checks the expiry
- If the token is still valid, it is reused (no API call)
- If expired or expiring soon, a new token is fetched automatically
- If the refresh fails (e.g. no network), mcp-launcher falls back to the existing token and logs a warning — it does not stop

> **Note**: The access token may be expired if you haven't used Claude Desktop for more than 1 hour. It will be refreshed automatically on the next launch — no manual action required.

### Current limitations

- **GitHub only**: Phase 2 token rotation is implemented for GitHub App only. Other services (AWS, Azure, GCP) use static secrets via Phase 1 for now.
- **No background refresh**: The token is only refreshed at launch time. If a session runs longer than 1 hour, the token used by the MCP server will expire mid-session. The MCP server itself does not re-request a token.
- **Private key security**: The GitHub App private key is stored in the OS keystore, which is more secure than a file, but it is a long-lived credential. If compromised, revoke it immediately from your GitHub App settings.

---

## Supported Platforms

| Platform | Keystore Backend | Passkey (Phase 3) |
|---|---|---|
| Windows | Credential Manager (DPAPI) | Windows Hello / FIDO2 |
| macOS | Keychain | Touch ID / Face ID / FIDO2 |
| Linux | libsecret / kwallet | FIDO2 |

---

## Security Notes for Contributors

- Never commit real tokens or credentials, even in tests
- Keep `launcher.json` in `.gitignore` — only `launcher.example.json` belongs in the repo
- The GitHub App private key must never be stored as a file; use the keystore
- Please report security issues via GitHub Security Advisories, not public Issues

---

## Roadmap

| Phase | Status | Description |
|---|---|---|
| 1 | ✅ Released | Secure launcher with OS keystore integration |
| 2 | ✅ Released | Automatic token rotation via GitHub App (GitHub only) |
| 3 | 📋 Planned | FIDO2 / passkey authentication |

---

## License

MIT
