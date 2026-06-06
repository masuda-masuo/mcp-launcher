# Adding a New Token Source

This document explains how to add a new automatic token source to mcp-launcher —
using AWS STS as the primary example — and how the existing GitHub App source
is structured so you can follow the same pattern for any future service.

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
| `internal/refresher/refresher.go` | Generic refresh engine (service-agnostic) |
| `cmd/launcher/main.go` | Wires `type` string → concrete fetcher |

---

## The `TokenFetcher` interface

Defined in `internal/githubapp/fetcher.go`:

```go
type TokenFetcher interface {
    FetchToken(ctx context.Context) (token string, expiry time.Time, err error)
}
```

Any new token source only needs to satisfy this interface.

> **Note — multiple output credentials**: GitHub App returns one token that maps
> to one environment variable. AWS STS returns three values
> (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN`).
> See [Multi-credential sources](#multi-credential-sources) below for the
> recommended way to handle this without changing the interface.

---

## Step-by-step: adding AWS STS

### 1. Add config fields

In `internal/config/config.go`, add AWS-specific fields to `TokenSource`.
All new fields must be `omitempty` so existing configs are unaffected.

```go
type TokenSource struct {
    Type                 string `json:"type"`

    // GitHub App (existing)
    AppIDKey             string `json:"app_id_key,omitempty"`
    PrivateKeyKey        string `json:"private_key_key,omitempty"`
    InstallationIDKey    string `json:"installation_id_key,omitempty"`
    TargetEnvKey         string `json:"target_env_key,omitempty"`

    // AWS STS (new)
    RoleARNKey           string `json:"role_arn_key,omitempty"`
    RoleSessionName      string `json:"role_session_name,omitempty"`
    DurationSeconds      int    `json:"duration_seconds,omitempty"`
    // TargetEnvKey reused as the keystore prefix for the three credential values.

    RefreshBeforeSeconds int    `json:"refresh_before_seconds"`
}
```

### 2. Create the fetcher package

Create `internal/awssts/fetcher.go`:

```go
package awssts

import (
    "context"
    "fmt"
    "time"

    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/credentials"
    "github.com/aws/aws-sdk-go-v2/service/sts"
    mcpconfig "github.com/masuda-masuo/mcp-launcher/internal/config"
    "github.com/masuda-masuo/mcp-launcher/internal/keystore"
)

// Fetcher implements githubapp.TokenFetcher for AWS STS AssumeRole.
// Because STS returns three credential values instead of one, the token
// returned here is a sentinel — the real values are written directly to the
// keystore inside FetchToken and read back by buildEnv in main.go.
// See docs/adding-a-token-source.md § Multi-credential sources.
type Fetcher struct {
    store  keystore.Store
    source mcpconfig.TokenSource
}

func NewFetcher(store keystore.Store, source mcpconfig.TokenSource) *Fetcher {
    return &Fetcher{store: store, source: source}
}

func (f *Fetcher) FetchToken(ctx context.Context) (token string, expiry time.Time, err error) {
    roleARN, err := f.store.Get(f.source.RoleARNKey)
    if err != nil {
        return "", time.Time{}, fmt.Errorf("getting role ARN: %w", err)
    }

    duration := f.source.DurationSeconds
    if duration == 0 {
        duration = 3600 // default 1 hour
    }

    cfg, err := config.LoadDefaultConfig(ctx)
    if err != nil {
        return "", time.Time{}, fmt.Errorf("loading AWS config: %w", err)
    }

    client := sts.NewFromConfig(cfg)
    sessionName := f.source.RoleSessionName
    if sessionName == "" {
        sessionName = "mcp-launcher"
    }

    out, err := client.AssumeRole(ctx, &sts.AssumeRoleInput{
        RoleArn:         aws.String(roleARN),
        RoleSessionName: aws.String(sessionName),
        DurationSeconds: aws.Int32(int32(duration)),
    })
    if err != nil {
        return "", time.Time{}, fmt.Errorf("AssumeRole: %w", err)
    }

    creds := out.Credentials
    prefix := f.source.TargetEnvKey

    // Write the three credential parts to the keystore so buildEnv can read them.
    for key, val := range map[string]string{
        prefix + "_KEY_ID":     aws.ToString(creds.AccessKeyId),
        prefix + "_SECRET":     aws.ToString(creds.SecretAccessKey),
        prefix + "_SESSION":    aws.ToString(creds.SessionToken),
    } {
        if err := f.store.Set(key, val); err != nil {
            return "", time.Time{}, fmt.Errorf("storing %s: %w", key, err)
        }
    }

    return aws.ToString(creds.AccessKeyId), aws.ToTime(creds.Expiration), nil
}
```

### 3. Wire the new type in main.go

In `cmd/launcher/main.go`, find the place where `refresher.New` is called and
add a factory that selects the right fetcher by `type`:

```go
// internal/refresher/refresher.go — extend New() to accept a fetcher
func NewWithFetcher(store keystore.Store, source config.TokenSource, tokenKey string, fetcher TokenFetcher) *Refresher {
    return &Refresher{store: store, fetcher: fetcher, source: source, tokenKey: tokenKey}
}
```

```go
// cmd/launcher/main.go — helper
func newFetcher(store keystore.Store, source config.TokenSource) (githubapp.TokenFetcher, error) {
    switch source.Type {
    case "github_app":
        return githubapp.NewFetcher(store, source), nil
    case "aws_sts":
        return awssts.NewFetcher(store, source), nil
    default:
        return nil, fmt.Errorf("unknown token_source type %q", source.Type)
    }
}
```

### 4. Extend `buildEnv` for multi-value credential sources

AWS STS writes three keystore keys per credential set. `buildEnv` in `main.go`
already reads `env_keys` from the config, so the simplest approach is to have
the user map each credential part explicitly:

```json
"env_keys": {
    "AWS_ACCESS_KEY_ID":     "mcp-launcher/aws/TOKEN_KEY_ID",
    "AWS_SECRET_ACCESS_KEY": "mcp-launcher/aws/TOKEN_SECRET",
    "AWS_SESSION_TOKEN":     "mcp-launcher/aws/TOKEN_SESSION"
}
```

No change to `buildEnv` is required — it already iterates `env_keys`.

### 5. Register long-lived credentials

```bash
# The IAM role ARN that mcp-launcher will assume
mcp-launcher register aws ROLE_ARN arn:aws:iam::123456789012:role/my-mcp-role
```

The base AWS credentials (used to call `AssumeRole`) are picked up from the
standard AWS credential chain — environment variables, `~/.aws/credentials`, or
an instance profile — so they do not need to be registered with mcp-launcher.

### 6. `launcher.json` example

```json
{
  "aws": {
    "command": "/usr/local/bin/aws-mcp-server",
    "args": [],
    "env_keys": {
      "AWS_ACCESS_KEY_ID":     "mcp-launcher/aws/TOKEN_KEY_ID",
      "AWS_SECRET_ACCESS_KEY": "mcp-launcher/aws/TOKEN_SECRET",
      "AWS_SESSION_TOKEN":     "mcp-launcher/aws/TOKEN_SESSION"
    },
    "token_source": {
      "type": "aws_sts",
      "role_arn_key": "mcp-launcher/aws/ROLE_ARN",
      "role_session_name": "mcp-launcher",
      "duration_seconds": 3600,
      "target_env_key": "mcp-launcher/aws/TOKEN",
      "refresh_before_seconds": 300
    },
    "check_interval_seconds": 60,
    "drain_timeout_seconds": 30
  }
}
```

---

## Multi-credential sources

GitHub App → 1 token → 1 environment variable. Simple.

Some services return a **bundle** of values that must all be injected as separate
environment variables (AWS STS, GCP service accounts, etc.). The recommended
pattern is:

1. **The fetcher writes each value to the keystore** under a predictable key
   derived from `target_env_key` (used as a prefix), e.g.
   `TOKEN_KEY_ID`, `TOKEN_SECRET`, `TOKEN_SESSION`.
2. **The user maps each key in `env_keys`** so `buildEnv` picks them up without
   any special-casing.
3. **`FetchToken` returns** the primary credential (e.g. `AccessKeyId`) as the
   token value and the expiry — the refresher uses these to manage the refresh
   cycle as usual.

This keeps the `TokenFetcher` interface unchanged and the refresher fully generic.

---

## Checklist for adding any new token source

- [ ] Add `omitempty` fields to `TokenSource` in `internal/config/config.go`
- [ ] Create `internal/<provider>/fetcher.go` implementing `TokenFetcher`
- [ ] Add the `type` string to the factory switch in `cmd/launcher/main.go`
- [ ] If the source returns multiple credentials, follow the keystore-prefix pattern above
- [ ] Add a `launcher.example.json` snippet to `docs/` or update the existing example file
- [ ] Add unit tests in `internal/<provider>/fetcher_test.go` (mock the HTTP/SDK layer)
- [ ] Update `README.md` — Current Limitations section — once the new type is released

---

## Candidate token sources for future phases

| Service | Mechanism | Output credentials | Notes |
|---|---|---|---|
| AWS STS | `AssumeRole` via `aws-sdk-go-v2` | 3 (key, secret, session) | Described above |
| GCP | Service Account → OAuth2 token via `google.golang.org/api` | 1 (Bearer token) | `GOOGLE_OAUTH_ACCESS_TOKEN` |
| Azure | Service Principal → Bearer token via `azure-sdk-for-go` | 1 (Bearer token) | `AZURE_ACCESS_TOKEN` |
| HashiCorp Vault | AppRole → Secret Lease via Vault SDK | 1+ (depends on secret engine) | Lease renewal instead of restart |
| Atlassian | OAuth2 Refresh Token → Access Token | 1 (Bearer token) | Refresh token itself is long-lived |
| Snowflake | Key-pair JWT → OAuth token | 1 (Bearer token) | Similar to GitHub App JWT flow |

All of these fit the same pattern: one fetcher package, one `type` string, one
entry in the factory switch.
