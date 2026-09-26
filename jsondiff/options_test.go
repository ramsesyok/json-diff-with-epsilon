package jsondiff

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadConfig(t *testing.T) {
	want := &Options{
		Default:    Tolerance{Abs: 1e-6},
		Ignore:     []string{".meta.timestamp", ".. | .requestId?"},
		Unordered:  []string{".tags"},
		Tolerances: []ToleranceRule{{Path: ".items[].price", Abs: 0.01}, {Path: ".stats", Rel: 0.001}},
	}
	yamlCfg := `default:
  abs: 1e-6
ignore:
  - .meta.timestamp
  - .. | .requestId?
unordered:
  - .tags
tolerances:
  - path: .items[].price
    abs: 0.01
  - path: .stats
    rel: 0.001
`
	jsonCfg := `{
  "default": {"abs": 1e-6},
  "ignore": [".meta.timestamp", ".. | .requestId?"],
  "unordered": [".tags"],
  "tolerances": [{"path": ".items[].price", "abs": 0.01}, {"path": ".stats", "rel": 0.001}]
}`
	for _, tc := range []struct{ name, content string }{
		{"rules.yaml", yamlCfg},
		{"rules.yml", yamlCfg},
		{"rules.json", jsonCfg},
		{"rules.conf", yamlCfg},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := LoadConfig(writeFile(t, tc.name, tc.content))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("got %+v", got)
			}
		})
	}
}

func TestLoadConfigEmpty(t *testing.T) {
	got, err := LoadConfig(writeFile(t, "empty.yaml", ""))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, &Options{}) {
		t.Errorf("got %+v", got)
	}
}

func TestLoadConfigErrors(t *testing.T) {
	tests := []struct{ name, content, want string }{
		{"unknown.yaml", "epsilon: 1\n", "epsilon"},
		{"unknown.json", `{"epsilon": 1}`, "epsilon"},
		{"negative.yaml", "default:\n  abs: -1\n", "non-negative"},
		{"missing-path.yaml", "tolerances:\n  - abs: 1\n", "empty path"},
		{"bad-jq.yaml", "ignore:\n  - '.a['\n", "invalid path"},
		{"bad.json", `{`, "unexpected EOF"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadConfig(writeFile(t, tt.name, tt.content))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("got %v, want error containing %q", err, tt.want)
			}
		})
	}
	if _, err := LoadConfig(filepath.Join(t.TempDir(), "none.yaml")); err == nil {
		t.Error("missing file: expected error")
	}
}
