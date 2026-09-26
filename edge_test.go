package jsondiff

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestCompareEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		e, a string
		opts *Options
		want []string
	}{
		{
			name: "ordered array with extra actual elements",
			e:    `[1]`,
			a:    `[1,2,3]`,
			want: []string{"added .[1]", "added .[2]"},
		},
		{
			name: "ignore extra ordered element",
			e:    `[1]`,
			a:    `[1,2]`,
			opts: &Options{Ignore: []string{".[1]"}},
			want: []string{},
		},
		{
			name: "ignore matched only in actual via select",
			e:    `{"items":[{"id":1}]}`,
			a:    `{"items":[{"id":1},{"id":2,"debug":true}]}`,
			opts: &Options{Ignore: []string{".items[] | select(.debug?)"}},
			want: []string{},
		},
		{
			name: "ignore inside unordered elements",
			e:    `{"items":[{"id":1,"ts":1},{"id":2,"ts":2}]}`,
			a:    `{"items":[{"id":2,"ts":9},{"id":1,"ts":8}]}`,
			opts: &Options{Unordered: []string{".items"}, Ignore: []string{".items[].ts"}},
			want: []string{},
		},
		{
			name: "ignore unpaired unordered element",
			e:    `{"items":[{"id":1}]}`,
			a:    `{"items":[{"id":9,"debug":true},{"id":1}]}`,
			opts: &Options{Unordered: []string{".items"}, Ignore: []string{".items[] | select(.debug?)"}},
			want: []string{},
		},
		{
			name: "unordered with duplicates",
			e:    `[1,1,2]`,
			a:    `[2,1,2]`,
			opts: &Options{Unordered: []string{"."}},
			want: []string{"removed .[1]", "added .[2]"},
		},
		{
			name: "unordered empty arrays",
			e:    `{"t":[]}`,
			a:    `{"t":[]}`,
			opts: &Options{Unordered: []string{".t"}},
			want: []string{},
		},
		{
			name: "tolerance rule matched only in actual",
			e:    `[99.0]`,
			a:    `[100.5]`,
			opts: &Options{Tolerances: []ToleranceRule{{Path: ".[] | select(. > 100)", Abs: 2}}},
			want: []string{},
		},
		{
			name: "slice paths are ignored without error",
			e:    `[1,2]`,
			a:    `[1,3]`,
			opts: &Options{Ignore: []string{".[1:3]"}},
			want: []string{"changed .[1]"},
		},
		{
			name: "runtime error in path evaluation is not an error",
			e:    `{"a":null,"b":1}`,
			a:    `{"a":null,"b":1}`,
			opts: &Options{Ignore: []string{".a[]"}},
			want: []string{},
		},
		{
			name: "relative tolerance with negative expected",
			e:    `[-1000, -1000]`,
			a:    `[-1000.9, -998]`,
			opts: &Options{Default: Tolerance{Rel: 0.001}},
			want: []string{"changed .[1]"},
		},
		{
			name: "relative tolerance with zero expected",
			e:    `[0]`,
			a:    `[1e-12]`,
			opts: &Options{Default: Tolerance{Rel: 0.5}},
			want: []string{"changed .[0]"},
		},
		{
			name: "numbers beyond float64 range",
			e:    `[1e400, 1e400]`,
			a:    `[1e400, 1.0000001e400]`,
			opts: &Options{Tolerances: []ToleranceRule{{Path: ".[1]", Rel: 1e-6}}},
			want: []string{},
		},
		{
			name: "tiny difference beyond float64 precision",
			e:    `[0.1000000000000000000001]`,
			a:    `[0.1000000000000000000002]`,
			want: []string{"changed .[0]"},
		},
		{
			name: "negative zero",
			e:    `[0, -0.0]`,
			a:    `[-0, 0]`,
			want: []string{},
		},
		{
			name: "empty object and empty array",
			e:    `{"a":{},"b":[]}`,
			a:    `{"a":[],"b":{}}`,
			want: []string{"type_mismatch .a", "type_mismatch .b"},
		},
		{
			name: "null values",
			e:    `{"a":null,"b":null}`,
			a:    `{"a":null,"b":false}`,
			want: []string{"type_mismatch .b"},
		},
		{
			name: "unicode and escaped keys",
			e:    `{"日本":1,"a\"b":1,"<x>":1}`,
			a:    `{"日本":2,"a\"b":2,"<x>":2}`,
			want: []string{`changed .["<x>"]`, `changed .["a\"b"]`, `changed .["日本"]`},
		},
		{
			name: "unicode string values",
			e:    `{"s":"こんにちは"}`,
			a:    `{"s":"こんばんは"}`,
			want: []string{"changed .s"},
		},
		{
			name: "deeply nested",
			e:    `{"a":[{"b":[{"c":[1.0]}]}]}`,
			a:    `{"a":[{"b":[{"c":[1.5]}]}]}`,
			opts: &Options{Tolerances: []ToleranceRule{{Path: ".a[0].b", Abs: 1}}},
			want: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := mustCompare(t, tt.e, tt.a, tt.opts)
			if got := brief(r); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHugeExponents(t *testing.T) {
	tests := []struct {
		name string
		e, a string
		opts *Options
		want []string
	}{
		{"equal to itself", `[1E1000001, -1e-1000001, 1e99999999999]`, `[1E1000001, -1e-1000001, 1e99999999999]`, nil, []string{}},
		{"relative tolerance", `[1e1000001]`, `[1.0000001e1000001]`, &Options{Default: Tolerance{Rel: 1e-6}}, []string{}},
		{"different", `[1e1000001, 1e99999999999]`, `[2e1000001, 2e99999999999]`, nil, []string{"changed .[0]", "changed .[1]"}},
		{"underflowing literal", `[1E-700000000, 1E-700000000]`, `[1E-700000000, 1]`, nil, []string{"changed .[1]"}},
		{"tiny values within abs", `[1e-1000001]`, `[0]`, &Options{Default: Tolerance{Abs: 1e-300}}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := mustCompare(t, tt.e, tt.a, tt.opts)
			if got := brief(r); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %q, want %q", got, tt.want)
			}
			var buf bytes.Buffer
			if err := r.WriteJSON(&buf); err != nil {
				t.Errorf("WriteJSON: %v", err)
			}
			if err := r.WriteText(&buf, false); err != nil {
				t.Errorf("WriteText: %v", err)
			}
		})
	}
}

