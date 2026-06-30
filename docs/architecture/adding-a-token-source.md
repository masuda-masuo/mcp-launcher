# Adding a New Token Source

This document explains how to add a new automatic token source to mcp-launcher,
following the same pattern used by the existing GitHub App and AWS STS implementations.

---

## How the existing system works

The token rotation pipeline has three layers:

```
config.TokenSource          (what the user configures in launcher.json)
        ↓
TokenFetcher interface       (internal contract: fetch a token, return expiry)
        ↓
refresher.Refresher          (engine: checks expiry, calls fetcher, writes keystore)
        ↓
mcpproxy / main.go           (restarts child when keystore token changes)
```

A `TokenSource` describes **which long-lived credentials to use** and **where to
put the result**. The fetcher knows **how to exchange** those credentials for a
short-lived token. The refresher and proxy know nothing about any specific
service — they only speak `TokenFetcher`.

### Key files

| Path | Role |
|---|---|
| `internal/config/config.go` | `TokenSource` struct — user-facing config fields |
| `internal/githubapp/fetcher.go` | GitHub App implementation of `TokenFetcher` |
| `internal/awssts/fetcher.go` | AWS STS implementation of `TokenFetcher` |
| `internal/refresher/refresher.go` | Generic refresh engine (service-agnostic) |
| `cmd/launcher/main.go` | Wires `type` string → concrete fetcher |

---

## The `TokenFetcher` interface

```go
type TokenFetcher interface {
    FetchToken(ctx context.Context) (token string, expiry time.Time, err error)
}
```

Any new token source only needs to satisfy this interface.

---

## How multi-credential sources work (AWS STS)

GitHub App returns one token → one environment variable. Simple.

AWS STS returns three values (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN`).
The pattern used:

1. **The fetcher writes each value to the keystore** under a key derived from `target_env_key` as a prefix:
   - `{target_env_key}_ACCESS_KEY_ID`
   - `{target_env_key}_SECRET_ACCESS_KEY`
   - `{target_env_key}_SESSION_TOKEN`

   For example, `target_env_key: "AWS"` → writes `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN`.

2. **The user maps each key in `env_keys`** so `buildEnv` picks them up.
   The left side must include `target_env_key` itself (required by validation), and the right side uses the flat keystore key names written by the fetcher:

   ```json
   "env_keys": {
     "AWS":                   "AWS_ACCESS_KEY_ID",
     "AWS_ACCESS_KEY_ID":     "AWS_ACCESS_KEY_ID",
     "AWS_SECRET_ACCESS_KEY": "AWS_SECRET_ACCESS_KEY",
     "AWS_SESSION_TOKEN":     "AWS_SESSION_TOKEN"
   }
   ```

3. **`FetchToken` returns** the `AccessKeyId` as the token value and the expiry.
   The refresher uses these to manage the refresh cycle as usual.

This keeps the `TokenFetcher` interface unchanged and the refresher fully generic.

---

## Step-by-step: adding a new token source

### 1. Add config fields

In `internal/config/config.go`, add provider-specific fields to `TokenSource`.
All new fields must be `omitempty` so existing configs are unaffected.

```go
type TokenSource struct {
    Type                 string `json:"type"`

    // GitHub App (existing)
    AppIDKey             string `json:"app_id_key,omitempty"`
    PrivateKeyKey        string `json:"private_key_key,omitempty"`
    InstallationIDKey    string `json:"installation_id_key,omitempty"`
    TargetEnvKey         string `json:"target_env_key,omitempty"`

    // AWS STS (existing)
    RoleARNKey           string `json:"role_arn_key,omitempty"`
    RoleSessionName      string `json:"role_session_name,omitempty"`
    DurationSeconds      int    `json:"duration_seconds,omitempty"`

    // YourProvider (new)
    YourProviderKey      string `json:"your_provider_key,omitempty"`

    RefreshBeforeSeconds int    `json:"refresh_before_seconds"`
}
```

### 2. Create the fetcher package

Create `internal/<provider>/fetcher.go` implementing `TokenFetcher`:

```go
package yourprovider

import (
    "context"
    "fmt"
    "time"

    "github.com/masuda-masuo/mcp-launcher/internal/keystore"
)

type Fetcher struct {
    store  keystore.Store
    // provider-specific fields
}

func NewFetcher(store keystore.Store, /* config fields */) *Fetcher {
    return &Fetcher{store: store}
}

func (f *Fetcher) FetchToken(ctx context.Context) (token string, expiry time.Time, err error) {
    // 1. Read long-lived credentials from keystore
    // 2. Exchange for short-lived token via provider SDK/API
    // 3. If multiple output credentials: write each to keystore
    // 4. Return primary token value and expiry
    return token, expiry, nil
}
```

### 3. Wire the new type in main.go

In `cmd/launcher/main.go`, add the new type to the fetcher factory switch:

```go
func newFetcher(ctx context.Context, store keystore.Store, svc config.Service) (TokenFetcher, error) {
    switch svc.TokenSource.Type {
    case "github_app":
        return githubapp.NewFetcher(ctx, store, ...)
    case "aws_sts":
        return awssts.NewFetcher(ctx, store, ...)
    case "your_provider":
        return yourprovider.NewFetcher(store, ...)
    default:
        return nil, fmt.Errorf("unknown token_source type %q", svc.TokenSource.Type)
    }
}
```

### 4. `launcher.json` example

```json
{
  "my-service": {
    "command": "C:\\path\\to\\mcp-server.exe",
    "args": [],
    "env_keys": {
      "YOUR_TOKEN": "YOUR_TOKEN"
    },
    "token_source": {
      "type": "your_provider",
      "your_provider_key": "mcp-token/my-service/CREDENTIALS",
      "target_env_key": "YOUR_TOKEN",
      "refresh_before_seconds": 600
    },
    "check_interval_seconds": 60,
    "drain_timeout_seconds": 30
  }
}
```

---

## Checklist for adding any new token source

- [ ] Add `omitempty` fields to `TokenSource` in `internal/config/config.go`
- [ ] Create `internal/<provider>/fetcher.go` implementing `TokenFetcher`
- [ ] Add the `type` string to the factory switch in `cmd/launcher/main.go`
- [ ] If the source returns multiple credentials, follow the keystore-prefix pattern (see above)
- [ ] Add unit tests in `internal/<provider>/fetcher_test.go` (mock the HTTP/SDK layer)
- [ ] Update `README.md` and relevant docs once the new type is released

---

## Candidate token sources for future phases

| Service | Mechanism | Output credentials | Notes |
|---|---|---|---|
| GCP | Service Account → OAuth2 token via `google.golang.org/api` | 1 (Bearer token) | `GOOGLE_OAUTH_ACCESS_TOKEN` |
| Azure | Service Principal → Bearer token via `azure-sdk-for-go` | 1 (Bearer token) | `AZURE_ACCESS_TOKEN` |
| HashiCorp Vault | AppRole → Secret Lease via Vault SDK | 1+ (depends on secret engine) | Lease renewal instead of restart |
| Atlassian | OAuth2 Refresh Token → Access Token | 1 (Bearer token) | Refresh token itself is long-lived |
| Snowflake | Key-pair JWT → OAuth token | 1 (Bearer token) | Similar to GitHub App JWT flow |
