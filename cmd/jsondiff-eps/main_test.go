package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fixture struct{ dir string }

func newFixture(t *testing.T, files map[string]string) fixture {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return fixture{dir}
}

func (f fixture) path(name string) string { return filepath.Join(f.dir, name) }

type outcome struct {
	code           int
	stdout, stderr string
}

func runCLI(stdin string, tty bool, args ...string) outcome {
	var out, errOut bytes.Buffer
	code := run(args, strings.NewReader(stdin), &out, &errOut, tty)
	return outcome{code, out.String(), errOut.String()}
}

func TestRun(t *testing.T) {
	f := newFixture(t, map[string]string{
		"e.json":     `{"a":1.0,"b":"x","ts":1,"tags":["p","q"]}`,
		"same.json":  `{"tags":["p","q"],"ts":1,"b":"x","a":1}`,
		"close.json": `{"a":1.0005,"b":"x","ts":2,"tags":["q","p"]}`,
		"diff.json":  `{"a":2,"b":"y","ts":1,"tags":["p","q"]}`,
		"bad.json":   `{"a":`,
		"rules.yaml": "default:\n  abs: 0.001\nignore:\n  - .ts\nunordered:\n  - .tags\n",
		"bad.yaml":   "epsilon: 1\n",
	})
	e := f.path("e.json")
	t.Setenv("NO_COLOR", "")

	tests := []struct {
		name   string
		stdin  string
		tty    bool
		args   []string
		code   int
		stdout string // substring; "" means stdout must be empty
		stderr string // substring; "" means stderr must be empty
	}{
		{name: "equal", args: []string{e, f.path("same.json")}, code: 0},
		{name: "equal verbose", args: []string{"-v", e, f.path("same.json")}, code: 0,
			stdout: "0 differences (compared 5 values, ignored 0)"},
		{name: "within tolerance with config", args: []string{"-c", f.path("rules.yaml"), e, f.path("close.json")}, code: 0},
		{name: "flags after arguments", args: []string{e, f.path("close.json"), "--config", f.path("rules.yaml")}, code: 0},
		{name: "cli flags", args: []string{"--abs", "0.001", "--ignore", ".ts", "--unordered", ".tags", e, f.path("close.json")}, code: 0},
		{name: "cli abs overrides config", args: []string{"-c", f.path("rules.yaml"), "--abs", "0", e, f.path("close.json")}, code: 1,
			stdout: "~ .a: 1.0 → 1.0005 (diff=0.0005, tolerance: abs=0 rel=0 by default)"},
		{name: "cli rel", args: []string{"--rel", "0.001", "--ignore", ".ts", "--unordered", ".tags", e, f.path("close.json")}, code: 0},
		{name: "differences", args: []string{e, f.path("diff.json")}, code: 1,
			stdout: "2 differences (compared 5 values, ignored 0)"},
		{name: "stdin as actual", stdin: `{"a":1,"b":"x","ts":1,"tags":["p","q"]}`, args: []string{e, "-"}, code: 0},
		{name: "stdin as expected", stdin: `{"a":2}`, args: []string{"-", e}, code: 1, stdout: "~ .a: 2 → 1.0"},
		{name: "quiet", args: []string{"-q", e, f.path("diff.json")}, code: 1},
		{name: "no color when not a tty", args: []string{e, f.path("diff.json")}, code: 1, stdout: "~ .a"},
		{name: "color on tty", tty: true, args: []string{e, f.path("diff.json")}, code: 1, stdout: "\x1b[33m~ .a"},
		{name: "no-color on tty", tty: true, args: []string{"--no-color", e, f.path("diff.json")}, code: 1, stdout: "\n~ .b"},
		{name: "forced color", args: []string{"--color", e, f.path("diff.json")}, code: 1, stdout: "\x1b[33m"},
		{name: "json equal", args: []string{"--format", "json", e, f.path("same.json")}, code: 0, stdout: `"equal": true`},
		{name: "json diff", args: []string{"--format", "json", e, f.path("diff.json")}, code: 1, stdout: `"kind": "changed"`},
		{name: "version", args: []string{"--version"}, code: 0, stdout: "jsondiff-eps dev"},
		{name: "help", args: []string{"-h"}, code: 0, stderr: "Usage:"},

		{name: "missing argument", args: []string{e}, code: 2, stderr: "expected 2 arguments"},
		{name: "both stdin", args: []string{"-", "-"}, code: 2, stderr: "only one of"},
		{name: "unknown flag", args: []string{"--nope", e, e}, code: 2, stderr: "flag provided but not defined"},
		{name: "bad format", args: []string{"--format", "xml", e, e}, code: 2, stderr: "--format"},
		{name: "bad abs", args: []string{"--abs", "x", e, e}, code: 2, stderr: "must be a number"},
		{name: "negative abs", args: []string{"--abs", "-1", e, e}, code: 2, stderr: "non-negative"},
		{name: "missing file", args: []string{e, f.path("none.json")}, code: 2, stderr: "actual: open"},
		{name: "invalid json", args: []string{f.path("bad.json"), e}, code: 2, stderr: "expected:"},
		{name: "invalid stdin", stdin: "", args: []string{e, "-"}, code: 2, stderr: "empty input"},
		{name: "bad config", args: []string{"-c", f.path("bad.yaml"), e, e}, code: 2, stderr: "config:"},
		{name: "bad jq path", args: []string{"--ignore", ".[", e, e}, code: 2, stderr: "invalid path"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runCLI(tt.stdin, tt.tty, tt.args...)
			if got.code != tt.code {
				t.Errorf("exit code = %d, want %d\nstdout: %s\nstderr: %s", got.code, tt.code, got.stdout, got.stderr)
			}
			check := func(name, got, want string) {
				if want == "" && got != "" {
					t.Errorf("%s should be empty, got %q", name, got)
				}
				if !strings.Contains(got, want) {
					t.Errorf("%s = %q, want substring %q", name, got, want)
				}
			}
			check("stdout", got.stdout, tt.stdout)
			check("stderr", got.stderr, tt.stderr)
		})
	}
}

func TestRunJSONOutputIsParseable(t *testing.T) {
	f := newFixture(t, map[string]string{
		"e.json": `{"items":[{"price":100.5}]}`,
		"a.json": `{"items":[{"price":100.62}]}`,
	})
	got := runCLI("", false, "--format", "json", f.path("e.json"), f.path("a.json"))
	var res struct {
		Equal   bool `json:"equal"`
		Summary struct {
			Differences int `json:"differences"`
		} `json:"summary"`
	}
	if err := json.Unmarshal([]byte(got.stdout), &res); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, got.stdout)
	}
	if res.Equal || res.Summary.Differences != 1 {
		t.Errorf("got %+v", res)
	}
}
