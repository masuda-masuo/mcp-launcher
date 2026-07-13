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
mcp-token register my-aws-mcp ROLE_ARN arn:aws:iam::123456789012:role/MyMCPRole
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
      "role_arn_key": "mcp-token/my-aws-mcp/ROLE_ARN",
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
mcp-token register github APP_ID 123456
mcp-token register github PRIVATE_KEY "$(cat private-key.pem)"
mcp-token register github INSTALLATION_ID 78901234
```

**2. Create `launcher.json`**

```json
{
  "github": {
    "command": "C:\\path\\to\\github-mcp-server.exe",
    "args": ["stdio"],
    "env_keys": {
      "GITHUB_PERSONAL_ACCESS_TOKEN": "mcp-token/github/github-app"
    },
    "token_source": {
      "type": "github_app",
      "app_id_key": "mcp-token/github/APP_ID",
      "private_key_key": "mcp-token/github/PRIVATE_KEY",
      "installation_id_key": "mcp-token/github/INSTALLATION_ID",
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
- [On-Demand Mint Socket](docs/architecture/mint-socket.md) — systemd socket-activated `mcp-token`, for consumers (sandboxes, `streamable-http` daemons) that can't shell out to the CLI themselves

---

## License

MIT

## `mcp-token` — on-demand token broker & keystore CLI

`mcp-token` is a standalone CLI for managing keystore secrets and minting
short-lived tokens. It shares the same `launcher.json` config and `token_source`
logic as the launcher, so no extra setup is needed beyond registration.

It exists for clients that run **outside** the launcher stdio proxy — most
notably an MCP server running as a long-lived `streamable-http` daemon, where the
launcher is no longer in the path to inject `GITHUB_TOKEN`. Such a client can
fetch a fresh token on demand without ever holding the GitHub App private key
(which stays in the keystore):

```
$ mcp-token github
ghs_xxxxxxxxxxxxxxxxxxxx

# e.g. as a token command for a downstream daemon (issue #25):
GITHUB_TOKEN_COMMAND="mcp-token github"
```

### CLI Commands

| Command | Description |
|---|---|
| `mcp-token <service>` | Mint a fresh short-lived token for `<service>` and print to stdout. Only `github_app` token sources supported today. |
| `mcp-token register <service> <KEY> <value>` | Store a secret under `mcp-token/<service>/<KEY>` in the OS keystore. |
| `mcp-token list [<service>]` | List all registered keystore keys, optionally filtered by service. Searches both `mcp-token/` and `mcp-launcher/` prefixes (deduplicated) for backward compatibility. |
| `mcp-token delete <service> <KEY>` | Delete a single key. Falls back to `mcp-launcher/` prefix if not found under `mcp-token/`. |
| `mcp-token delete --all <service>` | Delete all keys for a service (with confirmation prompt). Use `--force` to skip confirmation. |
| `mcp-token convert [<service>]` | Migrate keys from `mcp-launcher/` to `mcp-token/` prefix. With `--force`, skip confirmation. Converts all services, or a specific service. |
| `mcp-token version` | Print the mcp-token version. |

### On-demand mint socket (systemd, Linux)

For consumers that can't shell out to `mcp-token` directly (a sandboxed
container, a long-lived `streamable-http` daemon), `mcp-token github` can be
wired up behind a systemd-activated Unix socket instead: connecting mints a
fresh token on the spot, with no daemon and no wall-clock refresh timer.

```bash
scripts/install-mint-socket.sh
```

installs and enables it in systemd user scope.

On a machine with no checkout and no Go toolchain (a server, a VM), take the
**mint-socket kit** published with every `mcp-token` release instead — it is
the same installer plus the two unit files, and it downloads the pinned
`mcp-token` binary from the same release, verifying it against `checksums.txt`:

```bash
VER=v1.3.0
BASE="https://github.com/masuda-masuo/mcp-launcher/releases/download/mcp-token%2F$VER"
curl -fsSLO "$BASE/mcp-token-mint-socket-$VER.tar.gz"
curl -fsSLO "$BASE/checksums.txt"
sha256sum --check --ignore-missing checksums.txt
tar xzf "mcp-token-mint-socket-$VER.tar.gz"
scripts/install-mint-socket.sh --config /path/to/launcher.json
```

See [On-Demand Mint Socket](docs/architecture/mint-socket.md) for the full
contract, the security boundary, and consumer footguns worth reading before
bind-mounting the socket into anything.

### Environment

| Variable | Default | Description |
|---|---|---|
| `MCP_TOKEN_FETCH_TIMEOUT` | `30s` | GitHub API timeout as a Go duration (e.g. `45s`). |

### Release tags

`mcp-token` is released independently of the launcher under `mcp-token/vX.Y.Z`
tag namespace, while the launcher keeps the bare `vX.Y.Z` namespace. Both
binaries are built in the same release workflow.

### Migration from `mcp-launcher/` keystore keys (v0.2.x)

If you used `mcp-launcher register` from v0.2.x, your keystore keys use the
`mcp-launcher/` prefix. To migrate:

1. **Automated** (recommended):
   ```bash
   mcp-token convert          # all services, with confirmation
   mcp-token convert --force  # skip confirmation
   ```
2. **Manual** (per-service):
   ```bash
   mcp-token convert github   # convert only the "github" service
   ```
3. **Update `launcher.json`** — change all keystore key references from
   `mcp-launcher/...` to `mcp-token/...` (e.g. `"mcp-launcher/github/APP_ID"`
   → `"mcp-token/github/APP_ID"`).

The old `mcp-launcher register` command still works with a deprecation warning
but writes keys under the new `mcp-token/` prefix.
