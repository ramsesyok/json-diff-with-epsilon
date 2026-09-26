package jsondiff

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// brief renders differences as "kind path" for compact assertions.
func brief(r *Result) []string {
	out := []string{}
	for _, d := range r.Differences {
		out = append(out, string(d.Kind)+" "+d.Path)
	}
	return out
}

func mustCompare(t *testing.T, e, a string, opts *Options) *Result {
	t.Helper()
	r, err := CompareBytes([]byte(e), []byte(a), opts)
	if err != nil {
		t.Fatalf("CompareBytes: %v", err)
	}
	return r
}

func TestCompare(t *testing.T) {
	tests := []struct {
		name string
		e, a string
		opts *Options
		want []string
	}{
		{
			name: "key order does not matter",
			e:    `{"a":1,"b":{"c":"x","d":[1,2]}}`,
			a:    `{"b":{"d":[1,2],"c":"x"},"a":1}`,
			want: []string{},
		},
		{
			name: "strings compared exactly",
			e:    `{"s":"abc"}`,
			a:    `{"s":"abd"}`,
			want: []string{"changed .s"},
		},
		{
			name: "removed and added keys",
			e:    `{"a":1,"b":2}`,
			a:    `{"a":1,"c":3}`,
			want: []string{"removed .b", "added .c"},
		},
		{
			name: "null is not a missing key",
			e:    `{"a":null}`,
			a:    `{}`,
			want: []string{"removed .a"},
		},
		{
			name: "number and numeric string are different types",
			e:    `{"a":1}`,
			a:    `{"a":"1"}`,
			want: []string{"type_mismatch .a"},
		},
		{
			name: "true and 1 are different types",
			e:    `[true]`,
			a:    `[1]`,
			want: []string{"type_mismatch .[0]"},
		},
		{
			name: "1 and 1.0 and 1e0 are equal",
			e:    `[1, 100, 0.5]`,
			a:    `[1.0, 1e2, 5e-1]`,
			want: []string{},
		},
		{
			name: "big integers compared exactly without tolerance",
			e:    `[12345678901234567890]`,
			a:    `[12345678901234567891]`,
			want: []string{"changed .[0]"},
		},
		{
			name: "default absolute tolerance",
			e:    `{"a":1.0,"b":2.0}`,
			a:    `{"a":1.0005,"b":2.002}`,
			opts: &Options{Default: Tolerance{Abs: 0.001}},
			want: []string{"changed .b"},
		},
		{
			name: "difference exactly at the tolerance is accepted",
			e:    `[100.5, 0.1]`,
			a:    `[100.51, 0.4]`,
			opts: &Options{Default: Tolerance{Abs: 0.3}, Tolerances: []ToleranceRule{{Path: ".[0]", Abs: 0.01}}},
			want: []string{},
		},
		{
			name: "relative tolerance uses expected as base",
			e:    `[1000, 0.001]`,
			a:    `[1000.9, 0.0011]`,
			opts: &Options{Default: Tolerance{Rel: 0.001}},
			want: []string{"changed .[1]"},
		},
		{
			name: "abs or rel is enough",
			e:    `[0, 1000]`,
			a:    `[0.0001, 1001]`,
			opts: &Options{Default: Tolerance{Abs: 0.001, Rel: 0.001}},
			want: []string{},
		},
		{
			name: "ignore removes subtree including one-sided keys",
			e:    `{"meta":{"ts":1,"x":{"y":2}},"a":1}`,
			a:    `{"meta":{"ts":2,"x":{"y":3,"z":4}},"a":1}`,
			opts: &Options{Ignore: []string{".meta.ts", ".meta.x"}},
			want: []string{},
		},
		{
			name: "ignore key only present in actual",
			e:    `{"a":1}`,
			a:    `{"a":1,"requestId":"x"}`,
			opts: &Options{Ignore: []string{".. | .requestId?"}},
			want: []string{},
		},
		{
			name: "ignore at any depth",
			e:    `{"a":{"id":1,"v":1},"b":[{"id":2,"v":2}]}`,
			a:    `{"a":{"id":9,"v":1},"b":[{"id":8,"v":2}]}`,
			opts: &Options{Ignore: []string{".. | .id?"}},
			want: []string{},
		},
		{
			name: "rule applies to descendants",
			e:    `{"stats":{"cpu":{"avg":1.0}},"other":1.0}`,
			a:    `{"stats":{"cpu":{"avg":1.05}},"other":1.05}`,
			opts: &Options{Tolerances: []ToleranceRule{{Path: ".stats", Abs: 0.1}}},
			want: []string{"changed .other"},
		},
		{
			name: "deeper rule wins regardless of order",
			e:    `{"stats":{"cpu":{"avg":1.0,"max":1.0}}}`,
			a:    `{"stats":{"cpu":{"avg":1.05,"max":1.05}}}`,
			opts: &Options{Tolerances: []ToleranceRule{
				{Path: ".stats.cpu.avg", Abs: 0.001},
				{Path: ".stats", Abs: 0.1},
			}},
			want: []string{"changed .stats.cpu.avg"},
		},
		{
			name: "last rule wins at the same depth",
			e:    `{"a":1.0}`,
			a:    `{"a":1.05}`,
			opts: &Options{Tolerances: []ToleranceRule{
				{Path: ".a", Abs: 0.001},
				{Path: ".. | .a?", Abs: 0.1},
			}},
			want: []string{},
		},
		{
			name: "omitted abs in rule is zero, not inherited",
			e:    `{"a":0}`,
			a:    `{"a":0.0001}`,
			opts: &Options{Default: Tolerance{Abs: 0.01}, Tolerances: []ToleranceRule{{Path: ".a", Rel: 0.5}}},
			want: []string{"changed .a"},
		},
		{
			name: "wildcard array rule",
			e:    `{"items":[{"price":1.0},{"price":2.0}]}`,
			a:    `{"items":[{"price":1.005},{"price":2.02}]}`,
			opts: &Options{Tolerances: []ToleranceRule{{Path: ".items[].price", Abs: 0.01}}},
			want: []string{"changed .items[1].price"},
		},
		{
			name: "select condition rule",
			e:    `{"items":[{"type":"tax","v":1.0},{"type":"x","v":1.0}]}`,
			a:    `{"items":[{"type":"tax","v":1.5},{"type":"x","v":1.5}]}`,
			opts: &Options{Tolerances: []ToleranceRule{{Path: `.items[] | select(.type == "tax") | .v`, Abs: 1}}},
			want: []string{"changed .items[1].v"},
		},
		{
			name: "ordered array length mismatch",
			e:    `[1,2,3]`,
			a:    `[1,2]`,
			want: []string{"removed .[2]"},
		},
		{
			name: "array order matters by default",
			e:    `{"t":["a","b"]}`,
			a:    `{"t":["b","a"]}`,
			want: []string{"changed .t[0]", "changed .t[1]"},
		},
		{
			name: "unordered array",
			e:    `{"t":["a","b","c"]}`,
			a:    `{"t":["c","a","b"]}`,
			opts: &Options{Unordered: []string{".t"}},
			want: []string{},
		},
		{
			name: "unordered array with tolerance and leftovers",
			e:    `{"t":[1.0, 2.0, 3.0]}`,
			a:    `{"t":[2.001, 1.001, 5.0]}`,
			opts: &Options{Unordered: []string{".t"}, Default: Tolerance{Abs: 0.01}},
			want: []string{"removed .t[2]", "added .t[2]"},
		},
		{
			name: "unordered array of objects",
			e:    `{"items":[{"id":1,"v":1.0},{"id":2,"v":2.0}]}`,
			a:    `{"items":[{"id":2,"v":2.0},{"id":1,"v":1.0}]}`,
			opts: &Options{Unordered: []string{".items"}},
			want: []string{},
		},
		{
			name: "unordered does not apply to nested arrays",
			e:    `{"t":[[1,2]]}`,
			a:    `{"t":[[2,1]]}`,
			opts: &Options{Unordered: []string{".t"}},
			want: []string{"removed .t[0]", "added .t[0]"},
		},
		{
			name: "keys needing quotes",
			e:    `{"a.b":1,"":2,"x y":{"0":3}}`,
			a:    `{"a.b":2,"":3,"x y":{"0":4}}`,
			want: []string{`changed .[""]`, `changed .["a.b"]`, `changed .["x y"]["0"]`},
		},
		{
			name: "root scalar",
			e:    `1`,
			a:    `2`,
			want: []string{"changed ."},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := mustCompare(t, tt.e, tt.a, tt.opts)
			if got := brief(r); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %q, want %q", got, tt.want)
			}
			if r.Equal != (len(tt.want) == 0) {
				t.Errorf("Equal = %v", r.Equal)
			}
		})
	}
}

