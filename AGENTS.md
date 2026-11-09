# Repository Guidelines

## プロジェクト構成

CorazaとOWASP CRSを利用するHTTP-WASM WAFです。

- `main.go`：設定読込、WAF初期化、リクエスト・レスポンス処理。
- `body.go`：本文処理。`init_tinygo.go`：TinyGo向けGC設定。`operators/`：ビルド対象別の演算子登録。
- ルートの`*_test.go`：単体テスト、HTTP E2E、ホスト統合例。
- `testing/coreruleset/`：別GoモジュールのFTW回帰テスト。ルートとともに`go.work`で管理する。
- `testdata/directives.conf`：テスト用設定。`build/`：生成WASM。`.github/workflows/`：CI・リリース処理。

## ビルド・開発コマンド

`mise install`で`mise.toml`のGo 1.23.12・TinyGo 0.34.0を導入します。以下はリポジトリルートで実行してください。

```sh
mise exec -- go run mage.go -l     # ターゲット一覧
mise exec -- go run mage.go build  # build/coraza-http-wasm.wasmを生成
mise exec -- go run mage.go test   # 単体テスト・Example
mise exec -- go run mage.go e2e    # WASMを読み込むHTTP E2E
mise exec -- go run mage.go ftw    # CRSのFTW回帰テスト
mise exec -- go run mage.go format # go mod tidyとgosimports
mise exec -- go run mage.go lint   # golangci-lintと整形差分の検査
```

テストは生成WASMを埋め込むため、`test`・`e2e`・`ftw`の前に再ビルドしてください。`ftw`はWASMをテストモジュールへコピーします。`format`は依存ファイルも変更し、`lint`も整形を実行するため、実行後の差分を確認します。CIのツールチェーン指定はローカルと異なるため、`.github/workflows/ci.yaml`も確認してください。

## コーディング規約

Go標準のタブインデントとgofmt互換の整形を使用します。公開識別子は`MixedCaps`、非公開識別子は`mixedCaps`、複数語のファイル名は`snake_case.go`に揃えます。既存の`tinygo`・`e2e`ビルドタグ、HTTP-WASM ABI、プラットフォーム別演算子登録を維持してください。Lint設定は`.golangci.yml`、ツールの指定は`magefile.go`を参照します。

## テスト方針

Goの`testing`、`testify/require`、wazero上のE2E、go-ftwを使用します。テスト名は`TestXxx`、E2Eは`TestE2EXxx`、ファイル名は`*_test.go`とします。数値のカバレッジ閾値はCIで指定されていません。

WAF処理を変更したら、正常通信と攻撃、検査対象のヘッダ・本文・遮断応答に回帰テストを追加してください。CRSや演算子の変更ではFTWも実行し、失敗・除外・未実行を明示します。失敗を隠すskipや期待値緩和は行わず、再現条件とログを記録してください。

## ブランチ・コミット・Pull Request

作業開始時は`git status --short`を確認し、`main`から専用ブランチを作成します。例：`git switch -c docs/add-repository-guidelines main`。既存の未コミット変更を保持してください。

コミットは履歴に合わせて`feat:`、`fix:`、`chore:`などの接頭辞と短い命令形の説明を使用します。例：`fix: preserve request body after inspection`。

PRには変更理由、影響範囲、関連Issue、実行コマンドと結果を記載します。依存・設定変更では互換性への影響も説明し、提出前に`git diff --check`を実行してください。

## セキュリティ・設定

設定例は`README.md`のJSON `directives`と`testdata/directives.conf`を参照します。デバッグログへ機密情報を残さず、検証結果を共有する際は認証情報を除去してください。タグpushはCIのDraft Release作成・成果物アップロードを起動します。push・リリース公開・本番反映には明示的な承認が必要です。
