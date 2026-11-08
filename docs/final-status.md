# 更新結果

## 現在の状況（2026-11-09）

更新候補とTraefik Local Pluginの接続を実装。SSE 1件とFTW `920610-2`は未解決。
今回の完了条件は、CRS更新と実ルールでの基本動作確認まで（2026-11-09のユーザー指定）。この範囲は既知バグ付きで検証済み。サービス別の入力制限、本番SLA・負荷調整、配布・本番反映は別作業とする。
Coraza更新に必要な互換性修正を含め、`43fb604`で変更をコミット済み。負荷試験の失敗・性能上の懸念は参考記録に残し、成功へ読み替えない。
両リポジトリの作業ブランチは`chore/update-coraza-crs`。push、Release公開、本番反映は未実施。

運用方針: ユーザー合意により標準Traefikイメージを維持し、Traefik本体のカスタムビルドは採用しない。
ホスト修正パッチは参考資料にとどめる。SSEとraw fragmentは[既知のバグ](known-bugs.md)として管理し、失敗テストと通常CIの失敗判定は保持する。
この2件だけの同一原因の失敗は、更新差分のレビュー・コミットを停止する理由から外す。全テスト成功や公開・本番反映の承認を意味しない。

- FTW残件11件を同じfixtureで通常Coraza/WASMへ送ると、ステータスと検知IDがすべて一致した。[比較結果](ftw-native-wasm-comparison.md)。
- FTWのバックエンドを公式Corazaと同じalbedo v0.3.0へ変更。Host/multipartは公式期待値を採用。旧HTTPはignoreせず400/505拒否とWAF未到達を検証する。
- FTW全件は5,193件、skip=0、既存ignore=107、失敗`920610-2`の1件。`build/resume-ftw-full.log`に記録。
- 本文保持は検査済みprefixと未読tailを再送して復元する。前段の有限な本文上限を全WAFルートへ必須とする方式でユーザー合意済み。サンプルはTraefik bufferingをWAFより前に配置し13 MiBを指定する。実サービスに応じて設定する。
- DetectionOnly標準のProcessPartialを維持する。Coraza検査上限超過でも前段上限以内なら本文を保持して通過し、前段上限を超えれば両モードで413とする。
- hostは再構築本文全量をメモリに保持するため、上限と同時処理数を含めて負荷評価が必要。Traefikのディスク退避だけでhostメモリは制限できない。
- 外側のbufferingと空WAF拒否応答の組合せで500を再現し、Content-Lengthを0とする修正を追加。SDKの空Write無視によるレスポンス本文漏洩も回帰テストで再現し修正した。
- SSEはhostの全応答buffering/Flusher未対応が原因。ユーザーから現在は利用サービスがない可能性が高いとの回答があり、現時点の対応優先度を下げる。将来の利用時に再評価する。SSEテスト失敗は保持する。

最終WASMでビルド、unit、完全E2E、実Traefik通信、FTW全件を再実行した。新たなFTW失敗はなく、SSEとraw fragmentの残件を維持する。

## バージョン

| 項目 | 更新前 | 確定した更新 |
|---|---|---|
| Coraza | v3.2.1 | v3.8.1 |
| OWASP CRS | v4.0.0 | v4.30.0 |
| Go | 1.23.12 | 1.26.10 |
| TinyGo | 0.34.0 | 0.43.0 |
| LLVM | 18.1.2 | 22.1.4 |

Coraza/CRSは公式GitHub latest APIとGoモジュール情報から確認。

## 変更ファイル

WASM側:

