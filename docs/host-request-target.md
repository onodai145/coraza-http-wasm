# raw fragment拒否のホスト修正案

標準HTTP WASM host v0.7.0 / Traefik v3.6.0ではFTW `920610-2`が失敗する。
パス部分ではWASMだけで元のraw `#`とエンコード済み`%23`を区別できない。
この修正案は**標準Traefikへ未反映**であり、通常のgo.modやCIは切り替えていない。
ユーザーの運用方針によりTraefik本体のカスタムビルドは採用しない。このパッチは参考資料として保持し、標準Traefikでの既知の制約を管理する。

## 原因と修正範囲

Goはパス内のraw `#`を`RequestURI`および`URL.RawPath`へ保存する。
ホストのGetURIがパスをEscapedPath()で再構成すると`#`が`%23`になり、CRSが生の区切り文字を見られない。
クエリはRawQueryのまま渡されるため、この情報消失の問題はパス部分に限る。
GetURIの返却値を全面的にrawへ変える最初の試作は、既存SetURIテストの空白正規化を壊したため不採用とした。

採用候補のパッチは`guest.ServeHTTP`入口で、元の`RequestURI`に未エンコードの`#`があればHTTP 400を返す。
パス、クエリ、absolute-formを対象とし、`%23`や`%2523`は拒否しない。
クエリ内のraw `#`も拒否するのは、fragmentをrequest targetに含めないプロトコル制約を一貫して適用するため。
既存のGetURI/SetURI、URI書換え、Corazaのルールエンジン、WASM ABIは変更しない。
拒否はプロトコル検証であり、DetectionOnlyやSecRuleEngine Offでも適用される。

RFC 9110 §7.1ではtarget URIからfragmentを除外する。
RFC 9112 §3.2は不正なrequest-lineへ400または適切なredirectを推奨し、自動修正して処理することを避けるとしている。

- https://www.rfc-editor.org/rfc/rfc9110.html#section-7.1
- https://www.rfc-editor.org/rfc/rfc9112.html#section-3.2
- https://github.com/http-wasm/http-wasm-host-go/blob/v0.7.0/handler/nethttp/host.go
- https://github.com/traefik/traefik/blob/v3.6.0/pkg/plugins/wasm.go

パッチ: `patches/http-wasm-host-go-v0.7.0-request-target.patch`。
元モジュールはApache-2.0。パッチは入口の400拒否と回帰テスト7ケースのみ。

## 検証

- 未修正ホストではrawパス・クエリ・absolute-formの3ケースが204で通過して失敗し、修正後は400となりguest未到達を確認。
- 修正ホストの既存net/httpテストと追加7ケースは成功。
- 実WASM/実CRSの920610-1（%23非検知）と920610-2（raw #は400）は両方成功。対象抽出実行のskip 5191は全件成功を意味しない。
- 修正ホストでFTW全件対象（FTW_INCLUDE指定なし）のTestFTWが成功（317.279秒、exit 0）。既存ignore設定は変更していない。成功時のGo標準出力には内訳が出ないため、ログはパッケージの成功結果となる。通常ホストの失敗結果は変更しない。

ログ: `build/host-request-target-{red,green,ftw,ftw-full}.log`。
WASM SHA256: `4ac4f33e131d66d568f6f5a079205f06f1deb289fe41d1a7800259a65aea4f6a`。

## 再現手順

このリポジトリのルートから実行する。隔離コピーと別modfileを使い、通常の依存やTraefikは変更しない。

```sh
mkdir -p build
probe_dir=$(mktemp -d "$PWD/build/host-target.XXXXXX")
host_mod_dir=$(mise exec -- go list -m -f '{{.Dir}}' github.com/http-wasm/http-wasm-host-go)
cp -R "$host_mod_dir"/. "$probe_dir"/
chmod -R u+w "$probe_dir"
patch -d "$probe_dir" -p1 < patches/http-wasm-host-go-v0.7.0-request-target.patch
GOWORK=off GOMAXPROCS=2 mise exec -- go test -C "$probe_dir" ./handler/nethttp

cp testing/coreruleset/go.mod "$probe_dir/ftw.mod"
cp testing/coreruleset/go.sum "$probe_dir/ftw.sum"
GOWORK=off mise exec -- go mod edit -modfile="$probe_dir/ftw.mod" \
  -replace="github.com/http-wasm/http-wasm-host-go=$probe_dir"
mkdir -p testing/coreruleset/build
cp build/coraza-http-wasm.wasm testing/coreruleset/build/coraza-http-wasm.wasm
GOWORK=off GOMAXPROCS=2 GOMEMLIMIT=768MiB FTW_INCLUDE='' mise exec -- go test \
  -C testing/coreruleset -modfile="$probe_dir/ftw.mod" -count=1 -timeout=15m -v .
```

## 配布上の制約

WASMを入れ替えてもTraefikに組み込まれたGoホストは変わらない。
適用にはホスト側での上流修正とTraefikへの取り込み、または修正ホストを組み込んだTraefikのカスタムビルドが必要。
どちらも現時点で未実施。標準TraefikのFTW残件を解消した扱いにはしない。
SSEは別のホスト制約であり、このパッチでは対応しない。
