# 引数を重いCRS phase 2評価前に拒否する設定候補（2026-11-08）

**Onモードで、長大な引数をHTTP413で早期拒否する任意設定例を検証した。**
標準Traefik v3.6.0で、単一1 MiBフォームを28.9 ms、JSONを16.6 msで拒否した。正常要求と攻撃遮断、ファイル転送を維持した。
上限内の要求の処理時間は従来通りで、DetectionOnlyではこの拒否が働かない。通常設定や本番への適用は行っていない。

これは[約60秒の遅延調査](body-latency.md)を受けた、制限方法の検証である。試験値がアプリ要件を満たすかは未確認。
Coraza v3.8.1、CRS v4.30.0、候補WASMのSHA256 `4ac4f33e131d66d568f6f5a079205f06f1deb289fe41d1a7800259a65aea4f6a`は変更していない。

## 選んだ方法と条件

CRS標準の`tx.arg_length` / 920370は、現在のanomaly scoring設定では加点して後続のphase 2ルールを実行し、最後の949110で拒否する。
重いルールへ進む前の拒否を調べるため、Coraza標準のSecRule・`t:length`・`deny`を使う小さなルール例をCRSより前へ配置した。
エンジンの独自実装、CRSの除外、プリフィルタOnへの変更は行っていない。

試験値:

| 対象 | 上限 | 判定 |
|---|---:|---|
| 解析後の引数の名前または値1つ | 16384 bytes（16 KiB） | 超過時413 |
| GET/POST引数の名前＋値の合計 | 65536 bytes（64 KiB） | 超過時413 |

`t:length`はUTF-8を含めbytesで測る。フォームのpercent decode後、JSONのescape解析後の値を確認する。
JSONの引数名にはCorazaの`json.`等の内部path名も含まれるため、raw JSONの文字数と合計値は一致しない。
GET＋POST合計の例であり、ファイルデータ、XML/raw body、cookie、path引数全体の総量を制限する意味ではない。
CRSのphase 1は既に実行済みで、後続のCRS phase 2ルールを止める構成である。本文全体も先に読み込んで解析するので、WAF前の有限な本文上限、Corazaの本文／メモリ上限の整合は引き続き必要。

実ルールは[early-arguments.conf](../testing/argumentlimit/early-arguments.conf)に置いた。ID100101/100102は導入先の独自ルールと衝突しないことを確認する。

## 実CRS＋WASMの検証

`argument_limit_e2e_test.go`が実際のCRSを読み込む。設定候補はテストにだけ渡し、既存の通常設定には追加していない。
最初にルール未追加で、16 KiB+1引数が期待413に対して200となる失敗を確認した（約888 ms）。
ルール追加後は同じケースが413・バックエンド未到達となり、後続のCRS phase 2への到達markerも実行されないことを確認した（初回約3 ms）。

27ケース（On=21、DetectionOnly=6）が成功。

- 16 KiBの値は通過、1 byte超過は413。UTF-8 bytes、percent encode、JSON escape、JSON array、長い引数名も検証。
- 引数名＋値の合計64 KiBは通過、1 byte超過は413。GET＋POST合計と複数JSON値も確認。
- Onで単一1 MiBフォーム27.2 ms、JSON70.6 msで413（in-process単発計測）。
- 通過時はバックエンドへ元の本文全bytesを保持。OnのSQLi/XSS/JSON XSSは実CRSの942/941と949110で403。
- 1 MiBファイルのmultipart転送は200・本文完全一致。ファイル内容の検査能力を示すテストではない。
- DetectionOnlyでは制限に一致しても200・本文保持・CRS到達。16 KiB+1フォーム約0.89秒、合計64 KiB+1の約3.52秒は引き続き評価コストが発生する。

時間を固定した合否条件にはせず、期待HTTP状態・backend到達・本文・検知ID・後続のCRS phase 2への到達順をassertする。
ログ: `build/load-validation/argument-limit-{red,green-initial,matrix}.log`。

## 標準Traefikでの検証

公式Traefik v3.6.0、専用Compose、2 CPU / 2 GiB・swapなし、GOMAXPROCS=2、GOMEMLIMIT=768MiB。
前日のバックエンド1秒期限の問題と分けるため、検証用httpbinのヘッダ期限30秒を使用した。本番への期限推奨ではない。
任意設定例を生成した設定へ注入し、通常の`config-dynamic.yaml`は編集していない。

