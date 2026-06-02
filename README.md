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
    ↓↑  (JSON-RPC over stdio)
mcp-launcher  ← fetches token from OS keystore, injects into env,
                and proxies the MCP stream so it can transparently
                restart the server when the token rotates
    ↓↑
MCP Server (github-mcp-server, etc.)
```

Your config files contain **no secrets**. The MCP server receives its token via environment variables, just as before — it never knows the difference. In Phase 2, mcp-launcher also acts as a JSON-RPC proxy so it can swap in a fresh token without the AI tool ever noticing (see [Phase 2](#phase-2-short-lived-tokens-via-github-app)).

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
- ✅ Transparent refresh — mcp-launcher proxies the MCP stream and restarts the
  server with a fresh token without the AI tool noticing (no manual restart)

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

In Phase 2, mcp-launcher does **not** just pass frames straight through. Because an environment variable cannot be changed after a process has started, rotating a token requires restarting the MCP server — and a naive restart would break the live MCP session. To solve this, mcp-launcher runs as a **JSON-RPC proxy** between the AI tool and the MCP server:

```
AI Tool ⇄ mcp-launcher (proxy) ⇄ github-mcp-server (restartable child)
```

On launch and on every refresh:

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
Injects into env → starts (or restarts) github-mcp-server
```

**Transparent restart.** When the token is rotated, the child MCP server must be restarted to pick up the new environment variable. The proxy makes this invisible to the AI tool:

- It caches the client's `initialize` request and `notifications/initialized`, and **replays them to the freshly spawned child** so the new server reaches the operational state. The AI tool never re-handshakes and never sees the restart.
- Restarts are **idle-gated**: a restart is only flagged as intent; the actual kill waits until there are no in-flight requests. Requests that arrive during the restart window are **queued and flushed afterwards**, so they are delayed but never lost.
- A **drain timeout** bounds the wait. If in-flight requests do not complete in time, they receive a retryable error and the restart is forced.
- If the refresh fails (e.g. no network), mcp-launcher logs a warning and continues with the existing token.

**What the MCP server sees:** only the short-lived access token (valid max 1 hour).

**What never reaches the MCP server:** App ID, private key, Installation ID. These stay inside mcp-launcher's process and are used solely to obtain the access token.

---

## Security Model

### What is stored where

| Secret | Storage | Visible to MCP server | Visible to Claude (LLM) |
|---|---|---|---|
| GitHub App ID | OS keystore | ❌ No | ❌ No |
| GitHub App private key (RSA) | OS keystore | ❌ No | ❌ No |
| Installation ID | OS keystore | ❌ No | ❌ No |
| Access token (max 1h) | OS keystore → child env | ✅ Yes | ⚠️ Indirectly¹ |

¹ The access token is only reachable by Claude if a (malicious or buggy) MCP server echoes it back inside a tool response. Claude cannot read the child process's environment variables directly.

The App ID, private key, and Installation ID are registered once via the CLI and never leave the keystore. They are never passed to the MCP server process and never appear in any channel that Claude can read.

The access token is injected into the child process environment. A trusted MCP server (such as the official GitHub MCP Server) uses it only to call the GitHub API and never exposes it in tool responses. Claude has no direct way to read environment variables — it can only observe what MCP tool responses return.

### Why short-lived tokens matter

A prompt injection attack — where malicious content in a web page or file tricks Claude into leaking information — can at most reach what Claude can see: MCP tool responses. It cannot reach the OS keystore.

| Credential | Exposure via prompt injection | Impact if leaked | Expiry |
|---|---|---|---|
| Private key | ❌ Not reachable | 🔴 High — can mint tokens indefinitely | Never (revoke manually) |
| Access token | ⚠️ Reachable via MCP tool responses | 🟡 Limited — usable for at most 1 hour | Max 1 hour |

This is why the GitHub App model is preferred over long-lived PATs: even in the worst case where an access token is observed by Claude or leaked via a tool response, it expires within an hour and cannot be used to generate further tokens.

The private key never enters any channel Claude can access, so it is not a prompt injection target. If it were compromised it would require OS-level access — a fundamentally different and much harder attack. Still, treat it as a long-lived credential: store it only in the keystore, never in a file, and revoke it immediately from your GitHub App settings if you suspect compromise.

### What this protects against

- ✅ API keys stored in plain text config files
- ✅ API keys accidentally committed to Git
- ✅ API keys leaking into AI chat logs
- ✅ Key sprawl across multiple tools and config locations
- ✅ Long-lived token exposure — even if the MCP server leaks the token, it expires within 1 hour
- ✅ Private key and App credentials reaching Claude or any MCP server

### What this does NOT protect against

- ❌ An attacker who already has access to your logged-in OS session (they can read the keystore too)
- ❌ Malware with the ability to read process environment variables
- ❌ A malicious MCP server that exfiltrates the short-lived token via tool responses — the token is still real and usable for up to 1 hour
- ❌ You manually pasting a token into a chat window
- ❌ Leakage of the GitHub App private key — if that is compromised, revoke it immediately in your GitHub App settings

**Think of it like a bank vault door on your house.** It won't help if someone is already inside, but it prevents you from leaving your keys on the front porch.

---

## Installation

