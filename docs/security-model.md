# Security Model

## What is stored where

| Secret | Storage | Visible to MCP server | Visible to Claude (LLM) |
|---|---|---|---|
| GitHub App ID | OS keystore | ❌ No | ❌ No |
| GitHub App private key (RSA) | OS keystore | ❌ No | ❌ No |
| Installation ID | OS keystore | ❌ No | ❌ No |
| Access token (max 1h) | OS keystore → child env | ✅ Yes | ⚠️ Indirectly¹ |
| AWS IAM Role ARN | OS keystore | ❌ No | ❌ No |
| AWS_ACCESS_KEY_ID (STS, max 1h) | OS keystore → child env | ✅ Yes | ⚠️ Indirectly¹ |
| AWS_SECRET_ACCESS_KEY (STS, max 1h) | OS keystore → child env | ✅ Yes | ⚠️ Indirectly¹ |
| AWS_SESSION_TOKEN (STS, max 1h) | OS keystore → child env | ✅ Yes | ⚠️ Indirectly¹ |

¹ The access token is only reachable by Claude if a (malicious or buggy) MCP server echoes it back inside a tool response. Claude cannot read the child process's environment variables directly.

The App ID, private key, and Installation ID are registered once via the CLI and never leave the keystore. They are never passed to the MCP server process and never appear in any channel that Claude can read.

## Why short-lived tokens matter

A prompt injection attack can at most reach what Claude can see: MCP tool responses. It cannot reach the OS keystore.

| Credential | Exposure via prompt injection | Impact if leaked | Expiry |
|---|---|---|---|
| Private key | ❌ Not reachable | 🔴 High — can mint tokens indefinitely | Never (revoke manually) |
| Access token | ⚠️ Reachable via MCP tool responses | 🟡 Limited — usable for at most 1 hour | Max 1 hour |

This is why the GitHub App model is preferred over long-lived PATs: even if an access token is observed by Claude or leaked via a tool response, it expires within an hour and cannot be used to generate further tokens. The same reasoning applies to AWS STS credentials.

The private key never enters any channel Claude can access. If it were compromised it would require OS-level access. Still, treat it as a long-lived credential: store it only in the keystore, never in a file, and revoke it immediately from your GitHub App settings if you suspect compromise.

## What this protects against

- ✅ API keys stored in plain text config files
- ✅ API keys accidentally committed to Git
- ✅ API keys leaking into AI chat logs
- ✅ Key sprawl across multiple tools and config locations
- ✅ Long-lived token exposure — even if the MCP server leaks the token, it expires within 1 hour
- ✅ Private key and App credentials reaching Claude or any MCP server
- ✅ Long-lived AWS IAM user keys — replaced with short-lived STS credentials

## What this does NOT protect against

- ❌ An attacker who already has access to your logged-in OS session (they can read the keystore too)
- ❌ Malware with the ability to read process environment variables
- ❌ A malicious MCP server that exfiltrates the short-lived token via tool responses — the token is still real and usable for up to 1 hour
- ❌ You manually pasting a token into a chat window
- ❌ Leakage of the GitHub App private key — if that is compromised, revoke it immediately in your GitHub App settings

**Think of it like a bank vault door on your house.** It won't help if someone is already inside, but it prevents you from leaving your keys on the front porch.

## Security Notes for Contributors

- Never commit real tokens or credentials, even in tests
- Keep `launcher.json` in `.gitignore` — only `launcher.example.json` belongs in the repo
- The GitHub App private key must never be stored as a file; use the keystore
- Please report security issues via GitHub Security Advisories, not public Issues
