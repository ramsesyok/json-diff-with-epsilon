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

Go がない環境では、[Releases](https://github.com/ramsesyok/json-diff-with-epsilon/releases) から OS / アーキテクチャに合ったアーカイブ（Linux / macOS / Windows × amd64 / arm64）をダウンロードし、`jsondiff-eps` を PATH の通った場所に置いてください。

```sh
# 例: Linux amd64
curl -fsSL -o jsondiff-eps.tar.gz \
  https://github.com/ramsesyok/json-diff-with-epsilon/releases/download/v0.1.0/jsondiff-eps_v0.1.0_linux_amd64.tar.gz
tar -xzf jsondiff-eps.tar.gz jsondiff-eps
sudo install jsondiff-eps /usr/local/bin/
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

runn の `exec` ステップから `jsondiff-eps` を呼び出し、API のレスポンスを期待値ファイルと比較します。
以下の内容は runn 1.11.0 で動作を確認しています。

### 前提

- `jsondiff-eps` が PATH の通った場所にあること（[インストール](#インストール)参照）
- runn 1.x では `exec` の実行に **`--scopes run:exec`** が必要です

```sh
runn run --scopes run:exec scenario.yml
```

### 基本形：レスポンスを期待値ファイルと比較する

```yaml
desc: 統計 API のレスポンスを期待値と比較する
runners:
  req: https://api.example.com
steps:
  get_stats:
    req:
      /api/stats:
        get:
          body: null
    test: current.res.status == 200
  compare:
    exec:
      command: jsondiff-eps --config rules.yaml testdata/stats.json -
      stdin: '{{ toJSON(steps.get_stats.res.body) }} '
    test: current.stdout == "" && current.stderr == "" && current.exit_code == 0
```

- 1 つ目の引数が期待値ファイル、2 つ目の `-` が標準入力（レスポンス）です。
- **`stdin` の `}}` の後ろの空白は必須です。** `'{{ toJSON(...) }}'` とすると runn が展開後の JSON を YAML のマップとして解釈し、`invalid stdin` エラーになります。ブロックスカラー（`stdin: |`）も末尾に文字どおりの `\n` が付いて JSON として不正になるため使えません。
- 無視するキーや許容誤差は `rules.yaml` に書きます（[設定ファイル](#設定ファイル)参照）。

### test の書き方

| 書き方 | 用途 |
|---|---|
| `current.stdout == "" && current.stderr == "" && current.exit_code == 0` | **推奨。** 失敗時に差分やエラーメッセージが runn の出力にそのまま表示されます |
| `current.exit_code == 0` | 簡潔ですが、失敗時に差分の内容は表示されません |
| `fromJSON(current.stdout).summary.differences == 0` | `--format json` と組み合わせて、差分の中身を条件にする場合 |

推奨の書き方では、一致時は標準出力・標準エラー出力とも空になることを利用しています。`current.stdout == ""` を **先頭に** 書くのがポイントです（runn は `&&` の左側で結果が決まると右側を評価・表示しません）。失敗時は次のように表示されます。

```
  Condition:
    current.stdout == "" && current.stderr == "" && current.exit_code == 0
    │
    ├── current.stdout == "" && current.stderr == "" => false
    │   ├── current.stdout == "" => false
    │   │   ├── current.stdout => "~ .items[0].price: 100.5 → 100.62 (diff=0.12, tolerance: abs=0.01 rel=0 by \".items[].price\")
    │   │   │   ~ .status: \"ok\" → \"error\"
    │   │   │
    │   │   │   2 differences (compared 12 values, ignored 1)
    │   │   │   "
```

`current.exit_code` の値の意味は次のとおりです。

| `current.exit_code` | 意味 |
|---|---|
| 0 | 一致（差はすべて許容誤差内、または ignore 対象） |
| 1 | 差分あり |
| 2 | エラー（ファイルがない、JSON が不正、設定ファイルの誤りなど。内容は `current.stderr`） |

### 差分の中身を条件にする（JSON 出力）

`--format json` を付けると結果を JSON で受け取れるので、`fromJSON(current.stdout)` で細かく判定できます。

```yaml
  compare:
    exec:
      command: jsondiff-eps --format json --config rules.yaml testdata/stats.json -
      stdin: '{{ toJSON(steps.get_stats.res.body) }} '
    test: |
      fromJSON(current.stdout).equal
      || all(fromJSON(current.stdout).differences, { .path startsWith ".debug" })