| 条件 | 結果 | 所要時間（単発） |
|---|---|---:|
| On、通常小本文 | 200・本文一致 | 23.4 ms |
| On、値16 KiB | 200・本文一致 | 908.7 ms |
| On、値16 KiB+1 | 413 | 3.45 ms |
| On、1 MiBフォーム | 413 | 28.9 ms |
| On、1 MiB JSON | 413 | 16.6 ms |
| On、引数合計超過 | 413 | 5.20 ms |
| On、1 MiB multipart | 200・本文一致 | 87.6 ms |
| On、SQLi/XSS | 403 | 8.52 / 9.62 ms |
| DetectionOnly、16 KiB+1フォーム／JSON | 200・本文一致 | 923 / 828 ms |
| DetectionOnly、SQLi/XSS | 200 | 9.44 / 9.78 ms |

13要求を完了し、Onへ戻して終了。summaryはこの直列診断のpassedであり、標準条件の負荷試験全工程の合格を意味しない。
WAF観測最大580.1 MiB。生成物: `build/load-validation/20261108-162352/`、実行ログ`build/load-validation/argument-limit-traefik.log`。
診断scriptは`build/load-validation/argument_limit_traefik.py`。前日の単発測定harnessを使い、本文の長さ・SHA256を確認した。

## 採用時の配置例（未適用）

必要な値が決まったら、pluginの`directives`で`@crs-setup.conf.example`の後、`@owasp_crs/**.conf`の前へ渡す。
WASMの外に置いた任意のconfファイルをIncludeする構成は採っていない。下記は文字列として渡す例。

```yaml
directives:
  - Include @coraza.conf-recommended
  - SecRequestBodyLimit 13107200
  - SecRequestBodyInMemoryLimit 13107200
  - SecRuleEngine On
  - SecAuditEngine Off
  - Include @crs-setup.conf.example
  - |
    SecRule ARGS_COMBINED_SIZE "@gt 65536" \
        "id:100102,phase:2,t:none,deny,status:413,log,msg:'Total argument limit exceeded'"
    SecRule ARGS|ARGS_NAMES "@gt 16384" \
        "id:100101,phase:2,t:none,t:length,deny,status:413,log,msg:'Single argument limit exceeded'"
  - Include @owasp_crs/**.conf
```

これに加えて、既存の前段bufferingを全WAFルートで維持する。例の13 MiB本文上限は、上記の引数長／合計とは別の制限である。

## 残る判断

- アプリが受け付けるフォーム／JSONの最大値、UTF-8 bytes、引数数、引数合計を確認して上限を決める。現在の16/64 KiBは試験値。
- 大きな正当な引数を受け付ける場合、この制限例ではその要求が拒否される。必要な入力を通したまま高速化する検討は別途必要。
- **DetectionOnlyではこの例によるCPU保護は働かない。** 実測も通過・後続評価となった。モードを強制変更していない。
- 本文解析・メモリ・同時数の費用は残る。例の採用だけでDoS耐性や本番SLAを保証しない。
- 通常のサンプル設定、リリース成果物、WASM、Traefik本体は未変更。採用・本番反映は未実施。

根拠: [Corazaのdisruptive actionsとDetectionOnly](https://www.coraza.io/docs/seclang/actions/)、[CRS v4.30.0の920370](https://github.com/coreruleset/coreruleset/blob/v4.30.0/rules/REQUEST-920-PROTOCOL-ENFORCEMENT.conf)、[Coraza v3.8.1 SizeCollection](https://github.com/corazawaf/coraza/blob/v3.8.1/internal/collections/sized.go)。

## 最終確認

単体テスト成功。完全E2Eは119.631秒で終了し、新規27ケースを含め成功、失敗は既知のSSE（`TestE2E: no event received; not streamed`）のみ。非0終了を保持した。
ログ: `build/load-validation/argument-limit-{unit,full-e2e}.log`。FTW全件は今回再実行しておらず、既知920610-2は未解決。
13要求の期待HTTP状態・設定の配置順序・OOM/再起動なし・資源閾値・取得/cleanupエラーなし・WASM同一を独立確認し、runの`independent-audit.json`へ保存した。専用コンテナ／ネットワークの削除も確認した。
レビューでCritical/Importantの指摘なし。保証範囲が後続CRS phase 2であることを明記した。
YAML例のparseと`git diff --check`は成功。検証時は未コミットで、push・Release公開・本番変更は行っていない。

## 現在の扱い（2026-11-09）

ユーザー指定により、今回の目標はCRS更新と基本動作確認まで。本資料の性能調整・サービス別の入力制限は別作業とし、追加の調査・通常設定への適用は行わない。測定値と制約は参考資料として保持する。

参考資料と設定例は更新コミット`43fb604`へ含めた。通常設定への適用は行っていない。
