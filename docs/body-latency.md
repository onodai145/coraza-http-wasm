# 単一フォーム引数の遅延調査（2026-11-08）

主因は、長大な引数に対する複数のCRS正規表現の評価コストである。現在のTinyGo/WASM経路では、そのコストが通常のGoより大きい。
本文転送・解析だけでは約60秒の遅延を説明できない。単一の遅いルールやコードの不具合を修正して解決した状態ではなく、性能上の問題は残る。

この結論は `application/x-www-form-urlencoded` の `q=` + `a`繰返しという正常入力についての測定であり、全種類の1 MiB本文やファイルアップロードの所要時間を意味しない。
Coraza v3.8.1、CRS v4.30.0、Go 1.26.10、TinyGo 0.43.0。WASM SHA256は `4ac4f33e131d66d568f6f5a079205f06f1deb289fe41d1a7800259a65aea4f6a`。
WASM、製品コード、Traefikの通常設定、ルール除外は変更していない。

## 1. 実Traefikで本文サイズを変えた再現

前日の専用harnessを使い、公式Traefik v3.6.0、実CRS、各Traefik2 CPU / 2 GiB・swapなしで、1要求ずつ直列に測定した。
GOMAXPROCS=2、GOMEMLIMIT=768MiB。前日のヘッダ期限502と分離するため、検証用httpbinのヘッダ期限30秒を使用した。本番値の提案ではない。
WAFなしの比較対象も同じバックエンドを使った。HTTP200とバックエンドのフォーム値の長さ・SHA256を確認した。

| 本文全体のサイズ | WAFなし | 実CRSあり |
|---|---:|---:|
| 1 KiB | 1.75 ms | 720 ms |
| 16 KiB | 1.35 ms | 913 ms |
| 64 KiB | 2.47 ms | 3,746 ms |
| 256 KiB | 11.88 ms | 14,452 ms |
| 1 MiB | 32.27 ms | 59,468 ms |

各1サンプルであり、分位点の推定には使わない。1 KiBは起動後最初の正常本文要求で、固定費・GC等の影響を分離できていない。
16 KiB〜1 MiBはサイズにほぼ比例して増加した。同じ1 MiBを追加のCPU採取要求で再現すると59.91秒だった。

同じ1 MiBについて、隔離された診断設定を比較した。

| 診断条件 | 時間 |
|---|---:|
| 実CRSあり・Engine On | 59.47秒 |
| CRS読込みは保持・Engine Off | 0.667秒 |
| 推奨設定の本文解析あり・CRSなし・Engine On | 0.682秒 |

比較のためだけに生成設定を変更し、通常設定には反映していない。Engine Off/CRSなしの結果をセキュリティテスト成功として扱わない。
この比較により、主な遅延箇所をCRS評価へ絞った。転送や再構築にも費用はあるが、約60秒の大部分ではない。
コンテナは全て終了・削除済み。summaryはpassedだがscopeはこの単発診断だけで、標準負荷試験全工程の合格ではない。
14要求が全て完了し、監視・ログ取得・cleanupのエラーなし。WAFメモリの観測最大は662.0 MiB、OOM・再起動なし。

## 2. WASM内の処理時間

標準のHTTP WASM host v0.7.0 / wazero v1.12.0のFunctionListenerを使い、同じWASMをin-processで動かした。
listenerは演算子・変換・正規表現の境界へ付け、文字ごとのcallbackは除いた。64 KiBで全体3,589 msのうち、次を観測した。

- Go `regexp.Regexp.doExecute`: 439回、3,434 ms（全体の約95.7%）。
- `rsc.io/binaryregexp.Regexp.doExecute`: 5回、22.5 ms。
- 最大の変換処理 `removeWhitespace`: 7.3 ms。各変換より正規表現評価が大きい。

listener自体の負荷が加わるため、絶対値の合否判断には計測なしの結果を使う。
TraefikのGo CPU pprofは約90.8%がruntime._ExternalCode、9.2%がruntime.memmoveで、guest内の関数へ帰属できなかった。このためFunctionListenerで切り分けた。

## 3. どのルールへ分散しているか

Corazaの公開DEBUG開始／終了イベントを16 KiBの要求で集計した。guest構造体のメモリlayoutには依存しない。
計測時の全要求1,079 ms、rule exclusive合計1,075 ms。DEBUG文字列整形も含む参考値であり、通常設定の時間ではない。

| rule ID | メッセージ | 参考時間 |
|---|---|---:|
| 934100 | Node.js Injection Attack 1/2 | 92.9 ms |
| 932250 | Remote Command Execution: Direct Unix Command Execution | 52.4 ms |
| 932260 | Remote Command Execution: Direct Unix Command Execution | 51.8 ms |
| 941290 | IE XSS Filters - Attack Detected | 51.4 ms |
| 921110 | HTTP Request Smuggling Attack | 50.2 ms |

最大の1件でも全体の約8.6%で、単一ルールだけが支配していない。932系の合計308 ms、942系194 ms、941系170 ms、934系124 msなどに分散している。
ルールの無効化、skip、ignore追加は行っていない。

