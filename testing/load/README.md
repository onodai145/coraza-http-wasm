# ローカル負荷試験

Linux上のPython 3、PyYAML、Docker Composeを使用する。WASMとLocal Pluginを先にビルド・配置する。
通常の設定ファイルを直接編集せず、build/load-validation以下へ実行ごとの設定・ログを生成する。
標準Traefik v3.6.0とgo-httpbin v2.9.0のローカルイメージを使う（自動pullしない）。

```sh
mise exec -- go run mage.go build
(cd ../coraza-http-wasm-traefik && WASM_SOURCE=../coraza-http-wasm/build/coraza-http-wasm.wasm mise exec -- go run mage.go prepareLocal)
python3 testing/load/run.py
```

localhostの18081（WAFあり）、18082（WAFなし）、18083（検証専用のheap診断）を使用する。
2つのTraefikは各2 CPU / 2 GiB / swapなし、httpbinは1 CPU / 512 MiB / swapなし。
CPU割当てに合わせ、両TraefikはGOMAXPROCS=2、httpbinはGOMAXPROCS=1を指定する。
Goホストのメモリ目標GOMEMLIMIT=768MiBを指定する。これはWASMを含む全メモリのhard limitではない。
診断用GODEBUG=gctrace=1、ローカル限定debug APIはこの試験専用設定であり、通常のサンプル設定には追加しない。

1/4/8同時接続、最大2 req/s/worker、正常POST・SQLi・XSS・multipart・本文上限を確認し、混合負荷を600秒継続する。
フォーム本文とmultipart生本文はバックエンド受信内容の長さ・SHA256を照合する。
1 MiBを単一のフォーム引数へ入れるケースは今回の環境で約63秒を要しており、試験全体は15分以上かかる場合がある。
遅延値や処理件数は参考値で、本番SLAの合否を自動判定しない。

メモリ85%、host available 4 GiB未満、disk free 10 GiB未満、HTTP状態・本文不一致で停止する。
停止時は当該projectの専用container IDだけへdocker stop（5秒猶予）を行い、実行中要求を終了させる。
停止・失敗時もsummaryとログを保存し、成功扱いにはしない。finallyで専用projectを削除する。

各runのsummary.jsonのstatusを確認する。results.jsonのabortedも確認し、実行途中の結果だけで全体成功としない。
resources.jsonlはサンプリング値であり、瞬間的ピークの上限保証ではない。

今回の測定・失敗試行・設定修正は[検証記録](../../docs/load-validation.md)を参照。

## バックエンド期限の対照試験

標準go-httpbin v2.9.0イメージのヘッダ読取期限は1秒固定で、今回の2 CPU・8同時混合ではEOFによる502を再現した。
Go並列数・メモリ目標の指定や接続再利用の停止だけでは解消しなかった。
バックエンドがリクエストヘッダを受け取る前に期限で閉じることを調べるため、同じhttpbin v2.9.0ライブラリを使う検証用サーバを別moduleで用意した。
Traefik本体は公式イメージを使用し、追加ビルドは試験用バックエンドだけに限定する。

```sh
GOWORK=off CGO_ENABLED=0 GOMAXPROCS=2 GOMEMLIMIT=768MiB mise exec -- go build \
  -C testing/load/backend -mod=readonly -o "$PWD/build/load-validation/httpbin-timeout-backend" .
LOAD_BACKEND_HEADER_TIMEOUT=1s python3 testing/load/run.py --profile soak --debug
LOAD_BACKEND_HEADER_TIMEOUT=30s python3 testing/load/run.py --profile soak --debug
```

`LOAD_BACKEND_HEADER_TIMEOUT`を指定したときだけ、上記binaryをtest backendへマウントする。指定しなければ標準イメージを使用する。
`--profile soak`は600秒混合＋30秒観測のみを実行し、大容量/段階試験の成功を意味しない。`--profile full`が既定。
`--debug`はDEBUGと5xx access logを保存する診断用設定である。

検証用サーバ間で変更するのはヘッダ期限1秒/30秒だけ。両方とも本文読取/応答期限120秒、idle60秒である。
標準イメージとはGoの版やその他の期限設定も異なるので、制御サーバでの成功を標準イメージの成功と読み替えない。
1秒対照では接続New→Closedが約1.000秒で、HTTP処理のActiveへ進まず502となることを確認した。
30秒の結果・残る本番条件は検証記録を参照。期限30秒を本番へ一律に推奨するものではない。

## 終了判定の回帰テスト

```sh
python3 -B -m unittest discover -s testing/load -p '*_test.py'
```

監視停止中の遅い異常通知、ログ欠落、cleanup失敗、監視thread残存が成功へ紛れないことを検証する。
EOF後の停止処理中に発生したconnection refused/DNS失敗は、最初のEOFと区別して読む。
EOFやメモリ停止を、SSE/FTW 920610-2の既知バグへ含めてはいけない。