func TestReportedPathsAreUsableAsIgnorePaths(t *testing.T) {
	e := `{"a.b":1,"":{"x y":[1,2]},"日本":{"k":true},"a\"b":null,"arr":[[1],[2]],"only":1}`
	a := `{"a.b":2,"":{"x y":[1,3,4]},"日本":{"k":false},"a\"b":0,"arr":[[1],[3]],"added":1}`
	r := mustCompare(t, e, a, nil)
	if len(r.Differences) < 6 {
		t.Fatalf("expected several differences, got %q", brief(r))
	}
	for _, d := range r.Differences {
		r2 := mustCompare(t, e, a, &Options{Ignore: []string{d.Path}})
		for _, d2 := range r2.Differences {
			if d2.Path == d.Path {
				t.Errorf("ignoring %s did not remove it", d.Path)
			}
		}
		if r2.Summary.Ignored == 0 {
			t.Errorf("ignoring %s ignored nothing", d.Path)
		}
	}
}

func TestCompareGoNumberTypes(t *testing.T) {
	e := []any{float32(1.5), int64(2), uint8(3), 4, 5.0}
	a := []any{1.5, 2.0, 3, int32(4), float32(5)}
	r, err := Compare(e, a, &Options{Ignore: []string{".[] | select(. > 100)"}})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Equal {
		t.Errorf("got %q", brief(r))
	}
}

func TestCompareUnsupportedGoTypes(t *testing.T) {
	type custom struct{ X int }
	r, err := Compare([]any{custom{1}}, []any{custom{2}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := brief(r); !reflect.DeepEqual(got, []string{"changed .[0]"}) {
		t.Errorf("got %q", got)
	}
}

func TestCompareBytesErrors(t *testing.T) {
	if _, err := CompareBytes([]byte(`{`), []byte(`{}`), nil); err == nil || !strings.HasPrefix(err.Error(), "expected:") {
		t.Errorf("got %v", err)
	}
	if _, err := CompareBytes([]byte(`{}`), []byte(`{`), nil); err == nil || !strings.HasPrefix(err.Error(), "actual:") {
		t.Errorf("got %v", err)
	}
}

func TestToleranceValidation(t *testing.T) {
	for _, tol := range []Tolerance{{Abs: -0.1}, {Rel: -1}} {
		if err := (&Options{Default: tol}).Validate(); err == nil {
			t.Errorf("%+v: expected error", tol)
		}
	}
	if err := (&Options{Default: Tolerance{Abs: 0.1, Rel: 0.2}}).Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPathString(t *testing.T) {
	tests := []struct {
		p    Path
		want string
	}{
		{Path{}, "."},
		{Path{0}, ".[0]"},
		{Path{"a", 1, "b"}, ".a[1].b"},
		{Path{"a b"}, `.["a b"]`},
		{Path{"x", "a-b", 2}, `.x["a-b"][2]`},
		{Path{"_ok9"}, "._ok9"},
		{Path{"9no"}, `.["9no"]`},
	}
	for _, tt := range tests {
		if got := tt.p.String(); got != tt.want {
			t.Errorf("%#v: got %s, want %s", tt.p, got, tt.want)
		}
	}
}