```

JSON 出力の形式は [出力例](#出力例) を参照してください。

### ファイルパスの基準

`exec` のコマンドは **runn を実行したカレントディレクトリ** で実行されます（runbook の場所ではありません）。一方、`vars` の `json://` などは runbook の場所が基準です。

- runbook のあるディレクトリで `runn run` を実行するか、
- カレントディレクトリからのパスで期待値ファイル・設定ファイルを指定してください。

別のディレクトリから実行してファイルが見つからない場合は、終了コード 2 と `current.stderr` に `open testdata/stats.json: no such file or directory` のようなメッセージが出ます。

### 複数の API を同じ手順で比較する

期待値ファイルのパスを `vars` に置くと、ステップを使い回しやすくなります。

```yaml
vars:
  expected_dir: testdata
steps:
  get_stats:
    req:
      /api/stats:
        get:
          body: null
  compare_stats:
    exec:
      command: jsondiff-eps --config rules.yaml {{ vars.expected_dir }}/stats.json -
      stdin: '{{ toJSON(steps.get_stats.res.body) }} '
    test: current.stdout == "" && current.stderr == "" && current.exit_code == 0
  get_users:
    req:
      /api/users:
        get:
          body: null
  compare_users:
    exec:
      command: jsondiff-eps --config rules.yaml --unordered . {{ vars.expected_dir }}/users.json -
      stdin: '{{ toJSON(steps.get_users.res.body) }} '
    test: current.stdout == "" && current.stderr == "" && current.exit_code == 0
```

CLI オプション（`--ignore`、`--unordered`、`--abs`、`--rel`）は設定ファイルに追加・上書きされるので、共通のルールは `rules.yaml` に、API 固有の指定はコマンドに書く、という使い分けができます。

### 期待値ファイルを作る・更新する

runn の `dump` の `out` でレスポンスをファイルに保存できます。初回の期待値作成や、仕様変更時の更新に使えます（保存した内容は目視で確認してからコミットしてください）。

```yaml
desc: 期待値ファイルを更新する
runners:
  req: https://api.example.com
steps:
  get_stats:
    req:
      /api/stats:
        get:
          body: null
  save:
    dump:
      expr: steps.get_stats.res.body
      out: testdata/stats.json
```

### よくあるエラー

| 症状 | 原因と対処 |
|---|---|
| `scope error: exec runner is not allowed` | `runn run --scopes run:exec ...` で実行する |
| `invalid exec command: invalid stdin` | `stdin` の `}}` の後ろに空白を入れる（`'{{ toJSON(...) }} '`） |
| `current.exit_code => 2`、`no such file or directory` | カレントディレクトリ基準のパスになっていない（[ファイルパスの基準](#ファイルパスの基準)） |
| `current.exit_code => 2`、`unexpected data after the JSON value` | `stdin: \|` のブロックスカラーを使っている。1 行の `'{{ toJSON(...) }} '` にする |
| 一致するはずの数値が差分になる | 既定の許容誤差は 0。`default.abs` / `tolerances` を設定する。表示される `tolerance: ... by "..."` で、どのルールが適用されたか確認できる |

### サンプル

動作するサンプルが [examples/runn](examples/runn) にあります（CI でも実行しています）。

```sh
cd examples/runn
runn run --scopes run:exec compare.yml --verbose

# HTTP 経由のシナリオ（別ターミナルで examples/runn を配信しておく）
python3 -m http.server 18080 --bind 127.0.0.1
runn run --scopes run:exec http.yml --verbose
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
| release-check | GoReleaser の設定でスナップショットビルド（リリース設定の検証） |
| govulncheck | 依存関係の既知脆弱性チェック |

### リリース

`v*` 形式のタグを push すると、`.github/workflows/release.yml` が GoReleaser で各 OS 向けバイナリをビルドし、GitHub Release を作成します。
Actions 画面から「Release」ワークフローを手動実行（`tag` に `v0.2.0` などを指定）しても、タグ作成とリリースを行えます。

ファジングで見つかった失敗入力は `testdata/fuzz/` に置くと、通常の `go test` で回帰テストとして実行されます。

## ライセンス

[MIT License](LICENSE)
