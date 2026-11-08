# ローカル負荷試験（2026-11-07）

標準バックエンドでの全工程は未完走。ヘッダ読取期限30秒の検証用バックエンドでは、8同時・600秒の混合負荷を完走した。本番性能の合格判定は保留する。
Traefikは公式v3.6.0のままで、カスタムビルドは行っていない。

WASM SHA256: `4ac4f33e131d66d568f6f5a079205f06f1deb289fe41d1a7800259a65aea4f6a`。
今回の負荷試験中にWASMは変更していない。SSEとFTW `920610-2`は[既知のバグ](known-bugs.md)として別管理する。今回の500・502・資源閾値停止をこの2件へ含めない。

## 条件

- 専用Docker Compose project、localhostの18081（WAF）、18082（比較用WAFなし）、18083（heap診断）。生成した設定を別マウントする。
- 両Traefikは各2 CPU / 2 GiB、バックエンドは1 CPU / 512 MiB。swapなし、Dockerログは5 MiB×2ファイル／コンテナ。
- 最終条件はTraefikのGOMAXPROCS=2、GOMEMLIMIT=768MiB、バックエンドのGOMAXPROCS=1。GOMEMLIMITはGoのsoft目標であり、WASMを含む全メモリのhard limitではない。
- 実CRS、SecRuleEngine On。混合負荷は256 bytes / 16 KiBの正常フォームPOST、SQLi、XSS。正常200・攻撃403を確認し、POSTのバックエンド受信値の長さ・SHA256を照合する。
- 各worker最大2 req/s。同時数1→4→8の段階試験と、8同時・600秒＋30秒無負荷観測。遅い応答では発行率も下がるため、最大スループットの測定ではない。
- メモリを約4秒間隔で観測。コンテナメモリ85%、host available memory 4 GiB未満、disk free 10 GiB未満、想定外HTTP状態・本文不一致で失敗終了する。
- abort時は当該projectの専用container IDだけを5秒猶予で停止する。ログ・inspectを保存し、finallyでprojectを削除する。

共有開発環境での限定測定である。本番SLA、最大能力、旧WASMとの性能同等性、長期のメモリリーク有無は確認できていない。観測間の瞬間的ピークも保証しない。

## 結果

| 条件・項目 | 結果 |
|---|---|
| 標準go-httpbin v2.9.0、通常POST、同時1/4/8 | 成功。最終の同時8は235件、15.22 req/s、p50 25.3 ms、p95 317 ms |
| 同じ試行のWAFなし、同時8 | 240件、16.00 req/s、p50 0.69 ms、p95 1.07 ms。上限付きの短時間比較 |
| 標準バックエンド、混合、同時1/4 | 成功。最終の同時4は178件、5.80 req/s、p95 1.10秒 |
| 標準バックエンド、混合、同時8 | 502が発生し停止。GOMAXPROCS/GOMEMLIMIT指定、接続再利用停止だけでは解消しない |
| メモリ目標なし、補完試験後の混合負荷 | 約266秒・1,508件で観測1,773.6 MiBが85%閾値を超えて停止。OOMKilled=false |
| 検証用バックエンド、ヘッダ期限1秒 | 502再現。接続がActiveへ進む前に約1.000秒で切断 |
| 検証用バックエンド、ヘッダ期限30秒、混合8同時 | **600秒完走**。4,348件（200=2,174、403=2,174）、本文一致、7.23 req/s |
| 同30秒条件の遅延 | p50 475 ms、p95 3,184 ms、p99 3,919 ms、最大7,323 ms |
| 同30秒条件の資源 | WAF観測最大848.2 MiB、無負荷観測末尾777.6 MiB。CPU観測最大199.73%。OOM・再起動なし |
| 大容量・上限の個別確認 | 1 MiB単一フォーム200・本文一致、1/12 MiB multipart200・本文一致、13 MiB multipartはOn413／DetectionOnly200・本文一致、前段超過CL/chunkedは413 |

