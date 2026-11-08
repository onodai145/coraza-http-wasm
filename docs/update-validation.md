# 更新・検証記録

## 実装手順

1. `mise.toml`だけでGo 1.23.12 / TinyGo 0.34.0を固定し、mainに初回コミット。
2. 両リポジトリの`chore/update-coraza-crs`で更新。
3. CRS v4.30.0を分割モジュールへ移行。Coraza v3.2.1の公式推奨設定を一時保持してCRS単独更新を検証。
4. Coraza v3.8.1と必要なWASI依存を更新し、互換なmiseツールチェーンを固定。HTTP WASM APIは維持。
5. 実CRS E2E・FTWを実行し、失敗は原因とともに記録。
6. フォーク成果物を明示バージョンで取得するLocal Plugin構成、CIとリリース手順を整備。

push、タグのリモート送信、Release公開、本番反映は行わない。

## 公式情報（2026-11-07確認）

- Coraza最新安定版: v3.8.1。GitHub latest APIとGo proxyの`@latest`が一致。最低Go 1.25.0。
  https://github.com/corazawaf/coraza/releases/tag/v3.8.1
- OWASP CRS最新安定版: v4.30.0。同様にGitHub latest APIとGoモジュールで確認。
  https://github.com/coreruleset/coreruleset/releases/tag/v4.30.0
- 旧`coraza-coreruleset/v4`はv4.25.0で凍結。`crs/v4`、`coraza/v3`、`crs/tests/v4`へ分割。CRS v4.30.0モジュールはGo 1.23以上。
  https://github.com/corazawaf/coraza-coreruleset#modules
- `coraza-wasilibs v0.3.0`はGo 1.26.0以上、Coraza v3.8.1を要求。
  https://github.com/corazawaf/coraza-wasilibs/blob/v0.3.0/go.mod
- TinyGo v0.43.0の対応Goは1.25–1.27。`scheduler=none`はgoroutineを許さず、タイマー登録もpanicする。
  https://github.com/tinygo-org/tinygo/blob/v0.43.0/builder/config.go
  https://github.com/tinygo-org/tinygo/blob/v0.43.0/src/runtime/scheduler_none.go

## CRS単独更新時の互換性

最新推奨設定v3.8.1は`SecRxPreFilter`を含み、Coraza v3.2.1は`unknown directive "secrxprefilter"`で初期化失敗。
CRS単独更新の検証では旧推奨設定をそのまま保持する。ルールを無効化して回避しない。

CRS単独更新: Go 1.23.12 / TinyGo 0.34.0 / Coraza v3.2.1のまま、ビルド、unit、既存E2E 9件、実CRS E2E 15件は成功。
FTWは新形式対応のv1.3.0へ移行。既存の壊れたMIME指定と既定アクション重複を修正。
FTWの全件実行はOOMによって中断。成功扱いにしない。FTW用モジュールはGOWORK=offで独立実行し、WASM依存の意図しない引き上げを防ぐ。

## 最新Corazaのツールチェーン調査

Go 1.26.10 / TinyGo 0.43.0 / Coraza v3.8.1 / wasilibs v0.3.0で再現:

- `-scheduler=none`: `time/sleep.go:215: attempted to start a goroutine without a scheduler`。
- `-scheduler=tasks`: wasm向けtask stack実装がなく`calleeSavedRegs`等が未定義。
- `-scheduler=asyncify`: WASM用スケジューラとして検証。旧C静的ライブラリとのリンクで`toupper`、`isupper_l`等が未定義になったため、RE2/libinjectionのみ対応する公式最新版へ更新して検証。
- OOM対策: TinyGo `-p=2`、mise環境`GOMAXPROCS=2` / `GOMEMLIMIT=768MiB`。Goヒープの目標値であり、WASMゲストやLLVMを含む全プロセスのメモリ上限ではない。

再現コマンドは`mise exec -- go run mage.go build`。変更したビルドオプションは依存更新に伴う互換性対応に限定。

## Traefik配置

- 対象: Traefik v3.6.0、Local Plugin。モジュール名は`github.com/onodai145/coraza-http-wasm-traefik`。
- `.traefik.yml`は既存のWASMマニフェスト仕様を維持。
- ローカルビルドは`WASM_SOURCE`、公開済みフォークReleaseは明示`VERSION`と`WASM_SHA256`で選択する。
- SHA256の検証が完了してからWASMを置換。単体テストで不一致時の既存成果物保持を確認する。
- CIのRemote Pluginテスト依存を取り除き、選択したReleaseを使うLocal Plugin E2Eを手動workflowへ移行。ローカルでも同じE2Eを実行する。
- pluginの旧Release workflowによるmainへの自動コミット・タグ作成は廃止。パッケージ保存に限定する。

