# Phase 2: Short-lived AWS Credentials via STS

Instead of storing long-lived IAM user access keys, mcp-launcher can assume an IAM Role via AWS STS and automatically rotate the resulting short-lived credentials (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN`).

The base credentials used to call `AssumeRole` are loaded from the **standard AWS credential chain** (environment variables, `~/.aws/credentials`, EC2/ECS instance profile, etc.) — they are never registered in the launcher keystore.

---

## Step 1: Create an IAM Role

Create a Role that your base credentials can assume. The trust policy should allow your IAM user (or the account root) to call `AssumeRole`:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "AWS": "arn:aws:iam::123456789012:root"
      },
      "Action": "sts:AssumeRole"
    }
  ]
}
```

Attach only the permissions the MCP server actually needs (e.g. read-only Lambda, CloudWatch Logs, S3). Avoid `AdministratorAccess`.

---

## Step 2: Register the IAM Role ARN in the keystore

```bash
mcp-launcher register my-aws-mcp ROLE_ARN arn:aws:iam::123456789012:role/MyMCPRole
```

Only the Role ARN is registered. The base credentials (`~/.aws/credentials`) are never stored in the keystore.

---

## Step 3: Update `launcher.json`

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

**Key points:**

- `env_keys` — the key on the left must include `target_env_key` itself (e.g. `"AWS"`). The values on the right are the flat keystore key names that the STS fetcher writes to (e.g. `"AWS_ACCESS_KEY_ID"`, not `"mcp-launcher/my-aws-mcp/AWS_ACCESS_KEY_ID"`)
- `role_arn_key` — keystore key where the IAM Role ARN is stored (registered in Step 2)
- `role_session_name` — appears in CloudTrail logs; use a unique name per service
- `duration_seconds` — lifetime of the STS credentials (default: 3600; min: 900)
- `target_env_key` — prefix used by the fetcher when writing credentials to the keystore (`AWS` → writes `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN`)

**Multiple services sharing the same Role:**

If you run multiple AWS MCP servers, each service should use its own `role_arn_key` (registered separately) and a unique `role_session_name`. They can share the same flat keystore keys (`AWS_ACCESS_KEY_ID` etc.) as long as they target the same Role.

---

## Step 4: Configure Claude Desktop

```json
{
  "mcpServers": {
    "my-aws-mcp": {
      "command": "C:\\path\\to\\mcp-launcher.exe",
      "args": ["my-aws-mcp"],
      "env": {
        "AWS_REGION": "us-east-1"
      }
    }
  }
}
```

> `launcher.json` is automatically loaded from the same directory as `mcp-launcher.exe`. No `MCP_LAUNCHER_CONFIG` needed.

---

## How credential rotation works

1. On first launch, mcp-launcher calls `AssumeRole` and stores all three credentials in the keystore under the flat key names (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN`)
2. Every `check_interval_seconds` (default: 60), it checks expiry
3. If within `refresh_before_seconds` (default: 600) of expiry, it calls `AssumeRole` again and updates the keystore
4. The MCP server is transparently restarted with the fresh credentials
5. If the refresh fails, mcp-launcher logs a warning and continues with the existing credentials until they expire

---

## Reference: Phase 1 (Static Access Key)

> Phase 1 is simpler but stores long-lived credentials and provides no rotation. Use Phase 2 above unless you have a specific reason not to.

**1. Register long-lived credentials**

```bash
mcp-launcher register my-aws-mcp AWS_ACCESS_KEY_ID AKIAIOSFODNN7EXAMPLE
mcp-launcher register my-aws-mcp AWS_SECRET_ACCESS_KEY wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
```

**2. `launcher.json`**

```json
{
  "my-aws-mcp": {
    "command": "C:\\path\\to\\aws-mcp-server.exe",
    "args": [],
    "env_keys": {
      "AWS_ACCESS_KEY_ID":     "mcp-launcher/my-aws-mcp/AWS_ACCESS_KEY_ID",
      "AWS_SECRET_ACCESS_KEY": "mcp-launcher/my-aws-mcp/AWS_SECRET_ACCESS_KEY"
    }
  }
}
```

No `token_source` block is needed for Phase 1. The credentials are injected at launch and never rotated.
