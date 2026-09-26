package jsondiff

import (
	"bytes"
	"testing"
)

var fuzzSeeds = [][2]string{
	{`{"a":1,"b":[1,2,{"c":"x"}]}`, `{"b":[1,2,{"c":"y"}],"a":1.0}`},
	{`[1,2,3]`, `[3,2,1,0]`},
	{`{"a.b":{"":null}}`, `{"a.b":{"":false},"日本":1}`},
	{`12345678901234567890`, `1.2345678901234567890e19`},
	{`{"x":[[],{}]}`, `{"x":[{},[]]}`},
	{`"s"`, `null`},
}

func decodeOrSkip(t *testing.T, data []byte) any {
	v, err := DecodeJSON(bytes.NewReader(data))
	if err != nil {
		t.Skip()
	}
	return v
}

// FuzzCompare checks invariants that hold for any pair of JSON documents:
//   - a document is equal to itself;
//   - Equal and Summary agree with Differences;
//   - comparing in the other direction swaps added and removed and keeps
//     changed and type_mismatch;
//   - every reported path, used as an ignore path, removes that difference.
func FuzzCompare(f *testing.F) {
	for _, s := range fuzzSeeds {
		f.Add([]byte(s[0]), []byte(s[1]))
	}
	f.Fuzz(func(t *testing.T, eb, ab []byte) {
		e := decodeOrSkip(t, eb)
		a := decodeOrSkip(t, ab)

		self, err := Compare(e, e, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !self.Equal {
			t.Fatalf("document not equal to itself: %q", brief(self))
		}

		r, err := Compare(e, a, nil)
		if err != nil {
			t.Fatal(err)
		}
		if r.Equal != (len(r.Differences) == 0) || r.Summary.Differences != len(r.Differences) {
			t.Fatalf("inconsistent result: %+v", r)
		}

		rev, err := Compare(a, e, nil)
		if err != nil {
			t.Fatal(err)
		}
		count := func(r *Result) map[Kind]int {
			m := map[Kind]int{}
			for _, d := range r.Differences {
				m[d.Kind]++
			}
			return m
		}
		c, cr := count(r), count(rev)
		if c[KindChanged] != cr[KindChanged] || c[KindTypeMismatch] != cr[KindTypeMismatch] ||
			c[KindAdded] != cr[KindRemoved] || c[KindRemoved] != cr[KindAdded] {
			t.Fatalf("asymmetric: %v vs %v", c, cr)
		}

		for i, d := range r.Differences {
			if i >= 5 {
				break
			}
			r2, err := Compare(e, a, &Options{Ignore: []string{d.Path}})
			if err != nil {
				t.Fatalf("reported path %q is not a valid ignore path: %v", d.Path, err)
			}
			for _, d2 := range r2.Differences {
				if d2.Path == d.Path {
					t.Fatalf("ignoring %q did not remove it", d.Path)
				}
			}
		}
	})
}

// FuzzCompareUnordered checks that shuffling is not required for equality:
// an array compared with its reverse is equal when unordered.
func FuzzCompareUnordered(f *testing.F) {
	for _, s := range fuzzSeeds {
		f.Add([]byte(s[0]))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		arr, ok := decodeOrSkip(t, data).([]any)
		if !ok {
			t.Skip()
		}
		rev := make([]any, len(arr))
		for i, v := range arr {
			rev[len(arr)-1-i] = v
		}
		r, err := Compare(arr, rev, &Options{Unordered: []string{"."}})
		if err != nil {
			t.Fatal(err)
		}
		if !r.Equal {
			t.Fatalf("array not equal to its reverse: %q", brief(r))
		}
	})
}

// FuzzParseConfig checks that arbitrary config input never panics.
func FuzzParseConfig(f *testing.F) {
	f.Add([]byte("default:\n  abs: 1e-6\nignore:\n  - .a\ntolerances:\n  - path: .b\n    rel: 0.1\n"))
	f.Add([]byte(`{"unordered":[".t"],"tolerances":[{"path":".. | .x?","abs":1}]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if opts, err := ParseYAMLConfig(data); err == nil {
			if _, err := CompareBytes([]byte(`{"a":[1,{"b":2}]}`), []byte(`{"a":[1,{"b":3}]}`), opts); err != nil &&
				bytes.Contains([]byte(err.Error()), []byte("panic")) {
				t.Fatal(err)
			}
		}
		_, _ = ParseJSONConfig(data)
	})
}
