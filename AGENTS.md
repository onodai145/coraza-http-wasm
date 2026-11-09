# Repository Guidelines

## プロジェクト構成

CorazaとOWASP CRSを利用するHTTP-WASM WAFです。

- `main.go`：設定読込、WAF初期化、リクエスト・レスポンス処理。
- `body.go`：本文保持。`init_reactor.go`：WASM reactor起動。`operators/`：ビルド対象別の演算子登録。
- ルートの`*_test.go`：単体テスト、HTTP E2E、ホスト統合例。
- `testing/coreruleset/`：別GoモジュールのFTW回帰テスト。`testing/load/`など：負荷・遅延・引数制限の検証。
- `third_party/http-wasm-guest-tinygo/`：修正済みSDK。変更時は`PATCHES.md`とLICENSEを維持する。
- `docs/`：検証記録・既知バグ。`build/`：生成WASM。`.github/workflows/`：CI・リリース処理。

## ビルド・開発コマンド

`mise install`で`mise.toml`のGo 1.26.10・TinyGo 0.43.0を導入します。以下はリポジトリルートで実行してください。

```sh
mise exec -- go run mage.go -l     # ターゲット一覧
mise exec -- go run mage.go build  # build/coraza-http-wasm.wasmを生成
mise exec -- go run mage.go test   # 単体テスト・Example
mise exec -- go run mage.go e2e    # WASMを読み込むHTTP E2E
mise exec -- go run mage.go ftw    # CRSのFTW回帰テスト
mise exec -- go run mage.go format # go mod tidyとgosimports
mise exec -- go run mage.go lint   # golangci-lintと整形差分の検査
```

テストは生成WASMを埋め込むため、`test`・`e2e`・`ftw`の前に再ビルドしてください。`ftw`はWASMをコピーし、`GOWORK=off`でテストモジュールを実行します。`format`は依存ファイルも変更し、`lint`も整形を実行するため、実行後の差分を確認します。

## コーディング規約

Go標準のタブインデントとgofmt互換の整形を使用します。公開識別子は`MixedCaps`、非公開識別子は`mixedCaps`、複数語のファイル名は`snake_case.go`に揃えます。既存の`tinygo`・`e2e`ビルドタグ、HTTP-WASM ABI、プラットフォーム別演算子登録を維持してください。Lint設定は`.golangci.yml`、ツールの指定は`magefile.go`を参照します。

## テスト方針

Goの`testing`、`testify/require`、wazero上のE2E、go-ftwを使用します。テスト名は`TestXxx`、E2Eは`TestE2EXxx`、ファイル名は`*_test.go`とします。数値のカバレッジ閾値はCIで指定されていません。

WAF変更では正常通信・攻撃・DetectionOnly・403・異常スコア・リクエスト／レスポンス本文を実CRSで確認します。CRSや演算子の変更ではFTWも実行します。既知のSSE失敗とFTW `920610-2`の判定は`docs/known-bugs.md`を参照してください。失敗・除外・未実行を明示し、skipや期待値緩和で隠さず、再現条件とログを記録します。

## ブランチ・コミット・Pull Request

作業開始時は`git status --short`を確認し、`main`から専用ブランチを作成します。例：`git switch -c docs/add-repository-guidelines main`。既存の未コミット変更を保持してください。

コミットは履歴に合わせて`feat:`、`fix:`、`chore:`などの接頭辞と短い命令形の説明を使用します。例：`fix: preserve request body after inspection`。

PRには変更理由、影響範囲、関連Issue、実行コマンドと結果を記載します。依存・設定変更では互換性への影響も説明し、提出前に`git diff --check`を実行してください。

## セキュリティ・設定

設定例は`README.md`のJSON `directives`を参照します。全WAFルートでホストに渡す前に有限の本文上限を設定し、WASMでは`SecRequestBodyInMemoryLimit`を`SecRequestBodyLimit`と同値にします。制限値はサービス要件と同時処理数で評価してください。ログ共有時は認証情報を除去します。`v`タグpushはCI成功時にDraft Release作成を起動します。push・リリース公開・本番反映には明示的な承認が必要です。
