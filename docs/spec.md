# jsondiff-eps 仕様書

数値データを含む 2 つの JSON を、許容誤差（epsilon）を考慮して比較するツール。
[runn](https://github.com/k1LoW/runn) の `exec` ステップから外部ツールとして呼び出すことを主な用途とする。

## 1. 概要

| 項目 | 内容 |
|---|---|
| 実装言語 | Go |
| 提供形態 | CLI（`jsondiff-eps`）＋ Go ライブラリ |
| モジュールパス | `github.com/ramsesyok/json-diff-with-epsilon` |
| ライブラリパッケージ | ルート直下 `package jsondiff` |
| CLI | `cmd/jsondiff-eps/main.go` |

## 2. 入力

```
jsondiff-eps [flags] <expected> <actual>
```

- 位置引数は 2 つ。1 つ目が期待値（expected）、2 つ目が実測値（actual）。
- どちらか一方に `-` を指定すると標準入力から読み込む。両方 `-` はエラー。
- 入力は標準 JSON（RFC 8259）。`NaN` / `Infinity` などはパースエラー（終了コード 2）。

## 3. 比較ルール

### 3.1 オブジェクト

- キーの順序は問わない。
- 片方にしか存在しないキーは差分（欠落 / 追加）として報告する。

### 3.2 配列

- 既定では順序どおり（インデックスごと）に比較する。要素数が異なる場合、余った要素を欠落 / 追加として報告する。
- `unordered` で指定したパスに一致する配列は順序を問わずに比較する。
  - 期待値側の各要素に対し、未対応の実測値側要素のうち「差分なし（許容誤差・ignore 適用後）」となる要素を先頭から探して対応付ける（O(n²)）。
  - 対応付けできなかった要素は欠落 / 追加として報告する。
  - 対応付けられた要素内部のパスは、期待値側のインデックスで表示する。
  - `unordered` は一致した配列ノード自身にのみ適用され、配下の配列には波及しない（配下にも適用したい場合は `.x | ..` のように明示する）。

### 3.3 型

JSON の型（object / array / string / number / boolean / null）を区別する。

| ケース | 結果 |
|---|---|
| `1` と `1.0`、`1e2` と `100` | 一致（数値として比較） |
| `"1"` と `1` | 不一致（型違い） |
| `null` とキー不在 | 不一致（欠落 / 追加） |
| `true` と `1` | 不一致（型違い） |

### 3.4 文字列・真偽値・null

完全一致で比較する（正規化なし）。

### 3.5 数値

適用される許容誤差を `abs`（絶対誤差）、`rel`（相対誤差）として、次のいずれかを満たせば一致とする。

```
|expected - actual| <= abs
|expected - actual| <= rel * |expected|
```

- 差の計算は、元の数値リテラルを任意精度の有理数（`big.Rat`）として行う（巨大整数・高精度小数でも丸め誤差なし）。
- 許容誤差の値は最短の 10 進表現（`0.3` など）として扱う。このため `100.5` と `100.51` の差は `abs: 0.01` ちょうどで一致と判定される（`float64` で計算した場合のような境界での誤判定がない）。
- 表示用の `diff` は `float64` に変換した値。

## 4. パス指定

runn の `compare` / `diff` 関数の `ignorePaths` と揃え、**jq のパス式**で指定する（評価は [gojq](https://github.com/itchyny/gojq) の `path()` を使用）。

| 目的 | 例 |
|---|---|
| 特定キー | `.meta.timestamp` |
| 配列の特定要素 | `.items[0].price` |
| 配列の全要素 | `.items[].price` |
| ある階層以下すべて | `.stats \| ..` |
| 任意の階層の `id` | `.. \| .id?` |
| 条件付き | `.items[] \| select(.type == "tax") \| .amount` |

- パス式は expected / actual の **両方** に対して評価し、得られたパスの和集合をルールの適用対象とする（片方にしか存在しないノードにも ignore を効かせるため）。
- ドキュメントの形に起因する評価時エラー（`null` の反復など）は、そのドキュメントについてそれ以降「一致なし」として扱う。
- 構文エラー、およびパス式でない式（`1`、`.a | tostring` など）は設定エラー（終了コード 2）。
- スライス（`.[1:3]`）で得られるパスは単一ノードに対応しないため無視する。
- ignore / tolerances は、一致したノード **とその配下すべて** に適用される。

## 5. ルールの優先順位

1. `ignore` に該当するノードは比較しない（最優先）。
2. 数値ノードに対して複数の `tolerances` ルールが該当した場合、**一致したパスが最も深い（対象に最も近い）ルール** を採用する。深さが同じ場合は **リストの後方** のルールを採用する。
3. どのルールにも該当しない場合は `default` を用いる。
4. ルール内で `abs` / `rel` の片方のみ指定した場合、もう片方は `0` とみなす（`default` からは引き継がない）。

## 6. 設定

### 6.1 設定ファイル

`--config` / `-c` で指定。拡張子 `.yaml` / `.yml` は YAML、`.json` は JSON として読む（その他の拡張子は YAML として読む）。

```yaml
default:
  abs: 1e-6
  rel: 0
ignore:
  - .meta.timestamp
  - .. | .requestId?
unordered:
  - .tags
  - .items
tolerances:
  - path: .items[].price
    abs: 0.01
  - path: .stats
    rel: 0.001
  - path: .stats.cpu.avg
    abs: 0.001
```

| キー | 型 | 既定値 | 説明 |
|---|---|---|---|
| `default.abs` | number | `0` | 既定の絶対誤差 |
| `default.rel` | number | `0` | 既定の相対誤差 |
| `ignore` | string[] | `[]` | 比較しないパス |
| `unordered` | string[] | `[]` | 順序を問わない配列のパス |
| `tolerances[].path` | string | 必須 | 適用パス |
| `tolerances[].abs` | number | `0` | 絶対誤差 |
| `tolerances[].rel` | number | `0` | 相対誤差 |

未知のキー、負の誤差値はエラー（終了コード 2）。

### 6.2 CLI オプション

| オプション | 説明 |
|---|---|
| `-c, --config <file>` | 設定ファイル |
| `--abs <float>` | 既定の絶対誤差（設定ファイルの `default.abs` を上書き） |
| `--rel <float>` | 既定の相対誤差（設定ファイルの `default.rel` を上書き） |
| `--ignore <path>` | 無視パス（複数指定可、設定ファイルに追加） |
| `--unordered <path>` | 順序を問わない配列のパス（複数指定可、設定ファイルに追加） |
| `--format text\|json` | 出力形式（既定 `text`） |
| `--color` / `--no-color` | 色付けの強制 ON / OFF（既定は TTY のときのみ ON） |
| `-q, --quiet` | 差分を出力せず終了コードのみ返す |
| `-v, --verbose` | 一致時も要約行を出力する（text 形式） |
| `--version` | バージョン表示 |

パス単位の許容誤差（`tolerances`）は設定ファイルでのみ指定する。

## 7. 出力

差分は標準出力、エラーメッセージは標準エラー出力に出す。パスは jq 形式で表示し、そのまま設定ファイルへ転記できる。

### 7.1 text（既定）

```
~ .items[0].price: 100.5 → 100.62 (diff=0.12, tolerance: abs=0.01 rel=0 by ".items[].price")
~ .status: "ok" → "error"
- .meta.version: "1.2"
+ .meta.extra: true
! .count: 1 (number) → "1" (string)

5 differences (compared 128 values, ignored 3)
```

| 記号 | 種別（`kind`） | 意味 |
|---|---|---|
| `~` | `changed` | 値の相違 |
| `-` | `removed` | expected にあり actual にない |
| `+` | `added` | actual にあり expected にない |
| `!` | `type_mismatch` | 型の相違 |

- 数値の相違には差（`diff`）と適用された許容誤差・由来ルール（`default` の場合は `by default`）を表示する。
- 差分は object のキー順（辞書順）・配列のインデックス順に並ぶ。
- 一致時は何も出力しない（`-v` 指定時は要約行のみ）。

### 7.2 json

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
  "summary": { "differences": 1, "compared": 128, "ignored": 3 }
}
```

- 一致時も `{"equal": true, "differences": [], ...}` を出力する。
- `removed` では `actual`、`added` では `expected` を省略する。`diff` / `tolerance` は数値の `changed` のみ。
- `type_mismatch` では `expected_type` / `actual_type`（`object` / `array` / `string` / `number` / `boolean` / `null`）を追加する。
- 由来ルールが既定値の場合、`tolerance.rule` は `"default"`。

## 8. 終了コード

| コード | 意味 |
|---|---|
| 0 | 一致（差はすべて許容誤差内または ignore 対象） |
| 1 | 差分あり |
| 2 | エラー（ファイル不在、JSON パースエラー、設定エラー、jq 式の構文エラー等） |

## 9. runn での利用例

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

JSON 出力を条件判定に使う場合:

```yaml
  compare:
    exec:
      command: jsondiff-eps --format json -c rules.yaml testdata/expected.json -
      stdin: '{{ toJSON(steps.req.res.body) }} '
    test: fromJSON(current.stdout).summary.differences == 0
```

runn 1.x での注意点（runn 1.11.0 で確認）:

- `exec` の実行には `runn run --scopes run:exec` が必要。
- `stdin: '{{ toJSON(...) }}'` のように JSON だけを展開すると、runn が展開結果を YAML のマップとして再解釈し `invalid stdin` になる。`}}` の後ろに空白を 1 つ置く（`'{{ toJSON(...) }} '`）と文字列のまま渡る。
- ブロックスカラー（`stdin: |`）では末尾に文字どおりの `\n` が付加され JSON として不正になるため使わない。

実行可能なサンプルは `examples/runn/` を参照。

## 10. ライブラリ API

```go
package jsondiff

