# FTW残件の通常Coraza/WASM比較

同じ公式CRS v4.30.0 fixtureをgo-ftw v1.3.0自身のシリアライザで生HTTPへ変換し、Coraza v3.8.1通常HTTPコネクタとWASMへ送信した。レスポンスステータスと検知IDは11件すべて一致。診断はDetectionOnly、PL4で実施。

| Case | Native | WASM | WAF到達 | 検知ID |
|---|---:|---:|---|---|
| 920274-1 | 200 | 200 | True | 920271, 920272, 920274, 949110, 980170 |
| 920280-3 | 505 | 505 | False |  |
| 920430-3 | 505 | 505 | False |  |
| 920430-5 | 400 | 400 | False |  |
| 920430-9 | 505 | 505 | False |  |
| 920610-2 | 404 | 404 | True |  |
| 920640-4 | 405 | 405 | True | 920273, 920470, 933161, 942150, 942160, 942190, 942340, 942370, 942410, 942430, 942431, 942432, 949110, 980170 |
| 920640-5 | 405 | 405 | True | 920180, 980170 |
| 922130-1 | 400 | 400 | True | 200002, 200003 |
| 922130-2 | 400 | 400 | True | 200002, 200003 |
| 922130-7 | 400 | 400 | True | 200002, 200003 |

詳細の生ログ: `build/ftw-native-wasm-comparison.jsonl`。診断バックエンドは両方とも同じhttpbin。正式FTWハーネスは、上流Corazaと同じalbedo v0.3.0へ更新した。

## 上流との照合

- https://github.com/corazawaf/coraza/blob/v3.8.1/testing/coreruleset/coreruleset_test.go
- https://github.com/corazawaf/coraza/blob/v3.8.1/testing/coreruleset/.ftw-overrides.yml
- https://github.com/corazawaf/coraza/blob/v3.8.1/testing/coreruleset/.ftw.yml

Host/multipartは上流Corazaの期待値を採用する。旧HTTP入力は上流のignoreを追加せず、400/505での拒否とWAF未到達を回帰テストで検証する。raw fragmentは検知・拒否を満たしていないため、期待値を緩めず未解決として残す。