30秒条件は混合負荷だけを行った独立試行で、大容量・モード切替を含む全工程の成功を意味しない。
CPUは割当て2コア分へ達しており、十分な余裕があるとはいえない。1 MiB単一フォーム引数は約63〜65秒を要した。小さなPOSTやmultipartの結果から、長大な引数の性能を判断できない。

30秒条件の159サンプル／コンテナを独立確認した。最大間隔4.03秒、閾値超過なし、終了前inspectは全コンテナrunning・OOMKilled=false・RestartCount=0。summaryのreasonsは空、取得・cleanupエラーなし、ログとheap記録を保存済み。原summaryは変更せず、確認結果を`independent-audit.json`へ保存した。
WAFメモリの2分区間平均は順に787.8 / 811.5 / 780.8 / 777.9 / 778.2 MiB。10分間に単調増加は見られなかったが、リークがないとの証明にはならない。
試験用コンテナ・ネットワークの削除を確認した。

## 発見した本文メモリ設定の不整合と修正

最初の1 MiBフォームはHTTP500となった。ログは`Failed to read request body ... memoryLimit reached while writing`。
Coraza推奨設定の検査上限は12.5 MiB、メモリ上限は128 KiBだが、TinyGo / no_fs_accessではディスク退避できず、BodyBufferは両上限が同じことを前提としている。

Traefikサンプルへ次を追加し、両READMEへ必要条件を記載した。

```yaml
- Include @coraza.conf-recommended
- SecRequestBodyLimit 13107200
- SecRequestBodyInMemoryLimit 13107200
```

実CRS＋WASMの`TestE2ERealCRSLargeBody`と、実Traefikのsmokeへ1 MiB本文保持検証を追加した。
最終E2Eの同ケースは64.29秒で成功。実TraefikもOn、DetectionOnly、検査上限1024のDetectionOnlyの3構成で、1 MiBを含む本文保持・正常／攻撃・前段413が各11項目成功した。小さい検査上限でOn413となることも確認した。モード検証後は設定を復元済み。
検査可能になったことと、応答時間が本番要件を満たすことは区別する。

