# jsondiff-eps

数値の許容誤差（epsilon）を考慮して 2 つの JSON を比較するツールです。
[runn](https://github.com/k1LoW/runn) の `exec` ステップから外部ツールとして呼び出すことを想定しています。

- オブジェクトのキー順は無視
- 数値は絶対誤差（`abs`）/ 相対誤差（`rel`）で比較。既定値に加え、パスごとに指定可能
- 無視するパス、順序を問わない配列を指定可能
- パスは runn の `compare` / `diff` と同じ **jq のパス式**（`.items[].price`、`.stats | ..`、`.. | .id?` など）
- 終了コード: `0` 一致 / `1` 差分あり / `2` エラー
- 出力: 人向けの text（既定）と機械向けの JSON

詳細な仕様は [docs/spec.md](docs/spec.md) を参照してください。

## インストール

```sh
go install github.com/ramsesyok/json-diff-with-epsilon/cmd/jsondiff-eps@latest
```

## 使い方

```
jsondiff-eps [flags] <expected> <actual>
```

`<expected>` / `<actual>` のどちらか一方に `-` を指定すると標準入力から読み込みます。

```sh
jsondiff-eps --config rules.yaml expected.json actual.json
curl -s https://example.com/api/stats | jsondiff-eps --abs 1e-6 --ignore .meta.timestamp expected.json -
```

| オプション | 説明 |
|---|---|
| `-c, --config <file>` | 設定ファイル（`.yaml` / `.yml` / `.json`） |
| `--abs <float>` | 既定の絶対誤差（設定ファイルを上書き） |
| `--rel <float>` | 既定の相対誤差（設定ファイルを上書き） |
| `--ignore <path>` | 無視するパス（複数指定可） |
| `--unordered <path>` | 順序を問わない配列のパス（複数指定可） |
| `--format text\|json` | 出力形式（既定 `text`） |
| `--color` / `--no-color` | 色付けの強制 ON / OFF（既定は端末出力時のみ ON、`NO_COLOR` を尊重） |
| `-q, --quiet` | 何も出力せず終了コードのみ返す |
| `-v, --verbose` | 一致時も要約行を出力 |
| `--version` | バージョン表示 |

## 設定ファイル

```yaml
default:            # どのルールにも該当しない数値の許容誤差
  abs: 1e-6
  rel: 0
ignore:             # 比較しないパス（配下も含む）
  - .meta.timestamp
  - .. | .requestId?
unordered:          # 順序を問わない配列
  - .tags
tolerances:         # パスごとの許容誤差（配下も含む）
  - path: .items[].price
    abs: 0.01
  - path: .items[] | select(.type == "tax") | .amount
    abs: 0.5
  - path: .stats
    rel: 0.05
  - path: .stats.cpu.avg
    abs: 0.001
```

- 数値は `|expected - actual| <= abs` または `|expected - actual| <= rel * |expected|` のどちらかを満たせば一致です。
- 複数の `tolerances` に該当した場合は、**最も深いパスに一致したルール** が優先されます（同じ深さならリストの後方）。
- ルール内で省略した `abs` / `rel` は `0` です（`default` からは引き継ぎません）。
- `ignore` は常に最優先です。

## 出力例

```
! .count: 1 (number) → "1" (string)
~ .items[0].price: 100.5 → 100.62 (diff=0.12, tolerance: abs=0.01 rel=0 by ".items[].price")
+ .meta.extra: true
- .meta.version: "1.2"
~ .status: "ok" → "error"

5 differences (compared 3 values, ignored 0)
```

`~` 値の相違 / `-` expected のみに存在 / `+` actual のみに存在 / `!` 型の相違。
表示されるパスはそのまま設定ファイルに転記できます。

`--format json` では次の形式で出力します。

```json
{
  "equal": false,
  "differences": [
    {
      "path": ".items[0].price",
      "kind": "changed",
      "expected": 100.5,
      "actual": 100.62,
      "diff": 0.12,
      "tolerance": { "abs": 0.01, "rel": 0, "rule": ".items[].price" }
    }
  ],
  "summary": { "differences": 1, "compared": 3, "ignored": 0 }
}
```

## runn から使う

```yaml
steps:
  req:
    req:
      /api/stats:
        get: {}
  compare:
    exec:
      command: jsondiff-eps --config rules.yaml testdata/expected.json -
      stdin: '{{ toJSON(steps.req.res.body) }} '
    test: current.exit_code == 0
```

```sh
runn run --scopes run:exec scenario.yml
```

- runn 1.x では `exec` の実行に `--scopes run:exec` が必要です。
- `stdin` の `}}` の後ろの **空白は必須** です。`'{{ toJSON(...) }}'` とすると runn が展開後の JSON を YAML のマップとして解釈し、`invalid stdin` エラーになります。

動作するサンプルが [examples/runn](examples/runn) にあります。

```sh
cd examples/runn
runn run --scopes run:exec compare.yml --verbose
```

## ライブラリとして使う

```go
import jsondiff "github.com/ramsesyok/json-diff-with-epsilon"

opts, err := jsondiff.LoadConfig("rules.yaml")
if err != nil { ... }
res, err := jsondiff.CompareBytes(expectedJSON, actualJSON, opts)
if err != nil { ... }
if !res.Equal {
	res.WriteText(os.Stdout, false)
}
```

## 開発

```sh
go test -race ./...                                           # ユニットテスト
go test -run '^$' -fuzz '^FuzzCompare$' -fuzztime 60s .       # ファジング
go install ./cmd/jsondiff-eps && scripts/e2e-runn.sh          # runn による E2E（runn / python3 / curl が必要）
```

CI（`.github/workflows/ci.yml`）では次を実行します。

| ジョブ | 内容 |
|---|---|
| lint | gofmt / `go mod tidy -diff` / go vet / staticcheck |
| test | Linux・macOS・Windows × Go 1.24 / 最新安定版で `go test -race`（カバレッジをジョブサマリーに出力） |
| fuzz | 各ファズターゲットを 60 秒ずつ実行（失敗入力を artifact として保存） |
| e2e-runn | runn（バージョン固定）で `examples/runn` の runbook を実行。HTTP サーバー経由のシナリオを含む |
| govulncheck | 依存関係の既知脆弱性チェック |

ファジングで見つかった失敗入力は `testdata/fuzz/` に置くと、通常の `go test` で回帰テストとして実行されます。
