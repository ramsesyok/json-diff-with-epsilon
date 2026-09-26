// Package jsondiff compares two JSON documents, treating numbers as equal when
// they are within a configurable absolute or relative tolerance.
//
// Paths used by Options (Ignore, Unordered and Tolerances) are jq path
// expressions, the same syntax runn uses for the ignorePaths argument of its
// compare and diff functions.
package jsondiff

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Tolerance is the allowed difference between two numbers. Two numbers are
// equal when |expected-actual| <= Abs or |expected-actual| <= Rel*|expected|.
type Tolerance struct {
	Abs float64 `json:"abs" yaml:"abs"`
	Rel float64 `json:"rel" yaml:"rel"`
}

// ToleranceRule applies a tolerance to the nodes matched by Path and to all of
// their descendants. An omitted Abs or Rel is zero; it is not inherited from
// Options.Default.
type ToleranceRule struct {
	Path string  `json:"path" yaml:"path"`
	Abs  float64 `json:"abs" yaml:"abs"`
	Rel  float64 `json:"rel" yaml:"rel"`
}

// Options controls how two documents are compared.
type Options struct {
	// Default is the tolerance for numbers matched by no rule in Tolerances.
	Default Tolerance `json:"default" yaml:"default"`
	// Ignore lists paths that are not compared, including their descendants.
	Ignore []string `json:"ignore" yaml:"ignore"`
	// Unordered lists paths of arrays whose element order does not matter.
	// It applies to the matched arrays only, not to arrays nested in them.
	Unordered []string `json:"unordered" yaml:"unordered"`
	// Tolerances lists per-path tolerances. When several rules match a number,
	// the rule matching the deepest path wins, and among rules matching at
	// the same depth the last one wins.
	Tolerances []ToleranceRule `json:"tolerances" yaml:"tolerances"`
}

// LoadConfig reads Options from a file. Files with a .json extension are read
// as JSON and all others as YAML. Unknown keys are rejected.
func LoadConfig(path string) (*Options, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var opts *Options
	if strings.EqualFold(filepath.Ext(path), ".json") {
		opts, err = ParseJSONConfig(data)
	} else {
		opts, err = ParseYAMLConfig(data)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return opts, nil
}

// ParseYAMLConfig parses Options from YAML. Unknown keys are rejected.
func ParseYAMLConfig(data []byte) (*Options, error) {
	opts := &Options{}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(opts); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	return opts, nil
}

// ParseJSONConfig parses Options from JSON. Unknown keys are rejected.
func ParseJSONConfig(data []byte) (*Options, error) {
	opts := &Options{}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(opts); err != nil {
		return nil, err
	}
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	return opts, nil
}

// Validate reports negative tolerances, empty paths and paths that are not
// valid jq expressions.
func (o *Options) Validate() error {
	_, err := compile(o)
	return err
}

func validateTolerance(name string, abs, rel float64) error {
	if !(abs >= 0) || !(rel >= 0) || math.IsInf(abs, 0) || math.IsInf(rel, 0) {
		return fmt.Errorf("%s: tolerance must be a finite non-negative number (abs=%v, rel=%v)", name, abs, rel)
	}
	return nil
}