根拠: [Coraza BodyBuffer v3.8.1](https://github.com/corazawaf/coraza/blob/v3.8.1/internal/corazawaf/body_buffer.go)、[推奨設定](https://github.com/corazawaf/coraza/blob/v3.8.1/coraza.conf-recommended)。

## 502の切分け

標準go-httpbin v2.9.0の公式ソースはReadHeaderTimeout=1秒、ReadTimeout=5秒。
DEBUG/access logでは最初の502が上流EOF、OriginStatus=0、OriginDuration約1.2〜2.3秒であることを確認した。停止後のconnection refused等は最初のEOFと分けて扱う。
接続再利用を停止しても再現したため、再利用だけを原因としない。パケット取得は権限不足で実施できていない。

同じhttpbin v2.9.0ライブラリを使う試験用サーバをGo 1.26.10でビルドし、ConnStateを記録した。
同一binaryでヘッダ期限だけを1秒／30秒へ変えた。両条件ともRead/Write timeout120秒、IdleTimeout60秒、最大本文15 MiB。
標準イメージとはGoの版や他の期限設定も異なるため、標準イメージとの直接比較には限界がある。

1秒対照では、接続`172.20.0.4:56352`がUTC09:08:47.835641にNew、09:08:48.835842にClosedとなり、Activeへ進んでいなかった。この対照ではハンドラ到達前のヘッダ期限切れを確認できた。
同一binaryの期限30秒条件は600秒を完走した。ただし標準イメージの502が修正されたとの判定や、本番への一律30秒推奨は行わない。

試験用バックエンドSHA256: `ff0eaa37a640e6694b14b5fd83363aa5cc85576f6616a586680c9aa183c0d259`。
根拠: [go-httpbin v2.9.0 server設定](https://github.com/mccutchen/go-httpbin/blob/v2.9.0/httpbin/cmd/cmd.go)。

## 試験ツールと証跡

再実行手順は[負荷試験README](../testing/load/README.md)。標準条件は`python3 testing/load/run.py`。
各runにsummary.json、results.json、resources.jsonl、containers-*.json、services.logを保存する。生成物は`build/`配下でGit管理外。

| run | 内容 |
|---|---|
| `20261107-125026` | 本文メモリ設定による500 |
| `20261107-172436` | 数値IPのHost等によるCRS403。正当なHost/Acceptへクライアントを修正。ルールは変更しない |
| `20261107-172643` | 段階試験成功後、multipartの応答照合処理で失敗 |
| `20261107-173256` | multipart・上限検証成功後、メモリ閾値で停止 |
| `20261107-174010` | GOMEMLIMIT指定のみでも502 |
| `20261107-175034` | GOMAXPROCSも指定、同時8で502 |
| `20261107-175857` / `20261107-180033` | DEBUGでEOF、接続再利用停止でも再現 |
| `20261107-180258` | パケット取得権限なし、取得データなし |
| `20261107-180837` | 対照サーバ1秒期限で502・ConnState確認 |
| `20261107-181041` | 対照サーバ30秒、600秒完走。独立確認結果も保存 |

multipart応答はgo-httpbin v2.9.0ではfiles=null・data=生のmultipart全文だった。最初のfiles参照失敗を保存し、dataの長さ・SHA256照合へ修正した。本文照合の省略はしていない。
監視終了時の異常通知やログ取得失敗が成功判定に紛れる競合も修正し、終了判定の6回帰テストが成功。30秒対照試行は旧finalizerだったため、前述の独立確認を行った。

負荷後の`mage test`は成功、`mage e2e`は追加1 MiB検証を含め成功し、既知のSSE1件だけが失敗した。FTWはWASMを変更していないため今回再実行していない。既存の`920610-2`失敗は保持する。
ログ: `build/load-validation/final-{unit,e2e,functional-modes,functional-services,functional-stop}.log`。

## 本番反映前に必要な判断

- 実バックエンドのヘッダ／本文／応答期限、入口タイムアウト、許容遅延、通常の本文・引数長を確認する。今回の30秒条件をそのまま採用しない。
- 単一1 MiB引数の約64秒が許容されるか評価する。アプリ要件に合う本文・引数上限とCPU・同時数を決め、その実設定で負荷を再試験する。
- GOMEMLIMITとGOMAXPROCSを採用する場合も、コンテナ全体のhard limit・入口本文上限・同時数を併せて評価する。今回の10分は長期安定性を保証しない。
- 標準fixtureの502とメモリ停止は未完走として保持する。現在の設定で全負荷試験成功とは扱わない。本番向けの性能・配布判断は別作業として残す。

今回の作業ではpush、Release公開、本番への配置は行っていない。

## 2026-11-08: 長大引数の遅延原因を調査

同じWASMと標準Traefikで単一フォーム引数を段階測定し、1 MiBで59.47秒を再現した。
Engine Offは0.667秒、本文解析のみは0.682秒。WASM内の関数計測では64 KiB要求の約95.7%がGo regexpの評価だった。
16 KiBのルール別時間はRCE/XSS/SQLi等の多数へ分散し、最大の934100でも全体の約8.6%。
同じ通常runtimeのin-process比較では、プリフィルタOffの1 MiBが57.48秒、実験的Onでも28.32秒で、問題は残る。通常設定はOffのまま。
原因・測定条件・再現手順は[本文遅延の調査](body-latency.md)を参照。性能修正や本番設定の変更は行っていない。

## 現在の扱い（2026-11-09）

ユーザー指定により、今回の完了条件はCRS更新と基本動作確認までとする。サービス別の性能調整・引数制限は別作業。負荷試験の未完走や遅延はそのまま記録し、基本動作更新のコミットを止める前提から外す。全負荷試験成功・本番性能合格・公開承認を意味しない。