- `mise.toml`、`go.mod`、`go.sum`、`go.work`: 固定ツールチェーン、依存更新、FTWモジュール分離。
- `main.go`、`operators/operators.go`: 標準Coraza演算子、severity未指定ログ、ヘッダ名正規化。
- `init_reactor.go`、`magefile.go`: reactor起動と従来HTTP WASM ABIの接続、precise GC、スタック1MiB、低並列ビルド。
- `init_tinygo.go`、`config/@coraza.conf-recommended`: 旧GCと一時保持した旧設定を削除。
- `third_party/http-wasm-guest-tinygo/`: SDK v0.4.0の長文字列・空本文書き込み修正。元コードとLICENSE、変更記録を保持。
- `patches/http-wasm-host-go-v0.7.0-request-target.patch`、`docs/host-request-target.md`: 標準ホストには未反映の入口400拒否案と隔離検証手順。
- `body_preservation_e2e_test.go`、`interruption_e2e_test.go`、`protocol_e2e_test.go`、`crs_e2e_test.go`、`testing/coreruleset/{go.mod,go.sum,ftw_test.go}`: 実CRS検証、FTW更新、albedoバックエンド、プラットフォーム別アサーション。
- `testing/load/`、`testing/bodylatency/`、`testing/argumentlimit/`、`argument_limit_e2e_test.go`: 参考となる負荷・遅延調査、通常設定へ未適用の引数制限例。
- `.github/workflows/{ci.yaml,nightly-coraza-check.yaml}`、`.github/dependabot.yml`、`README.md`、`docs/`: CI、更新PR、手順と検証記録。

Traefik側:

- `magefile.go`、`magefile_test.go`: フォークReleaseの明示バージョン・SHA256検証、ローカルWASM配置、回帰テスト。
- `mise.toml`、`go.mod`: ツールチェーン固定、フォークのモジュール名。
- `.traefik.yml`は仕様を維持。`config-static.yaml`、`config-dynamic.yaml`、`config-static.yaml.tmpl`、`docker-compose.yml`、`e2e/`をLocal Plugin/Traefik v3.6.0向けに調整。
- `.github/workflows/{ci.yaml,release.yaml}`、`scripts/{smoke,body_limits,verify_local_modes}.py`、`README.md`: ローカル検証、明示Release検証、パッケージ保存と配布手順。

## 実行結果

| 検証 | 結果 |
|---|---|
| `mage build` | 成功、4,275,520 bytes（約4.1 MiB） |
| `mage test` | 成功（Exampleを含む） |
| `mage e2e` | 実CRS 18/18、本文保持18/18、サイズ拒否・動的engine Off・空遮断4件・旧HTTP拒否3件は成功。従来HTTP 9件成功、追加SSE 1件失敗 |
| `mage ftw`全件（本文保持修正時） | 5,193件、skip 0、既存ignore 107、raw fragment 920610-2の1件失敗 |
| `mage ftw`全件（最終WASM） | 5,193件、skip 0、既存ignore 107、失敗は920610-2の1件。321秒。`build/external-limit-ftw-full.log` |
| 隔離ホストのraw fragment修正案 | 既存net/http＋追加7ケース成功、実CRS920610の2ケース成功、FTW全件対象TestFTW成功（317秒）。標準Traefikへ未反映 |
| Traefik単体テスト | 成功 |
| Traefik v3.6.0 Local Plugin E2E | HTTP 9/9、CL/chunkedの超過・境界・順序4/4成功 |
| Traefik実CRS通信 | On / DetectionOnly / DetectionOnly検査上限1024の3構成で正常・攻撃・本文保持・前段413が各10/10。小さい検査上限がOnで413となることも確認 |
| 負荷後の回帰検証 | unit成功。完全E2Eは追加1 MiB本文保持を含め成功、既知SSEだけ失敗。実Traefikの3モード各11項目成功、設定復元・コンテナ削除済み |
| ローカル負荷 | 標準バックエンド条件は502／メモリ閾値で未完走。検証用バックエンド期限30秒の混合8同時・600秒は4,348件、期待200/403と本文一致、p95 3.18秒、観測最大848.2 MiB。全工程成功や本番性能の合格を意味しない |
| YAML/Python構文・`git diff --check` | 成功 |

再開後のログ: `build/external-limit-{build,unit,full-e2e,traefik-e2e,traefik-modes}.log`、`build/interruption-{red,green}.log`。

実CRS検証は正常通信、SQLi、XSS、256引数、Request/Response Body、DetectionOnly、403、Anomaly Scoring閾値変更を含む。
既存ignore設定は変更していない。SSE/FTWの失敗結果は保持する。CIは既知失敗の非0終了を保持するため、自動のDraft Release生成は停止する。既知バグの分類・完了条件は[判定仕様](known-bugs.md)に従う。

## 成果物とLocal Pluginへの反映

成果物: `build/coraza-http-wasm.wasm`。
SHA256: `4ac4f33e131d66d568f6f5a079205f06f1deb289fe41d1a7800259a65aea4f6a`。
Traefik側`build/coraza-http-wasm.wasm`と一致を確認した。

