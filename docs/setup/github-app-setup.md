# Phase 2: Short-lived Tokens via GitHub App

This replaces the long-lived Personal Access Token with short-lived installation access tokens (max 1 hour) issued by a GitHub App. The token is automatically refreshed when it approaches expiry, and the MCP server is transparently restarted to pick it up.

## How it works

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

**Transparent restart.** The proxy caches the client's `initialize` request and replays it to the freshly spawned child, so the AI tool never re-handshakes and never sees the restart. Restarts are idle-gated and queue in-flight requests until the new server is ready. A drain timeout bounds the wait.

**What the MCP server sees:** only the short-lived access token (valid max 1 hour).

**What never reaches the MCP server:** App ID, private key, Installation ID.

## Step 1: Create a GitHub App

1. Go to https://github.com/settings/apps/new
2. Fill in:
   - **App name**: any name (e.g. `mcp-launcher-yourname`)
   - **Homepage URL**: any URL
   - **Webhook**: disable
   - **Permissions**: grant only what you need (e.g. `Contents: Read & Write`, `Issues: Read & Write`)
3. Click **Create GitHub App** — note the **App ID** shown at the top of the settings page

## Step 2: Generate a private key

On the App settings page, scroll to **Private keys** → **Generate a private key**. A `.pem` file will be downloaded.

## Step 3: Install the App

On the App settings page, go to **Install App** → select your account or organization → choose **All repositories** or specific repositories. The URL after installation will be:

```
https://github.com/settings/installations/1234567
```

The number at the end is your **Installation ID**.

> **Multiple organizations**: Each organization requires a separate installation. Create one `launcher.json` entry per organization with its own `installation_id_key`.

## Step 4: Register secrets in the keystore

```bash
mcp-launcher register github APP_ID 123456
mcp-launcher register github PRIVATE_KEY "$(cat path/to/private-key.pem)"
mcp-launcher register github INSTALLATION_ID 7654321
```

## Step 5: Update `launcher.json`

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

## Token refresh behavior

- Token is fetched from GitHub App API and stored in the keystore on first launch
- `check_interval_seconds` controls how often mcp-launcher checks token expiry in the background
- If the token is still valid, nothing happens (no API call, no restart)
- If expiring soon (within `refresh_before_seconds`), a new token is fetched and the child process is restarted transparently
- If the refresh fails (e.g. no network), mcp-launcher logs a warning and continues with the existing token

> **If you omit `check_interval_seconds`**: mcp-launcher only refreshes the token at launch, with no background polling. For long-running clients like Claude Desktop, set `check_interval_seconds` (e.g. `60`) to avoid mid-session token expiry.