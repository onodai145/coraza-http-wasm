# 更新・検証記録

## 実装手順

1. `mise.toml`だけでGo 1.23.12 / TinyGo 0.34.0を固定し、mainに初回コミット。
2. 両リポジトリの`chore/update-coraza-crs`で更新。
3. CRS v4.30.0を分割モジュールへ移行。Coraza v3.2.1の公式推奨設定を一時保持してCRS単独更新を検証。
4. Coraza v3.8.1と必要なWASI依存を更新し、互換なmiseツールチェーンを固定。HTTP WASM APIは維持。
5. 実CRS E2E・FTWを実行し、失敗は原因とともに記録。
6. フォーク成果物を明示バージョンで取得するLocal Plugin構成、CIとリリース手順を整備。

push、タグのリモート送信、Release公開、本番反映は行わない。

## 公式情報（2026-11-07確認）

- Coraza最新安定版: v3.8.1。GitHub latest APIとGo proxyの`@latest`が一致。最低Go 1.25.0。
  https://github.com/corazawaf/coraza/releases/tag/v3.8.1
- OWASP CRS最新安定版: v4.30.0。同様にGitHub latest APIとGoモジュールで確認。
  https://github.com/coreruleset/coreruleset/releases/tag/v4.30.0
- 旧`coraza-coreruleset/v4`はv4.25.0で凍結。`crs/v4`、`coraza/v3`、`crs/tests/v4`へ分割。CRS v4.30.0モジュールはGo 1.23以上。
  https://github.com/corazawaf/coraza-coreruleset#modules
- `coraza-wasilibs v0.3.0`はGo 1.26.0以上、Coraza v3.8.1を要求。
  https://github.com/corazawaf/coraza-wasilibs/blob/v0.3.0/go.mod
- TinyGo v0.43.0の対応Goは1.25–1.27。`scheduler=none`はgoroutineを許さず、タイマー登録もpanicする。
  https://github.com/tinygo-org/tinygo/blob/v0.43.0/builder/config.go
  https://github.com/tinygo-org/tinygo/blob/v0.43.0/src/runtime/scheduler_none.go

## CRS単独更新時の互換性

最新推奨設定v3.8.1は`SecRxPreFilter`を含み、Coraza v3.2.1は`unknown directive "secrxprefilter"`で初期化失敗。
CRS単独更新の検証では旧推奨設定をそのまま保持する。ルールを無効化して回避しない。

CRS単独更新: Go 1.23.12 / TinyGo 0.34.0 / Coraza v3.2.1のまま、ビルド、unit、既存E2E 9件、実CRS E2E 15件は成功。
FTWは新形式対応のv1.3.0へ移行。既存の壊れたMIME指定と既定アクション重複を修正。
FTWの全件実行はOOMによって中断。成功扱いにしない。FTW用モジュールはGOWORK=offで独立実行し、WASM依存の意図しない引き上げを防ぐ。
