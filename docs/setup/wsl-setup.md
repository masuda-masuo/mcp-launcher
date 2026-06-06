# WSL (Windows Subsystem for Linux) Setup

WSL does not have a desktop session, so the Linux keystore backend (GNOME Keyring / libsecret) is not reliably available. Use the **Windows-native binaries** and the **Windows Credential Manager** instead.

## Why the Linux keystore doesn't work in WSL

WSL lacks a D-Bus session and a running keyring daemon by default. Attempting to use the Linux binary results in errors like:

```
failed to unlock correct collection '/org/freedesktop/secrets/aliases/default'
```

or

```
dial unix /tmp/dbus-XXXXXXXX: connect: no such file or directory
```

## Step 1: Build Windows binaries from WSL

```bash
# Build mcp-launcher for Windows
cd /home/<you>/dev/projects/mcp-launcher
GOOS=windows GOARCH=amd64 go build -o mcp-launcher-windows.exe ./cmd/launcher

# Build github-mcp-server for Windows
cd /home/<you>/dev/projects/github-mcp-server
GOOS=windows GOARCH=amd64 go build -o github-mcp-server.exe ./cmd/github-mcp-server
```

## Step 2: Copy binaries to a Windows path

Run in PowerShell:

```powershell
copy \\wsl.localhost\Ubuntu\home\<you>\dev\projects\mcp-launcher\mcp-launcher-windows.exe C:\Users\<you>\mcp-launcher.exe
copy \\wsl.localhost\Ubuntu\dev\projects\github-mcp-server\github-mcp-server.exe C:\Users\<you>\github-mcp-server.exe
```

## Step 3: Register secrets using the Windows binary

Run in PowerShell (this stores secrets in Windows Credential Manager):

```powershell
C:\Users\<you>\mcp-launcher.exe register github APP_ID 123456
C:\Users\<you>\mcp-launcher.exe register github INSTALLATION_ID 7654321
C:\Users\<you>\mcp-launcher.exe register github PRIVATE_KEY (Get-Content \\wsl.localhost\Ubuntu\home\<you>\...\private-key.pem -Raw)
```

## Step 4: Update `launcher.json` to use Windows paths

```json
{
  "github": {
    "command": "C:\\Users\\<you>\\github-mcp-server.exe",
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

Copy `launcher.json` to a Windows path (e.g. `C:\Users\<you>\launcher.json`) so the Windows binary can find it.

## Step 5: Configure your MCP client

```json
{
  "command": "C:\\Users\\<you>\\mcp-launcher.exe",
  "args": ["github"],
  "env": {
    "MCP_LAUNCHER_CONFIG": "C:\\Users\\<you>\\launcher.json"
  }
}
```

## Verify

Run from PowerShell to confirm everything works end-to-end:

```powershell
$env:MCP_LAUNCHER_CONFIG = "C:\Users\<you>\launcher.json"
C:\Users\<you>\mcp-launcher.exe github
```

You should see output like:

```
GitHub MCP Server running on stdio
```