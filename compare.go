package jsondiff

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"reflect"
	"sort"
	"strconv"
)

// Kind is the kind of a Difference.
type Kind string

const (
	// KindChanged means both documents have a value of the same type at the
	// path, but the values differ.
	KindChanged Kind = "changed"
	// KindRemoved means the path exists only in the expected document.
	KindRemoved Kind = "removed"
	// KindAdded means the path exists only in the actual document.
	KindAdded Kind = "added"
	// KindTypeMismatch means the values at the path have different JSON types.
	KindTypeMismatch Kind = "type_mismatch"
)

// DefaultRule is the AppliedTolerance.Rule of numbers matched by no rule.
const DefaultRule = "default"

// AppliedTolerance is the tolerance used to compare two numbers and the rule
// it came from: the Path of a ToleranceRule, or DefaultRule.
type AppliedTolerance struct {
	Abs  float64 `json:"abs"`
	Rel  float64 `json:"rel"`
	Rule string  `json:"rule"`
}

// Difference is one difference between the documents.
type Difference struct {
	// Path is the jq path of the node. Inside unordered arrays, removed and
	// changed nodes use expected indexes and added nodes use actual indexes.
	Path string
	Kind Kind
	// Expected is unset for KindAdded and Actual is unset for KindRemoved.
	Expected any
	Actual   any
	// Diff and Tolerance are set only for numbers of KindChanged.
	Diff      *float64
	Tolerance *AppliedTolerance
}

// MarshalJSON omits the fields that do not apply to the Kind, while still
// emitting a JSON null value when one of the documents holds null.
func (d Difference) MarshalJSON() ([]byte, error) {
	m := orderedObject{{"path", d.Path}, {"kind", d.Kind}}
	if d.Kind != KindAdded {
		m = append(m, kv{"expected", d.Expected})
	}
	if d.Kind != KindRemoved {
		m = append(m, kv{"actual", d.Actual})
	}
	if d.Kind == KindTypeMismatch {
		m = append(m, kv{"expected_type", typeOf(d.Expected)}, kv{"actual_type", typeOf(d.Actual)})
	}
	if d.Diff != nil {
		m = append(m, kv{"diff", *d.Diff})
	}
	if d.Tolerance != nil {
		m = append(m, kv{"tolerance", d.Tolerance})
	}
	return m.MarshalJSON()
}

// Summary counts what the comparison did.
type Summary struct {
	Differences int `json:"differences"`
	// Compared is the number of scalar value pairs compared.
	Compared int `json:"compared"`
	// Ignored is the number of nodes skipped because they matched Ignore.
	Ignored int `json:"ignored"`
}

// Result is the outcome of a comparison.
type Result struct {
	Equal       bool         `json:"equal"`
	Differences []Difference `json:"differences"`
	Summary     Summary      `json:"summary"`
}

// DecodeJSON decodes a single JSON value, keeping numbers as json.Number so
// that they can be compared without rounding.
func DecodeJSON(r io.Reader) (any, error) {
	dec := json.NewDecoder(r)
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("empty input")
		}
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("unexpected data after the JSON value")
	}
	return v, nil
}

// CompareBytes decodes two JSON documents and compares them.
func CompareBytes(expected, actual []byte, opts *Options) (*Result, error) {
	e, err := DecodeJSON(bytes.NewReader(expected))
	if err != nil {
		return nil, fmt.Errorf("expected: %w", err)
	}
	a, err := DecodeJSON(bytes.NewReader(actual))
	if err != nil {
		return nil, fmt.Errorf("actual: %w", err)
	}
	return Compare(e, a, opts)
}

// Compare compares two decoded JSON values. Values are best decoded with
// DecodeJSON (or json.Decoder.UseNumber) so that numbers keep their exact
// literal, but float64 and other Go number types are accepted too.
// A nil opts compares without tolerance and without ignored paths.
func Compare(expected, actual any, opts *Options) (*Result, error) {
	if opts == nil {
		opts = &Options{}
	}
	c, err := compile(opts)
	if err != nil {
		return nil, err
	}
	m, err := c.match(toJQ(expected), toJQ(actual))
	if err != nil {
		return nil, err
	}
	w := &walker{m: m}
	w.walk(expected, actual, Path{}, Path{}, AppliedTolerance{
		Abs: opts.Default.Abs, Rel: opts.Default.Rel, Rule: DefaultRule,
	})
	diffs := w.diffs
	if diffs == nil {
		diffs = []Difference{}
	}
	return &Result{
		Equal:       len(diffs) == 0,
		Differences: diffs,
		Summary:     Summary{Differences: len(diffs), Compared: w.compared, Ignored: w.ignored},
	}, nil
}

