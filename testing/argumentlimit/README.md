# 引数の早期拒否を行う任意設定例

`early-arguments.conf`は、後続のCRS phase 2ルールより前で引数の名前・値を検査する試験用ルールである。
現在の通常設定には適用していない。上限値は試験用で、導入先の要件に合わせて決定する。

- 名前／値1つ: 16 KiB（16384 bytes）まで。
- GET/POST引数の名前＋値の合計: 64 KiB（65536 bytes）まで。
- 超過時: Engine Onで413。後続のCRS phase 2ルールへ進まず、バックエンドにも渡さない。
- DetectionOnly: 超過を記録して通過する。CPU負荷の抑制にはならない。
- ファイルデータは引数合計へ含めない。別途、WAF前の有限な本文上限が必要。

アプリで大きなフォーム／JSON引数が必要なら、この例をそのまま適用しない。
上限内の要求の検査速度は従来通り。CRSのphase 1、本文の読込みと解析は、これらphase 2ルールより先に行われる。

実CRS＋既存WASMのテスト:

```sh
GOMAXPROCS=2 GOMEMLIMIT=768MiB mise exec -- go test \
  -count=1 -tags=e2e -run '^TestE2EArgumentLimits$' -v .
```

テストはOnの21ケースとDetectionOnlyの6ケースを実行する。
フォーム・JSON・符号化・UTF-8・境界値・複数引数合計・実CRSによるSQLi/XSS検知とanomaly遮断・ファイル転送・本文完全一致を確認する。
テスト用markerルール100103は後続のCRS phase 2への到達を観測するためだけのもので、設定例には含めない。

WASMの外側の任意ファイルを`Include`する方式は使わず、設定例の内容をpluginのdirectivesへ文字列として渡す。
配置順序と候補のYAMLは[検証記録](../../docs/argument-limits.md)を参照。