RE2 v1.13.0 / libinjection v0.7.0でRE2側の未解決シンボルは解消。
残った`nottinygc v0.7.1`の`libmimalloc.a: undefined symbol: toupper`を解消するため、TinyGo標準の`-gc=precise`へ移行。`custommalloc`タグとnottinygc初期化を取り除いた。
ビルド設定は`-scheduler=asyncify -gc=precise -p=2`。HTTP WASMの関数シグネチャは変更しない。
GCとスケジューラが変わるため、旧バイナリとのレイテンシ・スループット・長時間メモリ使用量の同等性は別途負荷試験が必要。

RE2 v1.13.0は`tinygo.wasm`直接バックエンドが廃止され、生成Goバックエンドになる。
この組合せはExampleMainが403を返さず500、実CRS初期化のホストメモリが約3.7GiBに増えたため停止。
公式タグのソースを比較し、直接バックエンドを維持するv1.11.0へ固定する。v1.12.0以降は採用しない。
これはRE2のバージョン追従に必要な互換性制約であり、Dependabot PRも実WASMテストによる確認が必要。

## 採用する演算子構成

v1.11.0はTinyGo build tagが残るものの静的ライブラリのリンク設定がなく、`env.cre2_*`インポートが残るため、ホスト側で`module[env] not instantiated`となった。
外部C/C++ ABIに依存するWASI演算子の置換登録を停止し、Coraza本体が提供する標準演算子を採用する。
SQLi/XSS/正規表現/フレーズ照合はいずれもCorazaの標準実装であり、このフォークでルールエンジンを独自実装しない。
`coraza-wasilibs`とそのC/C++ライブラリは依存から除去する。Go1.26.10 / TinyGo0.43.0と標準precise GC / asyncifyは維持。
元のWASI演算子との性能差は本番前に負荷試験する。中間段階のRE2 v1.11.0固定は最終構成には含めない。

HTTP500の詳細調査では、TinyGo通常WASIコマンドの`wasmEntryCommand`がmain終了後に`mainReturnExit`を呼び、HTTP WASMホストが継続利用するインスタンスを終了させることを確認した。
公式`-buildmode=wasi-legacy`の`wasmEntryLegacy`はmain終了後もモジュールを維持するため、この互換モードを使う。
https://github.com/tinygo-org/tinygo/blob/v0.43.0/src/runtime/runtime_wasmentry.go

実CRSのGo正規表現コンパイラは既定64KiBのスタックでメモリアクセス違反を起こしたため`-stack-size=1MB`を設定して検証する。
旧カスタムGC用の初期メモリ1050ページへの後処理は標準GCでは使わず、TinyGo出力をそのまま配布する。

## Reactor互換アダプター

`wasi-legacy`はscheduler=none専用であり、現依存のgoroutine要件とは両立しない。
そのため`-buildmode=c-shared -scheduler=asyncify`を使い、次のABI適合処理を行う。

- `init_reactor.go`のinitで従来mainのWAF初期化を呼ぶ。通常のGoテストではこのファイルは除外。
- 既存guestライブラリの関数にスケジューラ対応`//go:wasmexport`ラッパーを追加する。ライブラリはv0.4.0に固定。
- WASM出力の`_initialize`を`_start`へ、ラッパーを`handle_request` / `handle_response`へ改名する。古い非スケジューラexportは除去。
- メモリサイズや関数シグネチャは変更しない。HTTP WASMホストは従来と同じ3つのexportを使う。

この適合処理はルールエンジンの変更ではない。実際のWASMを使うunit / E2E / Traefikで検証する。

## 確認できたHTTP互換性と未解決のSSE

reactorアダプターでunit/Exampleは成功、従来HTTP E2Eの9ケースも成功。
Coraza v3.8.1のE2EランナーはSSEを10件目として追加している。この検証は`no event received; not streamed`で失敗。
HTTP WASM host v0.7.0はレスポンス全体をバッファリングし、bufferingResponseWriterがhttp.Flusherを実装していない。
HTTP WASM guest APIにはレスポンスヘッダ到着時にバッファリングを切り替える仕組みもない。
このリポジトリのguestだけでSSEの逐次配信と現行のレスポンス本文遮断を両立させる解決は確認できていない。
SSEテストは除去・スキップしない。完全E2EおよびCIはこの失敗を返し、Releaseジョブもブロックする。
再現: `mise exec -- go run mage.go e2e`。SSE利用にはホスト側の対応、または運用でWAF対象ルートを分ける設計検討が必要。

