# WSL (Windows Subsystem for Linux) Setup

> ⚠️ **WSL での mcp-launcher 実行は非推奨です。**
> WSL はデスクトップセッションを持たないため、Linux のキーストアバックエンド（GNOME Keyring / libsecret）が利用できません。
> **Windows ホスト上で直接作業することを推奨します。**

---

## なぜ WSL では動かないのか

WSL は D-Bus セッションとキーリングデーモンをデフォルトで持ちません。Linux バイナリを使おうとすると以下のようなエラーになります：

```
failed to unlock correct collection '/org/freedesktop/secrets/aliases/default'
```

```
dial unix /tmp/dbus-XXXXXXXX: connect: no such file or directory
```

---

## 推奨: Windows ホストで直接作業する

mcp-launcher の全操作（バイナリの配置・シークレットの登録・`launcher.json` の作成）は **PowerShell** で行います。

### バイナリの配置

[Releases](https://github.com/masuda-masuo/mcp-launcher/releases) からWindows用バイナリをダウンロードして配置します：

```
C:\work\mcp\mcp-launcher.exe
```

### シークレットの登録（PowerShell）

```powershell
C:\work\mcp\mcp-token.exe register github APP_ID 123456
C:\work\mcp\mcp-token.exe register github INSTALLATION_ID 7654321
C:\work\mcp\mcp-token.exe register github PRIVATE_KEY (Get-Content C:\path\to\private-key.pem -Raw)
```

シークレットは Windows Credential Manager に保存されます。

### `launcher.json` と Claude Desktop の設定

`launcher.json` は `mcp-launcher.exe` と同じディレクトリに置けば自動で読み込まれます。

詳細は各セットアップガイドを参照してください：

- [AWS STS setup →](aws-sts-setup.md)
- [GitHub App setup →](github-app-setup.md)

---

## WSL からの AWS CLI 利用について

AWS CLI は引き続き WSL 側で使用できます（IAM Role の作成など）。mcp-launcher 本体と `launcher.json` だけ Windows ホスト側に置けば問題ありません。

```
WSL側:   aws cli（IAM Role作成・確認など）
Windows側: mcp-launcher.exe + launcher.json（シークレット管理・MCP起動）
```

---

## WSL からビルドする場合（開発者向け）

Windows バイナリを WSL からクロスコンパイルすることは可能です：

```bash
cd /home/<you>/dev/projects/mcp-launcher
GOOS=windows GOARCH=amd64 go build -o mcp-launcher.exe ./cmd/launcher
```

ビルドしたバイナリを PowerShell でコピーします：

```powershell
copy \\wsl.localhost\Ubuntu\home\<you>\dev\projects\mcp-launcher\mcp-launcher.exe C:\work\mcp\mcp-launcher.exe
```

その後のシークレット登録・設定は PowerShell で行います。
