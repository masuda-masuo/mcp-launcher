# Phase 2: Short-lived AWS Credentials via STS

Instead of storing long-lived IAM user access keys, mcp-launcher can assume an IAM Role via AWS STS and automatically rotate the resulting short-lived credentials (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN`).

The base credentials used to call `AssumeRole` are loaded from the **standard AWS credential chain** (environment variables, `~/.aws/credentials`, EC2/ECS instance profile, etc.) — they are never registered in the launcher keystore.

## Step 1: Register the IAM Role ARN in the keystore

```bash
mcp-launcher register my-aws-mcp ROLE_ARN arn:aws:iam::123456789012:role/MyMCPRole
```

## Step 2: Update `launcher.json`

```json
{
  "my-aws-mcp": {
    "command": "/path/to/aws-mcp-server",
    "args": ["stdio"],
    "env_keys": {
      "AWS_ACCESS_KEY_ID":     "mcp-launcher/my-aws-mcp/AWS_ACCESS_KEY_ID",
      "AWS_SECRET_ACCESS_KEY": "mcp-launcher/my-aws-mcp/AWS_SECRET_ACCESS_KEY",
      "AWS_SESSION_TOKEN":     "mcp-launcher/my-aws-mcp/AWS_SESSION_TOKEN"
    },
    "token_source": {
      "type": "aws_sts",
      "role_arn_key": "mcp-launcher/my-aws-mcp/ROLE_ARN",
      "role_session_name": "mcp-launcher-session",
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

- `role_arn_key` — keystore key where the IAM Role ARN is stored
- `role_session_name` — appears in CloudTrail logs
- `duration_seconds` — lifetime of the STS credentials (default: 3600; min: 900)
- `target_env_key` — prefix for the three keystore keys: `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN`

## How credential rotation works

- On first launch, mcp-launcher calls `AssumeRole` and stores all three credentials in the keystore
- Every `check_interval_seconds`, it checks expiry and calls `AssumeRole` again if within `refresh_before_seconds` of expiry
- The MCP server is transparently restarted with the fresh credentials
- If the refresh fails, mcp-launcher logs a warning and continues with the existing credentials