最新Corazaではseverity未指定のルールが独立値となるため、errorCbで既知severityだけを扱うとCRSの949110/959100ログが落ちる。
defaultでWarnへ出す修正を入れ、検知IDとAnomaly Scoring遮断IDを実WASMテストで確認する。

## 長いURIの不具合と修正

FTW 920380-1のタイムアウトを調査すると、256引数のURIでHTTP500、ErrorLogのfmt処理中にruntime.allocがfatalになった。
SDK v0.4.0の`handler/internal/mem/mem.go`ではReadBufLimitを超える文字列について、バッファ先頭をstringヘッダとして誤って解釈していた。
`third_party/http-wasm-guest-tinygo`へ元のhandlerとライセンスを保持し、当該1行を`string(buf)`へ修正。go.modのreplaceで適用する。
長いURIを実CRS E2Eへ加え、On / DetectionOnly / 高いAnomaly閾値の3モードで検証した。
修正後は18/18成功し、256引数ケースの処理は約0.3秒（従来約29秒後に500）となった。

現時点の最終WASM: build成功、unit/Example成功、実CRS 18/18成功、従来HTTP E2E 9/9成功。
完全E2Eは追加SSE 1件で失敗を維持。FTW全件は修正後に再実行する。

## FTW全件結果（SDK修正後）

5,193件を4分44秒で最後まで実行。skip=0、既存設定によるignore=107、失敗=12（新規ignore追加なし）。
失敗ID: 920274-1, 920280-3, 920430-3, 920430-5, 920430-9, 920610-2,
920640-4, 920640-5, 922130-1, 922130-2, 922130-7, 932207-7。
ログ: `build/final-ftw.log`。WASM側の検知ログは`testing/coreruleset/build/ftw-error.log`。

- 920274/920280/920430/920610/920640: 不正Host、旧HTTPバージョン、raw fragment、Content-Type/Content-Lengthの入力。HTTPステータス等の期待値を満たさない。Go HTTP層とFTWの期待値の差を含むが、全件の厳密な原因分離は未完了。
- 922130: 不正multipartヘッダ。Coraza側は200002で本文解析エラーを記録するが、FTWの期待する922130/ModSecurity形式のログと一致しない。
- 932207-7: 検知ID932207および内容は一致するが、HTTP WASMホストが返す`referer`と期待値`Referer`の表記が異なる。AddRequestHeader/AddResponseHeaderでGo標準CanonicalHeaderKeyを用いる修正を追加し、再検証する。

Traefik v3.6.0 Local Plugin: 既存HTTP E2E 9/9成功。実CRS設定で正常200、SQLi/XSS/リクエスト本文/レスポンス本文は各403を確認。

## 前回の再検証（本文保持修正前）

ヘッダ正規化を含む最終WASMでunit成功、実CRS 18/18成功、従来HTTP 9/9成功、SSE失敗を再確認。
FTWの既知失敗12件を再実行し、932207-7は成功へ改善。残る11件は同じ失敗を再現（`build/ftw-recheck-final.log`）。
全件実行で新しいignoreは追加していない。残件はステータス/プロトコル/本文解析ログの互換性に関わり、未解決として保持する。

当時のWASM SHA256: ccea44ef841c913301180fd571ecb1f02dc3bcc1d59660b5ee207601ac5222fa
Coraza本体の更新は完全テスト成功の条件を満たさないため未コミットの作業ツリーとして保持する。
SSEとFTW残件を解消・評価し、負荷試験を実施してから本番候補とする。


## 再開作業: プラットフォーム差と本文保持

FTW残件11件を通常Coraza/WASMへ同じ生HTTPで送る診断を実施し、ステータスと検知IDが全件一致した。`docs/ftw-native-wasm-comparison.md`に記録。
公式CorazaのFTWハーネスに合わせてalbedoを採用し、Host/multipartの公式期待値と、旧HTTPの実際の拒否400/505を検証する`.ftw-overrides.yml`を追加した。ignoreの追加やforced-passはしていない。
全件結果は5193件、skip 0、既存ignore 107、raw fragment 920610-2のみ失敗。raw fragmentはGo HTTP層でURIがエスケープされ、guestに生の`#`が届かず検知できない。期待値を緩めず残した。