Pre-built binaries are published on the [Releases](https://github.com/masuda-masuo/mcp-launcher/releases) page. You can also build from source.

### Build mcp-launcher from source

```bash
git clone https://github.com/masuda-masuo/mcp-launcher
cd mcp-launcher
go build -o mcp-launcher ./cmd/launcher
```

### Linux: install a keystore backend

On Linux, mcp-launcher stores secrets via the Secret Service API, which requires `libsecret` (GNOME Keyring) or KWallet to be installed and unlocked:

```bash
# Debian / Ubuntu
sudo apt install libsecret-1-0 gnome-keyring

# Fedora
sudo dnf install libsecret gnome-keyring

# Arch
sudo pacman -S libsecret gnome-keyring
```

> **Note**: A keyring daemon must be running and unlocked for the keystore to be accessible. On headless systems you may need to start and unlock the keyring manually. Windows (Credential Manager) and macOS (Keychain) require no extra packages.

### Set up the GitHub MCP Server

Download the official GitHub MCP Server binary from:
https://github.com/github/github-mcp-server/releases

> **Note**: The npm package `@modelcontextprotocol/server-github` is deprecated as of April 2025. Use the official binary above instead.

> **Note (Windows)**: Claude Desktop does not inherit the system PATH, so you must use the full absolute path to `github-mcp-server.exe` in your `launcher.json`.

---

## Quick Start: Phase 1 (Static PAT)

> A complete, ready-to-edit config is provided in [`launcher.example.json`](launcher.example.json). Copy it to `launcher.json` and adjust the paths and keys. Keep `launcher.json` out of version control (it is already in `.gitignore`).

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

Phase 2 replaces the long-lived Personal Access Token with short-lived installation access tokens (max 1 hour) issued by a GitHub App. The token is automatically refreshed when it approaches expiry, and the MCP server is transparently restarted to pick it up (see [How It Works](#phase-2-short-lived-token-via-github-app)).

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
    },
    "check_interval_seconds": 60,
    "drain_timeout_seconds": 30
  }
}
```

### How token refresh works

- Token is fetched from GitHub App API and stored in the keystore on first launch
- `check_interval_seconds` controls how often mcp-launcher checks the token expiry **in the background while running**
- If the token is still valid, nothing happens (no API call, no restart)
- If expiring soon (within `refresh_before_seconds`), a new token is fetched and the child process is restarted transparently
- The restart is idle-gated: mcp-launcher waits for any in-flight requests to complete before restarting
- If the refresh fails (e.g. no network), mcp-launcher logs a warning and continues with the existing token

> **If you omit `check_interval_seconds`**: mcp-launcher only refreshes the token **at launch**, with no background polling. This is fine for short-lived sessions, but if the AI tool keeps the connection open longer than the token's lifetime (1 hour), the token can expire mid-session and calls will start failing until the next launch. For long-running clients like Claude Desktop, set `check_interval_seconds` (e.g. `60`) so the token is refreshed in the background before it expires.

> **Note**: The access token may also be expired if you haven't used Claude Desktop for more than 1 hour. It will be refreshed automatically on the next launch — no manual action required.

### Current limitations

- **GitHub only**: Phase 2 token rotation is implemented for GitHub App only. Other services (AWS, Azure, GCP) use static secrets via Phase 1 for now.
- **Private key security**: The GitHub App private key is stored in the OS keystore, which is more secure than a file, but it is a long-lived credential. If compromised, revoke it immediately from your GitHub App settings.

---

## Configuration Reference

### Service config (`launcher.json`)

| Field | Required | Type | Description |
|---|---|---|---|
| `command` | ✅ | string | Full path to the MCP server executable |
| `args` | - | string[] | Command-line arguments passed to the MCP server |
| `env_keys` | ✅ | object | Map of `ENV_VAR_NAME` → keystore key. Each entry is fetched from the OS keystore and injected into the child process environment |
| `token_source` | - | object | GitHub App token configuration. When set, mcp-launcher fetches and refreshes short-lived tokens automatically |
| `check_interval_seconds` | - | int | How often (in seconds) to check whether the token needs refreshing. A restart only occurs when the token is actually near expiry — this is **not** "restart every N seconds". Omit to disable background checking (token is only refreshed at launch) |
| `drain_timeout_seconds` | - | int | Maximum time (in seconds) to wait for in-flight requests to complete before forcing a restart. Zero or omitted means wait indefinitely — a request that never returns (e.g. a hung or unresponsive server) would block the restart indefinitely. A finite value (e.g. `30`–`60`) is recommended so a stuck request cannot prevent token rotation; abandoned requests receive a retryable error |

### `token_source` fields

| Field | Required | Description |
|---|---|---|
| `type` | ✅ | Token source type. Currently only `"github_app"` is supported |
| `app_id_key` | ✅ | Keystore key where the GitHub App ID is stored |
| `private_key_key` | ✅ | Keystore key where the GitHub App RSA private key (PEM) is stored |
| `installation_id_key` | ✅ | Keystore key where the GitHub App Installation ID is stored |
| `target_env_key` | ✅ | The `env_keys` entry that will receive the generated access token (e.g. `"GITHUB_PERSONAL_ACCESS_TOKEN"`) |
| `refresh_before_seconds` | ✅ | Refresh the token when its remaining lifetime falls below this threshold (in seconds). Recommended: `600` (10 minutes) |

> **Security note**: `app_id_key`, `private_key_key`, and `installation_id_key` are keystore key names, not the secrets themselves. The actual values are registered via `mcp-launcher register` and never appear in `launcher.json`. They are never passed to the MCP server process and are not accessible to Claude.

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