func TestCompareSummary(t *testing.T) {
	r := mustCompare(t, `{"a":1,"b":"x","c":[1,2],"ts":1}`, `{"a":1,"b":"y","c":[1,2],"ts":2}`,
		&Options{Ignore: []string{".ts"}})
	want := Summary{Differences: 1, Compared: 4, Ignored: 1}
	if r.Summary != want {
		t.Errorf("got %+v, want %+v", r.Summary, want)
	}
}

func TestAppliedTolerance(t *testing.T) {
	r := mustCompare(t, `{"p":100.5,"q":1}`, `{"p":100.62,"q":2}`, &Options{
		Default:    Tolerance{Abs: 0.5},
		Tolerances: []ToleranceRule{{Path: ".p", Abs: 0.01}},
	})
	if len(r.Differences) != 2 {
		t.Fatalf("got %q", brief(r))
	}
	p, q := r.Differences[0], r.Differences[1]
	if *p.Diff != 0.12 || *p.Tolerance != (AppliedTolerance{Abs: 0.01, Rule: ".p"}) {
		t.Errorf("p: diff=%v tolerance=%+v", *p.Diff, *p.Tolerance)
	}
	if q.Tolerance.Rule != DefaultRule {
		t.Errorf("q: rule=%q", q.Tolerance.Rule)
	}
}

