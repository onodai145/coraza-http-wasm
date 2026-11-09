# 本文遅延の診断

同じCoraza/CRSを、HTTP WASM hostまたは通常のGo HTTP middlewareで直列に実行する。
ネットワークやTraefikを起動せず、フォーム `q=` + `a` の繰返しをバックエンドへ渡して、HTTP200と本文全バイトの一致を必須とする。
本文は指定サイズ全体で最大1 MiB、repeatは最大5。小さい本文で1回warmupし、その後を計測する。
通常設定・WASM・CRSルールを変更しない。load testやセキュリティテストの合格判定には使わない。

リポジトリrootで実行する。

```sh
# 現在のWASM。各サイズ1要求なのでp95等の統計は求めない。
GOMAXPROCS=2 GOMEMLIMIT=768MiB mise exec -- go run ./testing/bodylatency \
  -sizes 1024,16384,65536,262144,1048576

# 同じCoraza/CRSを通常のGoで比較する。
GOMAXPROCS=2 GOMEMLIMIT=768MiB mise exec -- go run ./testing/bodylatency \
  -native -sizes 16384,65536,262144,1048576 -repeat 2

# wasmの演算子・変換・regexp境界をwazero FunctionListenerで計測する。
GOMAXPROCS=2 GOMEMLIMIT=768MiB mise exec -- go run ./testing/bodylatency \
  -sizes 65536 -functions

# Coraza公開DEBUGイベントでルール開始〜終了を集計する。
GOMAXPROCS=2 GOMEMLIMIT=768MiB mise exec -- go run ./testing/bodylatency \
  -sizes 16384 -rule-times

# 実験的プリフィルタの診断比較。通常設定への適用は行わない。
GOMAXPROCS=2 GOMEMLIMIT=768MiB mise exec -- go run ./testing/bodylatency \
  -sizes 65536,1048576 -prefilter On
```

結果JSONをstdout、各要求の時間をstderrへ出力する。WASMではSHA256も出力する。
`-functions`と`-rule-times`は併用不可。計測時にはlistener/loggingの負荷が加わる。
時間はinclusive/exclusiveを区別し、入れ子のinclusive時間を合計しない。
DEBUGでは引数文字列の整形も実行されるため、小さい本文を用いる。引数内容は診断出力へ保存しない。

`-rules parser`はCRSを含めず本文解析を比較する診断条件。
`-prefilter On`は実験機能であり、本ツールの正常本文だけでは検知互換性を確認できない。
`-interruptible`はwazeroのWithCloseOnContextDoneを有効化するが、実行コストが変わるため通常条件と混ぜない。
既定runtimeはHTTP WASM hostの通常compiler設定と同じく、このフラグを無効にする。
5分のcontextは用意するが、既定runtimeでは実行中guestの強制中断を保証しない。単発・上記のサイズで実行する。

[調査結果](../../docs/body-latency.md)と[実Traefik負荷試験](../../docs/load-validation.md)を参照。
