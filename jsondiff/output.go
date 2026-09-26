package jsondiff

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const (
	colorReset   = "\x1b[0m"
	colorRed     = "\x1b[31m"
	colorGreen   = "\x1b[32m"
	colorYellow  = "\x1b[33m"
	colorMagenta = "\x1b[35m"
)

var kindMarks = map[Kind]struct{ mark, color string }{
	KindChanged:      {"~", colorYellow},
	KindRemoved:      {"-", colorRed},
	KindAdded:        {"+", colorGreen},
	KindTypeMismatch: {"!", colorMagenta},
}

// WriteText writes one line per difference followed by a summary line.
// Nothing is written when the documents are equal; use WriteSummary for that.
func (r *Result) WriteText(w io.Writer, color bool) error {
	if r.Equal {
		return nil
	}
	var b strings.Builder
	for _, d := range r.Differences {
		line := d.text()
		if color {
			line = kindMarks[d.Kind].color + line + colorReset
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	if _, err := io.WriteString(w, b.String()); err != nil {
		return err
	}
	return r.WriteSummary(w)
}

// WriteSummary writes the summary line, for example
// "2 differences (compared 10 values, ignored 1)".
func (r *Result) WriteSummary(w io.Writer) error {
	noun := "differences"
	if r.Summary.Differences == 1 {
		noun = "difference"
	}
	_, err := fmt.Fprintf(w, "%d %s (compared %d values, ignored %d)\n",
		r.Summary.Differences, noun, r.Summary.Compared, r.Summary.Ignored)
	return err
}

// WriteJSON writes the result as indented JSON.
func (r *Result) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func (d Difference) text() string {
	mark := kindMarks[d.Kind].mark
	switch d.Kind {
	case KindRemoved:
		return fmt.Sprintf("%s %s: %s", mark, d.Path, formatValue(d.Expected))
	case KindAdded:
		return fmt.Sprintf("%s %s: %s", mark, d.Path, formatValue(d.Actual))
	case KindTypeMismatch:
		return fmt.Sprintf("%s %s: %s (%s) → %s (%s)", mark, d.Path,
			formatValue(d.Expected), typeOf(d.Expected), formatValue(d.Actual), typeOf(d.Actual))
	}
	s := fmt.Sprintf("%s %s: %s → %s", mark, d.Path, formatValue(d.Expected), formatValue(d.Actual))
	if d.Diff != nil && d.Tolerance != nil {
		rule := strconv.Quote(d.Tolerance.Rule)
		if d.Tolerance.Rule == DefaultRule {
			rule = DefaultRule
		}
		s += fmt.Sprintf(" (diff=%s, tolerance: abs=%s rel=%s by %s)",
			formatFloat(*d.Diff), formatFloat(d.Tolerance.Abs), formatFloat(d.Tolerance.Rel), rule)
	}
	return s
}

func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// formatValue renders a value as compact JSON.
func formatValue(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return fmt.Sprint(v)
	}
	return strings.TrimSuffix(buf.String(), "\n")
}

type kv struct {
	key   string
	value any
}

// orderedObject marshals to a JSON object keeping the key order.
type orderedObject []kv

func (o orderedObject) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, e := range o {
		if i > 0 {
			buf.WriteByte(',')
		}
		k, _ := json.Marshal(e.key)
		buf.Write(k)
		buf.WriteByte(':')
		var vb bytes.Buffer
		enc := json.NewEncoder(&vb)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(e.value); err != nil {
			return nil, err
		}
		buf.Write(bytes.TrimSuffix(vb.Bytes(), []byte("\n")))
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}
