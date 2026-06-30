# Configuration Reference

## Service config (`launcher.json`)

| Field | Required | Type | Description |
|---|---|---|---|
| `command` | ✅ | string | Full path to the MCP server executable |
| `args` | - | string[] | Command-line arguments passed to the MCP server |
| `env_keys` | ✅ | object | Map of `ENV_VAR_NAME` → keystore key. Each entry is fetched from the OS keystore and injected into the child process environment. When using `token_source`, the `target_env_key` value must appear as a key on the left side (see note below) |
| `token_source` | - | object | Token source configuration. When set, mcp-launcher fetches and refreshes short-lived tokens automatically |
| `check_interval_seconds` | - | int | How often (in seconds) to check whether the token needs refreshing. A restart only occurs when the token is actually near expiry. Omit to disable background checking (token is only refreshed at launch) |
| `drain_timeout_seconds` | - | int | Maximum time (in seconds) to wait for in-flight requests to complete before forcing a restart. Zero or omitted means wait indefinitely. A finite value (e.g. `30`–`60`) is recommended |

> **`env_keys` and `target_env_key`**: When `token_source` is set, the value of `target_env_key` (e.g. `"AWS"`) must appear as a key on the left side of `env_keys`. This is required for validation. The right side values are the flat keystore key names written by the fetcher — not namespaced paths.
>
> Example for `target_env_key: "AWS"`:
> ```json
> "env_keys": {
>   "AWS":                   "AWS_ACCESS_KEY_ID",
>   "AWS_ACCESS_KEY_ID":     "AWS_ACCESS_KEY_ID",
>   "AWS_SECRET_ACCESS_KEY": "AWS_SECRET_ACCESS_KEY",
>   "AWS_SESSION_TOKEN":     "AWS_SESSION_TOKEN"
> }
> ```

---

## `token_source` fields

### Common fields

| Field | Required | Description |
|---|---|---|
| `type` | ✅ | Token source type: `"github_app"` or `"aws_sts"` |
| `target_env_key` | ✅ | For `github_app`: the `env_keys` entry that receives the access token. For `aws_sts`: the prefix used to derive the three keystore keys written by the fetcher (`<prefix>_ACCESS_KEY_ID`, `<prefix>_SECRET_ACCESS_KEY`, `<prefix>_SESSION_TOKEN`) |
| `refresh_before_seconds` | ✅ | Refresh the token when its remaining lifetime falls below this threshold (in seconds). Recommended: `600` (10 minutes) |

### `type: "github_app"` fields

| Field | Required | Description |
|---|---|---|
| `app_id_key` | ✅ | Keystore key where the GitHub App ID is stored |
| `private_key_key` | ✅ | Keystore key where the GitHub App RSA private key (PEM) is stored |
| `installation_id_key` | ✅ | Keystore key where the GitHub App Installation ID is stored |

> **Security note**: These are keystore key names, not the secrets themselves. The actual values are registered via `mcp-token register` and never appear in `launcher.json`.

### `type: "aws_sts"` fields

| Field | Required | Description |
|---|---|---|
| `role_arn_key` | ✅ | Keystore key where the IAM Role ARN is stored |
| `role_session_name` | ✅ | Session name passed to `AssumeRole` (appears in CloudTrail logs). Use a unique name per service for traceability |
| `duration_seconds` | - | Lifetime of the STS credentials in seconds (default: `3600`, minimum: `900`) |

> **Note**: The base AWS credentials used to call `AssumeRole` are loaded from the standard AWS credential chain — they are **not** registered in the launcher keystore.

---

## Supported Platforms

| Platform | Keystore Backend | Notes |
|---|---|---|
| Windows | Credential Manager (DPAPI) | Recommended |
| macOS | Keychain | Recommended |
| Linux | libsecret / kwallet | Requires `libsecret-1-0` and `gnome-keyring` |
| WSL | ⚠️ Not supported | Use Windows host directly. See [wsl-setup.md](../setup/wsl-setup.md) |