```sh
cd ../coraza-http-wasm-traefik
WASM_SOURCE=../coraza-http-wasm/build/coraza-http-wasm.wasm mise exec -- go run mage.go prepareLocal
docker compose -p coraza-local up -d traefik
python3 scripts/verify_local_modes.py
docker compose -p coraza-local down
```

Local Pluginディレクトリは`/plugins-local/src/github.com/onodai145/coraza-http-wasm-traefik`。
マニフェスト、WASM、SOURCE、SHA256SUMSを配布する。公開Plugin Catalogは不要。

## 未解決事項と本番反映前の作業

- SSE: HTTP WASM host v0.7.0の全レスポンスバッファリングとFlusher未対応。guest側のみで逐次配信と現行本文遮断を両立する解決は未確認。
- FTW `920610-2`のホスト入口400拒否案は[検証記録](host-request-target.md)を参照。標準Traefikには未反映。
- FTW `920610-2`: raw `#`がGo HTTP層からguestへ渡るときに`%23`へエスケープされ、検知されない。通常Corazaでも同じ結果。正当なエンコード済み`%23`との区別に元URIが必要で、既存guest APIでは解決未確認。期待値は緩めていない。
- [ローカル負荷試験](load-validation.md)で推奨メモリ上限128 KiBとno_fs_accessの不整合による500を発見し、サンプルのメモリ上限を検査上限12.5 MiBへ合わせた。単一1 MiB引数は約63〜65秒。標準backend期限1秒では8同時混合の502、メモリ予算の問題も発見した。検証用バックエンド期限30秒では600秒を完走したが、標準条件の修正完了とは判定しない。実バックエンド期限・許容遅延・引数長・CPU予算を決めた再評価は本番導入時の別作業とする。今回の基本動作確認の完了条件には含めない。これらを既知2件へ含めない。
- SDKローカル修正とreactor ABIアダプターは、将来の上流更新時にも回帰検証が必要。
- 2026-11-08の[本文遅延調査](body-latency.md)では、約60秒の主因を長大な引数に対するCRS正規表現の評価コストへ特定した。計測なしの同一runtimeでプリフィルタOffは1 MiBで57.48秒、実験的Onでも28.32秒。通常設定・WASMは変更せず、アプリ要件に応じた上限や別の実行方式の評価が必要。
- [引数の早期拒否例](argument-limits.md)を任意設定として検証。試験値は名前／値16 KiB、GET/POST引数合計64 KiB。Onで実Traefikの1 MiBフォーム約29 ms／JSON約17 msで413、実CRS＋WASM27ケースと実Traefik13要求が成功。DetectionOnlyにはCPU保護が働かず、通常設定への適用・実サービス向けの値の決定は未実施。完全E2Eは既知SSEのみ失敗。
- 基本動作の更新差分はレビュー・コミット済み。サービス別の性能調整・引数制限例の採用は別作業とする。公開する場合は別途ユーザー承認を得てタグをpushし、Draft Releaseを確認して手動公開する。
- 検証済みVERSION/SHA256でプラグインを取得し、本番設定と旧WASMをバックアップしてから承認を得て配置・再起動する。問題時は旧設定と旧WASMへ戻す。

調査根拠、失敗原因、再現コマンドとログの場所は[詳細記録](update-validation.md)を参照。

## 作成済みコミット

WASM側:

- `79bb955` chore: pin working Go and TinyGo toolchain
- `3fb0393` feat: embed OWASP CRS 4.30.0 and verify real CRS behavior
- `43fb604` chore: update Coraza and validate CRS integration

Coraza更新とCI/SDK/本文保持・空遮断応答修正は`43fb604`へコミット済み。追加の調査・設定例は参考資料として含め、通常の引数制限は変更していない。

Traefik側:

- `3d0c76d` feat: support verified fork WASM as a Traefik local plugin
- `c7f0a5d` fix: bound Traefik requests before WASM inspection
- `7032e1a` fix: align Coraza request body memory limits for WASM

Traefik側も本文メモリ上限の整合、1 MiB smokeとREADME追記までコミット済み。GitHub ActionsはYAML構文と相当するローカル手順を検証したが、リモート実行は未実施。
今回の成果物を公開Releaseから取得する経路も未検証（公開操作は実施していない）。