## 4. 通常Goおよびプリフィルタとの比較

同一のCoraza/CRSと同じ本文を、ネイティブCoraza HTTP middlewareとHTTP WASM hostでin-process実行した。
どちらも小さい本文でwarmupし、バックエンドの全バイトechoと200を検証した。実Traefik測定との転送・応答形式の違いがあるため、別の表にする。

| 条件 | 64 KiB | 1 MiB |
|---|---:|---:|
| 通常Go、プリフィルタOff（2回の範囲） | 340〜345 ms | 5.49〜5.58秒 |
| WASM、プリフィルタOff、計測器なし | 3.597秒 | 57.48秒 |
| WASM、プリフィルタOn、計測器なし | 1.724秒 | 28.32秒 |

WASMはこの比較で通常Goの約10倍。ただし、この差をTinyGoのscheduler単独の原因と特定したわけではない。
旧バージョン一式との同条件比較も行っていないため、今回の依存更新だけで発生した性能退行とは断定できない。

`SecRxPreFilter On`ではルールを除外せず、正規表現が一致し得ない入力を事前判定する。今回の入力は約半分へ改善したが、1 MiBは28秒残った。
[公式directive説明](https://www.coraza.io/docs/seclang/directives/#secrxprefilter)と[v3.8.1推奨設定](https://github.com/corazawaf/coraza/blob/v3.8.1/coraza.conf-recommended)は実験機能・既定Offとしている。
Onでの検知互換性やFTW全件は今回確認していない。通常設定へ採用せず、速度だけで安全性を判断しない。

`WithCloseOnContextDone=true`を付けた初期診断では、interrupt checkの実行コストが加わった。上の最終比較は通常host同様にfalseへ統一した。
初期のinterruptible条件の結果（Off 1 MiB=103.24秒、On=72.04〜72.07秒）は別条件として保存し、通常条件と混ぜない。

## 再現方法・記録

[診断ツールREADME](../testing/bodylatency/README.md)のコマンドをリポジトリrootで実行する。
Goの通常実行とWASMは現在の依存関係を使い、WASMは既存成果物を読み込むだけで再ビルドしない。

- Docker単発診断: `build/load-validation/20261108-152343/`。生成script: `build/load-validation/body_latency.py`。
- 同実行ログ: `build/load-validation/body-latency-execution-2.log`。最初の0件実行は失敗として`20261108-152314/`へ保存し、単発要求が必ず実行されるよう診断scriptを修正した。
- 通常Go: `build/load-validation/native-latency.{json,log}`。
- guest関数: `build/load-validation/guest-functions-clean-64k.{json,log}`。
- ルールDEBUG: `build/load-validation/guest-rule-debug-16k.{json,log}`。
- 計測器なしの最終比較: `build/load-validation/guest-default-runtime-{Off,On}.{json,log}`。
- 初期の計測負荷が多い関数probe: `guest-function-64k.{json,log}`。guest構造体layoutによるrule ID読出し試作は失敗し、`guest-rules-64k.log`へ保存。試作ソースは`bodylatency-memory-layout-attempt.go.txt`。最終ツールは公開DEBUGイベントへ変更した。

## 次の対応

今回は原因調査であり、通常設定やルールの挙動を変える性能修正は行っていない。
長大なフォーム／JSON引数が不要なサービスなら、必要な本文全体・引数長に応じた入口制限を検討する。具体値はアプリ要件の確認後に決める。
大きな引数が必要なら、この経路で許容遅延を満たせるか評価し、演算子実装や実行方式を別途比較する。単にタイムアウトを延長してもCPUコストは減らない。
プリフィルタを採用候補にする場合は、通常入力だけの今回の結果に加え、FTW全件・検知ID・符号化入力・独自ルールの互換性検証が必要。
既知SSE/FTW 920610-2とは別の性能上の懸念として記録する。push、Release、本番変更は行っていない。

続けて[引数の早期拒否例](argument-limits.md)を隔離検証した。試験値の16/64 KiBを超える要求はOnで後続のCRS phase 2評価より前に413とできたが、上限内の性能とDetectionOnlyの費用は残る。通常設定には適用していない。

## 最終確認

`mage test`（既存unitと診断packageのコンパイル）、`go vet ./testing/bodylatency`、`git diff --check`は成功。
最終ツールのWASM通常計測・関数計測・公開DEBUG計測、ネイティブ通常計測・parser/DEBUGの小規模確認で、200・本文一致・計測stack正常終了を確認した。
レビューでCritical/Importantの指摘なし。WASMの両リポジトリのSHA256は調査開始時と同一。
製品コードを変更していないため、今回E2E/FTW全件は再実行していない。前日の既知SSE/FTW失敗は保持し、プリフィルタOnの互換性確認へ読み替えない。

## 現在の扱い（2026-11-09）

ユーザー指定により、今回の目標はCRS更新と基本動作確認まで。本資料の性能調整・サービス別の入力制限は別作業とし、追加の調査・通常設定への適用は行わない。測定値と制約は参考資料として保持する。