type Tolerance struct {
    Abs float64 `json:"abs" yaml:"abs"`
    Rel float64 `json:"rel" yaml:"rel"`
}

type ToleranceRule struct {
    Path string  `json:"path" yaml:"path"`
    Abs  float64 `json:"abs"  yaml:"abs"`
    Rel  float64 `json:"rel"  yaml:"rel"`
}

type Options struct {
    Default    Tolerance       `json:"default"    yaml:"default"`
    Ignore     []string        `json:"ignore"     yaml:"ignore"`
    Unordered  []string        `json:"unordered"  yaml:"unordered"`
    Tolerances []ToleranceRule `json:"tolerances" yaml:"tolerances"`
}

func LoadConfig(path string) (*Options, error)

// expected / actual は json.Decoder.UseNumber() でデコードした値（json.Number を含む any）
func Compare(expected, actual any, opts *Options) (*Result, error)
func CompareBytes(expected, actual []byte, opts *Options) (*Result, error)
func DecodeJSON(r io.Reader) (any, error)

type Result struct {
    Equal       bool         `json:"equal"`
    Differences []Difference `json:"differences"`
    Summary     Summary      `json:"summary"`
}

func (r *Result) WriteText(w io.Writer, color bool) error // 一致時は何も書かない
func (r *Result) WriteSummary(w io.Writer) error
func (r *Result) WriteJSON(w io.Writer) error
```