type compiledRule struct {
	rule ToleranceRule
	q    *pathQuery
}

type compiled struct {
	ignore     []*pathQuery
	unordered  []*pathQuery
	tolerances []compiledRule
}

func compile(o *Options) (*compiled, error) {
	if err := validateTolerance("default", o.Default.Abs, o.Default.Rel); err != nil {
		return nil, err
	}
	c := &compiled{}
	for _, p := range o.Ignore {
		q, err := compilePathQuery(p)
		if err != nil {
			return nil, fmt.Errorf("ignore: %w", err)
		}
		c.ignore = append(c.ignore, q)
	}
	for _, p := range o.Unordered {
		q, err := compilePathQuery(p)
		if err != nil {
			return nil, fmt.Errorf("unordered: %w", err)
		}
		c.unordered = append(c.unordered, q)
	}
	for i, r := range o.Tolerances {
		name := fmt.Sprintf("tolerances[%d]", i)
		if err := validateTolerance(name, r.Abs, r.Rel); err != nil {
			return nil, err
		}
		q, err := compilePathQuery(r.Path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		c.tolerances = append(c.tolerances, compiledRule{rule: r, q: q})
	}
	return c, nil
}

// sides holds the paths a set of queries matched in each document.
type sides struct{ exp, act pathSet }

func (s sides) has(pe, pa Path) bool {
	if pe != nil {
		if _, ok := s.exp[pe.String()]; ok {
			return true
		}
	}
	if pa != nil {
		if _, ok := s.act[pa.String()]; ok {
			return true
		}
	}
	return false
}

type ruleSides struct {
	rule ToleranceRule
	sides
}

type matcher struct {
	ignore     sides
	unordered  sides
	tolerances []ruleSides
}

// evalSides returns the union of the paths the queries match in e and in a.
func evalSides(qs []*pathQuery, e, a any) (sides, error) {
	s := sides{pathSet{}, pathSet{}}
	for _, q := range qs {
		for _, d := range []struct {
			doc any
			set pathSet
		}{{e, s.exp}, {a, s.act}} {
			found, err := q.eval(d.doc)
			if err != nil {
				return s, err
			}
			for p := range found {
				d.set[p] = struct{}{}
			}
		}
	}
	return s, nil
}

func (c *compiled) match(e, a any) (*matcher, error) {
	m := &matcher{}
	var err error
	if m.ignore, err = evalSides(c.ignore, e, a); err != nil {
		return nil, fmt.Errorf("ignore: %w", err)
	}
	if m.unordered, err = evalSides(c.unordered, e, a); err != nil {
		return nil, fmt.Errorf("unordered: %w", err)
	}
	for i, r := range c.tolerances {
		s, err := evalSides([]*pathQuery{r.q}, e, a)
		if err != nil {
			return nil, fmt.Errorf("tolerances[%d]: %w", i, err)
		}
		m.tolerances = append(m.tolerances, ruleSides{r.rule, s})
	}
	return m, nil
}

// at returns the tolerance for the node, given the one inherited from its
// parent: a rule matching the node itself overrides the inherited one
// because it is deeper, and among rules matching the node the last one wins.
func (m *matcher) at(pe, pa Path, tol AppliedTolerance) AppliedTolerance {
	for _, r := range m.tolerances {
		if r.has(pe, pa) {
			tol = AppliedTolerance{Abs: r.rule.Abs, Rel: r.rule.Rel, Rule: r.rule.Path}
		}
	}
	return tol
}

type walker struct {
	m        *matcher
	diffs    []Difference
	compared int
	ignored  int
}

func (w *walker) add(d Difference) { w.diffs = append(w.diffs, d) }

// only reports a node present in one document only, unless it is ignored.
// The ignore check uses both pe and pa when the node has a counterpart path
// in the other document (object keys, ordered array indexes); either may be
// nil otherwise.
func (w *walker) only(kind Kind, v any, pe, pa Path) {
	if w.m.ignore.has(pe, pa) {
		w.ignored++
		return
	}
	if kind == KindRemoved {
		w.add(Difference{Path: pe.String(), Kind: kind, Expected: v})
	} else {
		w.add(Difference{Path: pa.String(), Kind: kind, Actual: v})
	}
}

func (w *walker) walk(e, a any, pe, pa Path, tol AppliedTolerance) {
	if w.m.ignore.has(pe, pa) {
		w.ignored++
		return
	}
	tol = w.m.at(pe, pa, tol)

	te, ta := typeOf(e), typeOf(a)
	if te != ta {
		w.compared++
		w.add(Difference{Path: pe.String(), Kind: KindTypeMismatch, Expected: e, Actual: a})
		return
	}

	switch te {
	case "object":
		w.walkObject(e.(map[string]any), a.(map[string]any), pe, pa, tol)
	case "array":
		ea, aa := e.([]any), a.([]any)
		if w.m.unordered.has(pe, pa) {
			w.walkUnordered(ea, aa, pe, pa, tol)
		} else {
			w.walkOrdered(ea, aa, pe, pa, tol)
		}
	case "number":
		w.compared++
		w.compareNumbers(e, a, pe, tol)
	default:
		w.compared++
		if !reflect.DeepEqual(e, a) {
			w.add(Difference{Path: pe.String(), Kind: KindChanged, Expected: e, Actual: a})
		}
	}
}

func (w *walker) walkObject(e, a map[string]any, pe, pa Path, tol AppliedTolerance) {
	keys := make([]string, 0, len(e)+len(a))
	for k := range e {
		keys = append(keys, k)
	}
	for k := range a {
		if _, ok := e[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		ce, okE := e[k]
		ca, okA := a[k]
		switch {
		case okE && okA:
			w.walk(ce, ca, pe.child(k), pa.child(k), tol)
		case okE:
			w.only(KindRemoved, ce, pe.child(k), pa.child(k))
		default:
			w.only(KindAdded, ca, pe.child(k), pa.child(k))
		}
	}
}

func (w *walker) walkOrdered(e, a []any, pe, pa Path, tol AppliedTolerance) {
	for i := 0; i < len(e) || i < len(a); i++ {
		switch {
		case i < len(e) && i < len(a):
			w.walk(e[i], a[i], pe.child(i), pa.child(i), tol)
		case i < len(e):
			w.only(KindRemoved, e[i], pe.child(i), pa.child(i))
		default:
			w.only(KindAdded, a[i], pe.child(i), pa.child(i))
		}
	}
}

// walkUnordered pairs each expected element with the first unpaired actual
// element that compares equal, then reports the elements left unpaired.
func (w *walker) walkUnordered(e, a []any, pe, pa Path, tol AppliedTolerance) {
	paired := make([]bool, len(a))
	var unpaired []int
	for i := range e {
		found := false
		for j := range a {
			if paired[j] {
				continue
			}
			t := &walker{m: w.m}
			t.walk(e[i], a[j], pe.child(i), pa.child(j), tol)
			if len(t.diffs) == 0 {
				paired[j] = true
				w.compared += t.compared
				w.ignored += t.ignored
				found = true
				break
			}
		}
		if !found {
			unpaired = append(unpaired, i)
		}
	}
	for _, i := range unpaired {
		w.only(KindRemoved, e[i], pe.child(i), nil)
	}
	for j := range a {
		if !paired[j] {
			w.only(KindAdded, a[j], nil, pa.child(j))
		}
	}
}

func (w *walker) compareNumbers(e, a any, pe Path, tol AppliedTolerance) {
	re, okE := toRat(e)
	ra, okA := toRat(a)
	if !okE || !okA {
		w.add(Difference{Path: pe.String(), Kind: KindChanged, Expected: e, Actual: a})
		return
	}
	d := new(big.Rat).Sub(re, ra)
	d.Abs(d)
	if d.Sign() == 0 {
		return
	}
	abs := floatRat(tol.Abs)
	if d.Cmp(abs) <= 0 {
		return
	}
	limit := new(big.Rat).Abs(re)
	limit.Mul(limit, floatRat(tol.Rel))
	if d.Cmp(limit) <= 0 {
		return
	}
	df, _ := d.Float64()
	w.add(Difference{Path: pe.String(), Kind: KindChanged, Expected: e, Actual: a, Diff: &df, Tolerance: &tol})
}

// toRat parses a number exactly from its literal.
func toRat(v any) (*big.Rat, bool) {
	s, ok := numberLiteral(v)
	if !ok {
		return nil, false
	}
	r, ok := new(big.Rat).SetString(s)
	return r, ok
}

// floatRat converts a tolerance to the decimal it was most likely written as
// (0.3 rather than 0.299999999999999988898), so that a difference of exactly
// the tolerance is accepted.
func floatRat(f float64) *big.Rat {
	r, _ := new(big.Rat).SetString(strconv.FormatFloat(f, 'g', -1, 64))
	return r
}

func numberLiteral(v any) (string, bool) {
	switch v := v.(type) {
	case json.Number:
		return string(v), true
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64), true
	case float32:
		return strconv.FormatFloat(float64(v), 'g', -1, 32), true
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprint(v), true
	}
	return "", false
}

func typeOf(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case map[string]any:
		return "object"
	case []any:
		return "array"
	}
	if _, ok := numberLiteral(v); ok {
		return "number"
	}
	return fmt.Sprintf("%T", v)
}
