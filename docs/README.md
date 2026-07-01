# mcp-launcher Documentation Index

各ドキュメントの概要と主な内容を記載します。ファイルを修正した際はここも合わせて更新してください。

---

## Setup

### [setup/aws-sts-setup.md](setup/aws-sts-setup.md)
AWS STS を使った Phase 2 セットアップガイド（推奨）。

- IAM Role の作成方法（信頼ポリシー・ポリシーのアタッチ）
- ROLE_ARN のキーストア登録
- `launcher.json` の正しい書き方（`env_keys` のフラットなキー名・`target_env_key` のルール）
- `claude_desktop_config.json` の設定
- 自動ローテーションの仕組み
- Phase 1（静的アクセスキー）の書き方（末尾に参考として記載）

### [setup/github-app-setup.md](setup/github-app-setup.md)
GitHub App を使った Phase 2 セットアップガイド。

- GitHub App の作成・秘密鍵の生成
- APP_ID / PRIVATE_KEY / INSTALLATION_ID のキーストア登録
- `launcher.json` の設定例
- トークンローテーションの仕組み

### [setup/wsl-setup.md](setup/wsl-setup.md)
WSL（Windows Subsystem for Linux）環境についての注意事項。

- **WSL での mcp-launcher 実行は非推奨**（キーストアが利用不可）
- Windows ホスト上で直接作業することを推奨
- AWS CLI は引き続き WSL 側で使用可能（IAM Role 作成など）
- WSL からのクロスコンパイル方法（開発者向け）

---

## Architecture

### [architecture/configuration-reference.md](architecture/configuration-reference.md)
`launcher.json` の全フィールドのリファレンス。

- サービス設定フィールド（`command` / `args` / `env_keys` / `token_source` / `check_interval_seconds` / `drain_timeout_seconds`）
- `token_source` 共通フィールド（`type` / `target_env_key` / `refresh_before_seconds`）
- `type: "github_app"` 固有フィールド
- `type: "aws_sts"` 固有フィールド
- 対応プラットフォームとキーストアバックエンド一覧

### [architecture/security-model.md](architecture/security-model.md)
セキュリティモデルと脅威分析。

- シークレットの保存場所と Claude・MCP サーバーへの可視性の整理
- 短命トークンがプロンプトインジェクション対策として有効な理由
- 保護される脅威・保護されない脅威の一覧
- コントリビューター向けセキュリティノート

### [architecture/adding-a-token-source.md](architecture/adding-a-token-source.md)
新しいトークンソースを追加するコントリビューター向けガイド。

- トークンローテーションパイプラインの構造（`TokenFetcher` インターフェース）
- 複数クレデンシャルを返すソース（AWS STS 型）の実装パターン
- 新規追加のステップ（config フィールド追加 → fetcher 実装 → main.go への配線）
- 追加チェックリスト
- 将来の候補トークンソース（GCP / Azure / Vault 等）

---

## CLI Reference

`mcp-token` CLI のコマンド一覧は [README > mcp-token](../README.md#mcp-token--on-demand-token-broker--keystore-cli) を参照してください。
