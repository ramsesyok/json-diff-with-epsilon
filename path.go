package jsondiff

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/itchyny/gojq"
)

// Path is the location of a node: a sequence of object keys (string) and
// array indexes (int).
type Path []any

var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// String formats the path as a jq path expression such as .items[0].price,
// or . for the root.
func (p Path) String() string {
	if len(p) == 0 {
		return "."
	}
	var b strings.Builder
	for _, e := range p {
		switch e := e.(type) {
		case string:
			if identRe.MatchString(e) {
				b.WriteString(".")
				b.WriteString(e)
			} else {
				if b.Len() == 0 {
					b.WriteString(".")
				}
				q, _ := json.Marshal(e)
				b.WriteString("[")
				b.Write(q)
				b.WriteString("]")
			}
		case int:
			if b.Len() == 0 {
				b.WriteString(".")
			}
			b.WriteString("[")
			b.WriteString(strconv.Itoa(e))
			b.WriteString("]")
		}
	}
	return b.String()
}

func (p Path) child(e any) Path {
	c := make(Path, len(p), len(p)+1)
	copy(c, p)
	return append(c, e)
}

// pathQuery is a compiled jq path expression.
type pathQuery struct {
	src  string
	code *gojq.Code
}

func compilePathQuery(src string) (*pathQuery, error) {
	if strings.TrimSpace(src) == "" {
		return nil, fmt.Errorf("empty path")
	}
	// Parse the expression alone first so that syntax errors do not refer
	// to the path() wrapper.
	if _, err := gojq.Parse(src); err != nil {
		return nil, fmt.Errorf("invalid path %q: %w", src, err)
	}
	q, err := gojq.Parse("path(" + src + ")")
	if err != nil {
		return nil, fmt.Errorf("invalid path %q: %w", src, err)
	}
	code, err := gojq.Compile(q)
	if err != nil {
		return nil, fmt.Errorf("invalid path %q: %w", src, err)
	}
	return &pathQuery{src: src, code: code}, nil
}

// pathSet holds the formatted paths a query matched in one document.
type pathSet map[string]struct{}

// eval returns the paths the query matches in doc, which must already be
// converted with toJQ. A runtime error caused by the document's shape (for
// example iterating over null) ends the evaluation and keeps the paths found
// so far. An expression that is not a path expression, such as 1 or
// .a | tostring, is reported as an error.
func (q *pathQuery) eval(doc any) (pathSet, error) {
	set := pathSet{}
	iter := q.code.Run(doc)
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if err, isErr := v.(error); isErr {
			if strings.HasPrefix(err.Error(), "invalid path") {
				return nil, fmt.Errorf("%q is not a path expression: %w", q.src, err)
			}
			break
		}
		raw, ok := v.([]any)
		if !ok {
			continue
		}
		p, ok := fromJQPath(raw)
		if !ok {
			continue
		}
		set[p.String()] = struct{}{}
	}
	return set, nil
}

func fromJQPath(raw []any) (Path, bool) {
	p := make(Path, 0, len(raw))
	for _, e := range raw {
		switch e := e.(type) {
		case string:
			p = append(p, e)
		case int:
			p = append(p, e)
		case float64:
			if e != math.Trunc(e) {
				return nil, false
			}
			p = append(p, int(e))
		default:
			// slices such as .[1:3] cannot be mapped to a single node
			return nil, false
		}
	}
	return p, true
}

// toJQ converts a decoded JSON value into the representation gojq accepts.
func toJQ(v any) any {
	switch v := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(v))
		for k, e := range v {
			m[k] = toJQ(e)
		}
		return m
	case []any:
		a := make([]any, len(v))
		for i, e := range v {
			a[i] = toJQ(e)
		}
		return a
	case json.Number:
		if i, err := strconv.Atoi(string(v)); err == nil {
			return i
		}
		f, _ := strconv.ParseFloat(string(v), 64)
		return f
	case float32:
		return float64(v)
	case int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		f, _ := strconv.ParseFloat(fmt.Sprint(v), 64)
		return f
	default:
		return v
	}
}
