# coraza-http-wasm

> CRS v4.30.0とCoraza v3.8.1の更新・基本動作を検証済みです。SSEとFTW 920610-2は[既知のバグ](docs/known-bugs.md)として失敗結果を保持しています。性能調整・任意の引数制限・本番導入は別作業です。[現在の状況](docs/final-status.md)と[負荷試験の記録](docs/load-validation.md)を参照してください。

Web Application Firewall WASM middleware built on top of Coraza and implementing the [http-wasm](https://http-wasm.io/) ABI.

## Getting started

`go run mage.go -l` lists all the available commands:

```bash
$ go run mage.go -l
Targets:
  build*    builds the wasm binary.
  e2e       runs e2e tests
  format    formats code in this repository.
  ftw       runs the FTW test suite
  lint      verifies code format.
  test      runs all unit tests.

* default target
```

### Building the binary

```bash
go run mage.go build
```

You will find the WASM plugin under `./build/coraza-http-wasm.wasm`.

### Basic Configuration

```json
{
   "directives": [
    "SecRuleEngine On",
    "SecDebugLog /dev/stdout",
    "SecDebugLogLevel 9",
    "SecRule REQUEST_URI \"@streq /admin\" \"id:101,phase:1,log,deny,status:403\""
   ]
  }
```

### Test it

```console
curl -I 'http://localhost:8080/admin'    # 403
curl -I 'http://localhost:8080/anything' # 200
```

## フォークのビルドと更新

ツールチェーンは`mise.toml`で固定します。OOM対策としてTinyGoのビルド並列数は2です。

```sh
mise install
mise exec -- go run mage.go build
mise exec -- go run mage.go test
mise exec -- go run mage.go e2e
mise exec -- go run mage.go ftw
```

E2Eには実際の組み込みCRSを使う18ケースを含みます。FTWは別Goモジュールで実行します。
依存関係の調査・互換性・検証上の制限は[検証記録](docs/update-validation.md)を参照してください。

すべてのWAF対象ルートで、ホストへ渡す前に有限のリクエスト本文上限を設定してください。
本文保持のための再構築では、host v0.7.0が本文全量をメモリに保持します。
Traefikではサービス別のbuffering `maxRequestBodyBytes`をWAFより前に配置します。
超過時はDetectionOnlyでも413です。Corazaの検査上限とDetectionOnlyのProcessPartialは維持します。
13 MiBはTraefikサンプルの値であり、全サービス共通の既定値ではありません。
上限と同時処理数に応じたメモリ・ディスク容量の評価も必要です。
`SecRequestBodyInMemoryLimit`は`SecRequestBodyLimit`と同じ値に設定してください。
WASMには検査本文のディスク退避先がなく、上流推奨設定の128 KiBを超える本文でエラーになります。
Traefikサンプルでは両方を`13107200`（12.5 MiB）に設定します。

成果物は`build/coraza-http-wasm.wasm`。隣接するTraefikフォークで次を実行すると、公開Releaseを作らずLocal Pluginへ取り込めます。

```sh
cd ../coraza-http-wasm-traefik
WASM_SOURCE=../coraza-http-wasm/build/coraza-http-wasm.wasm mise exec -- go run mage.go prepareLocal
```

リリース時は、検証済みコミットに`v0.4.0-onodai.1`のような独自タグを作成します。
タグのpush前にレビュー・承認が必要です。タグpush後のCIはテストとビルドを実施し、
WASM、ZIP、SHA256SUMSをDraft Releaseへ保存します。公開と本番反映は手動承認で行います。
Dependabotは更新PRを作成するだけで、自動マージやデプロイは行いません。

現時点で完全E2EにはSSE 1件の既知の失敗があります。レスポンスをバッファリングするHTTP WASM host v0.7.0の制約で、テストはスキップしていません。CIは失敗を返し、Release生成をブロックします。
SDKの長文字列・空本文書き込みのローカル修正は[PATCHES.md](third_party/http-wasm-guest-tinygo/PATCHES.md)を参照してください。