func TestCompareGoValues(t *testing.T) {
	e := map[string]any{"a": 1.0, "b": []any{1, "x", nil, true}}
	a := map[string]any{"a": 1.0000001, "b": []any{1.0, "x", nil, true}}
	r, err := Compare(e, a, &Options{Default: Tolerance{Abs: 1e-6}})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Equal {
		t.Errorf("got %q", brief(r))
	}
}

func TestOptionErrors(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		want string
	}{
		{"negative default", Options{Default: Tolerance{Abs: -1}}, "default"},
		{"negative rule", Options{Tolerances: []ToleranceRule{{Path: ".a", Rel: -1}}}, "tolerances[0]"},
		{"empty path", Options{Ignore: []string{" "}}, "empty path"},
		{"syntax error", Options{Unordered: []string{".["}}, "unordered: invalid path"},
		{"not a path expression", Options{Ignore: []string{"1"}}, "not a path expression"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := CompareBytes([]byte(`{"a":1}`), []byte(`{"a":1}`), &tt.opts)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("got %v, want error containing %q", err, tt.want)
			}
		})
	}
}

func TestDecodeJSONErrors(t *testing.T) {
	for _, in := range []string{``, `{`, `{"a":NaN}`, `[Infinity]`, `1 2`} {
		if _, err := DecodeJSON(strings.NewReader(in)); err == nil {
			t.Errorf("%q: expected error", in)
		}
	}
}

func TestWriteText(t *testing.T) {
	r := mustCompare(t,
		`{"items":[{"price":100.5}],"status":"ok","meta":{"version":"1.2"},"count":1}`,
		`{"items":[{"price":100.62}],"status":"error","meta":{"extra":true},"count":"1"}`,
		&Options{Tolerances: []ToleranceRule{{Path: ".items[].price", Abs: 0.01}}})
	var buf bytes.Buffer
	if err := r.WriteText(&buf, false); err != nil {
		t.Fatal(err)
	}
	want := `! .count: 1 (number) → "1" (string)
~ .items[0].price: 100.5 → 100.62 (diff=0.12, tolerance: abs=0.01 rel=0 by ".items[].price")
+ .meta.extra: true
- .meta.version: "1.2"
~ .status: "ok" → "error"

5 differences (compared 3 values, ignored 0)
`
	if buf.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", buf.String(), want)
	}

	buf.Reset()
	if err := r.WriteText(&buf, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), colorRed+`- .meta.version: "1.2"`+colorReset) {
		t.Errorf("colored output missing: %q", buf.String())
	}
}

func TestWriteTextDefaultRuleAndEqual(t *testing.T) {
	r := mustCompare(t, `[1]`, `[2]`, nil)
	var buf bytes.Buffer
	_ = r.WriteText(&buf, false)
	if !strings.HasPrefix(buf.String(), "~ .[0]: 1 → 2 (diff=1, tolerance: abs=0 rel=0 by default)\n") {
		t.Errorf("got %q", buf.String())
	}

	buf.Reset()
	r = mustCompare(t, `[1]`, `[1]`, nil)
	_ = r.WriteText(&buf, false)
	if buf.Len() != 0 {
		t.Errorf("equal result wrote %q", buf.String())
	}
}

func TestWriteJSON(t *testing.T) {
	r := mustCompare(t,
		`{"a":1.5,"b":null,"c":1}`,
		`{"a":1.75,"b":1,"d":"<x>"}`,
		&Options{Default: Tolerance{Abs: 0.1}})
	var buf bytes.Buffer
	if err := r.WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}
	var got any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	want := map[string]any{
		"equal": false,
		"differences": []any{
			map[string]any{"path": ".a", "kind": "changed", "expected": 1.5, "actual": 1.75, "diff": 0.25,
				"tolerance": map[string]any{"abs": 0.1, "rel": 0.0, "rule": "default"}},
			map[string]any{"path": ".b", "kind": "type_mismatch", "expected": nil, "actual": 1.0,
				"expected_type": "null", "actual_type": "number"},
			map[string]any{"path": ".c", "kind": "removed", "expected": 1.0},
			map[string]any{"path": ".d", "kind": "added", "actual": "<x>"},
		},
		"summary": map[string]any{"differences": 4.0, "compared": 2.0, "ignored": 0.0},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), `"<x>"`) {
		t.Errorf("HTML characters escaped: %s", buf.String())
	}

	buf.Reset()
	r = mustCompare(t, `{}`, `{}`, nil)
	_ = r.WriteJSON(&buf)
	if !strings.Contains(buf.String(), `"differences": []`) {
		t.Errorf("equal result: %s", buf.String())
	}
}