正常POST本文の保持テストを追加し、host v0.7.0のbufferingRequestBody.Readが`err != nil && n > 0`の場合だけ保存するため、検査済み本文が次ハンドラから消えることを再現した。
`restoreRequestBody`で検査済みprefixと未読tailをwrite_bodyへ再送する方式を採用する。host bytes.Bufferへ全量を蓄積するため、すべてのWAF対象ルートで前段の有限な本文上限が必須。ユーザー合意により、Traefik bufferingをWAFより前へ配置し、サービスごとの上限を設定する。13 MiBはサンプルの値であり、plugin内の固定制限ではない。DetectionOnly標準のProcessPartialは維持し、前段上限の超過はどのモードでも413となる。
host修正を選ぶ場合はReadの保存条件だけでなく、handleNextがprefixだけを渡して未読tailを閉じる問題にも対応する必要がある。


## 前段上限と空の遮断応答

Traefik v3.6.0の実行試験で、bufferingをWAFの前に配置すると本文のない遮断応答が500へ変わる問題を再現した（初回試験のツール出力: expected 424 / got 500、`no data ready`。同名の最新ログは修正後の成功結果）。oxy bufferはContent-Length未指定の424/403で本文readerを取得し、書き込みが一度もないと`no data ready`になる。遮断応答に`Content-Length: 0`を明示する。

またSDK v0.4.0のBody.Write(nil)がhost importを呼ばずreturnするため、レスポンス検査で遮断しても元のバックエンド本文が残ることを回帰テストで確認した。HTTP WASM hostはwrite_bodyのbuf_len=0を空本文への置換として実装している。vendored SDKのWrite/WriteStringを修正し、空書き込みをhostまで渡す。phase 1〜4の空遮断応答・Content-Length・バックエンド到達をテストする。再現失敗ログ: `build/interruption-red.log`。変更箇所と出所はSDKのPATCHES.mdにも記録。

本文復元はReadRequestBodyFromが実際に読み込んだn>0の場合のみ行う。phase 1のctl:ruleEngine=Offで本文を先読みしないことを専用Readerで検証する。ctl:requestBodyAccess=Offはphase 1でのみ有効であり、phase 2での切替成功を主張しない。

前段bufferingのmemRequestBodyBytesによるディスク退避とは別に、hostは再構築本文をメモリに保持する。有限の本文上限に加えて同時処理数を考慮したメモリ評価が必要。

公式資料:
- https://doc.traefik.io/traefik/reference/routing-configuration/http/middlewares/buffering/
- https://www.coraza.io/docs/seclang/directives/#secrequestbodylimitaction
- https://github.com/vulcand/oxy/blob/v2.0.3/buffer/buffer.go
- https://github.com/http-wasm/http-wasm-host-go/blob/v0.7.0/handler/middleware.go


## 本文上限方式の実Traefik検証結果

最終WASM SHA256: `4ac4f33e131d66d568f6f5a079205f06f1deb289fe41d1a7800259a65aea4f6a`（4,275,520 bytes）。
ビルドとunit成功。完全E2Eは実CRS18件、本文保持18件、本文拒否、動的engine Off、空遮断4件、旧HTTP拒否3件が成功し、SSE1件のみ失敗を維持した。

Traefik v3.6.0では従来HTTP9件と本文上限テスト4件が成功。Content-Length/chunkedのいずれも13 MiBちょうどはWAFへ到達し、1 byte超過はWAFより前で413となった。
実CRSのOn/DetectionOnly、さらにSecRequestBodyLimitとSecRequestBodyInMemoryLimitを1024に下げたDetectionOnlyで、各10件の正常/攻撃/本文保持/前段413が成功した。小さな検査上限がOnで413となることも先に確認し、設定が反映されず通過した可能性を排除した。最初はメモリ上限を下げ忘れCorazaの設定検証で失敗したが、ログの原因に従い両上限を整合させて再実行した。
設定の元バイト列への復元とテストコンテナの削除を確認した。

ログ: `build/external-limit-{build,unit,full-e2e,traefik-e2e,traefik-modes,traefik-services}.log`。
完全E2E/FTWの残件を解消・評価するまではCoraza更新候補をコミットせず、本番配布しない。

最終WASMのFTW全件再実行は321秒で終了。5,193件、skip 0、既存ignore 107、失敗は`920610-2`の1件で、新たな失敗はない。`build/external-limit-ftw-full.log`。期待値を緩めず非0終了を保持する。


## SSE優先度とraw fragmentホスト修正案

ユーザーは現在SSE利用サービスがない可能性が高いと回答。SSE対応の優先度を下げ、既知制約と失敗テストを維持する。
raw fragmentについては、GetURIでRawPathをそのまま返す試作が既存SetURIの空白正規化を壊したため不採用。代わりにホスト入口でRequestURI内のraw #を400拒否するパッチを用意した。%23/%2523は通過し、既存GetURI/SetURIを変更しない。
隔離コピーの既存net/http＋追加7ケース、実CRS920610-1/2が成功。FTW全件対象TestFTWも317.279秒で成功した（exit 0）。通常のgo.mod/CIや標準Traefikは変更せず、そちらの既知失敗は保持する。WASM SHA256も変更なし。
パッチ・根拠・再現手順・適用上の制約は[ホスト修正案](host-request-target.md)を参照。


## 既知バグとしての受容

ユーザーがSSEとFTW 920610-2をいったん既知のバグとして管理する方針を決定した。対象ケースと同一の失敗理由を限定して記録し、これらだけを理由に更新差分のレビュー・コミットを停止し続けない。テストの削除・skip・ignore追加・期待値緩和は行わず、生の失敗結果を保持する。全テスト成功、公開承認、本番適用承認には読み替えない。現在のCIの自動リリース停止は未変更。現在の判定仕様は[既知のバグと更新判定](known-bugs.md)を参照し、過去の全成功待ちの記録よりこの方針を優先する。

## ローカル負荷試験

2026-11-07、標準Traefik v3.6.0の専用Composeで段階・本文上限・混合負荷を実行した。
no_fs_accessではSecRequestBodyInMemoryLimitとSecRequestBodyLimitが一致する必要があるため、サンプルの両上限を12.5 MiBへ整合させた。実CRS＋WASMと実Traefikの1 MiB本文保持を追加検証したが、単一の長大引数には約63〜65秒を要した。

標準go-httpbin v2.9.0では8同時混合で502、Goメモリ目標なしの試行では観測1,773.6 MiBによる資源閾値停止があり、全工程は未完走。
同じhttpbinライブラリを使う対照サーバの期限1秒では、Active到達前の約1秒切断と502を確認。同一binaryで期限を30秒にした条件は600秒・4,348件（正常200/攻撃403と本文一致）を完走した。p95 3.18秒、WAF観測最大848.2 MiB、OOMなし。
標準条件の成功とは扱わず、本番のバックエンド期限・引数長・許容遅延・CPU予算に基づく再評価が必要。これらは受容済み既知2件とは別で、更新確定は保留する。

負荷後のunitは成功、完全E2Eは既知SSEのみ失敗。実TraefikのOn/DetectionOnly/小さい検査上限のDetectionOnlyで各11項目成功し、設定復元・専用コンテナ削除を確認した。今回WASMは変更せず、FTWは再実行していない。
再現手順・条件・失敗試行・生ログの場所は[負荷試験記録](load-validation.md)を参照。

2026-11-08、長大な単一フォーム引数の遅延を切り分けた。実Traefikで1 MiB=59.47秒、Engine Off=0.667秒、本文解析のみ=0.682秒を観測。
WASM内の関数計測とCoraza DEBUGイベントで、複数のCRS正規表現評価へ時間が分散することを確認した。
計測なしの同一runtimeではプリフィルタOff=57.48秒、On=28.32秒。実験的Onを通常設定へ採用せず、問題は未解決として保持する。
診断ツールと詳細は[本文遅延調査](body-latency.md)を参照。今回の調査でWASM・通常設定・製品コードは変更していない。

同日、Corazaの標準SecRuleで後続のCRS phase 2より前に引数上限を判定する[任意設定例](argument-limits.md)を検証した。試験用16/64 KiB超過をOnで413とし、実CRS＋WASM27ケース・標準Traefik13要求が成功した。DetectionOnlyでは通過・後続評価となることも確認。上限内の本文保持・実CRS anomaly遮断・1 MiB multipartの保持は成功。
通常設定とWASMは変更せず、本番値は未決定。unit成功、完全E2Eは既知SSEのみ失敗（119.631秒）。FTWは今回の任意設定を適用しておらず再実行していない。

## 2026-11-09: 今回の完了条件

ユーザーが今回の目標を「CRSを更新して動くようにするまで」と指定した。実CRSでの基本動作・通常HTTP WASM API・Traefik Local Pluginの接続を更新対象とし、サービス別の入力制限・本番性能調整は別作業とする。
追加調査は参考資料として残し、16/64 KiBの試験ルールを通常設定へ適用しない。先の本番要件確認を更新確定の前提にはしない。既知SSE/FTWの失敗、負荷試験の未完走・遅延、公開・本番未実施は明示したまま変更を確定する。
