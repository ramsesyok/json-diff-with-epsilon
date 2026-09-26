// Command jsondiff-eps compares two JSON documents with numeric tolerance.
//
// Exit status is 0 when the documents are equal, 1 when they differ and 2 on
// errors.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strconv"
	"strings"

	jsondiff "github.com/ramsesyok/json-diff-with-epsilon"
)

const (
	exitEqual = 0
	exitDiff  = 1
	exitError = 2
)

// version is set by the release build with -ldflags "-X main.version=...".
var version = "dev"

// currentVersion returns version, or for a binary built with
// "go install ...@vX.Y.Z" the module version recorded in the build info.
func currentVersion() string {
	if version != "dev" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return version
}

const usage = `Usage: jsondiff-eps [flags] <expected> <actual>

Compare two JSON documents, treating numbers within a tolerance as equal.
Either <expected> or <actual> may be "-" to read it from standard input.
Paths are jq path expressions, as in runn's compare and diff functions.

Exit status: 0 equal, 1 different, 2 error.

Flags:
  -c, --config <file>     config file (.yaml/.yml or .json)
      --abs <float>       default absolute tolerance (overrides the config)
      --rel <float>       default relative tolerance (overrides the config)
      --ignore <path>     path to ignore (repeatable, added to the config)
      --unordered <path>  array path whose order is ignored (repeatable, added to the config)
      --format <format>   output format: text or json (default text)
      --color             force colored output
      --no-color          disable colored output
  -q, --quiet             print nothing, only set the exit status
  -v, --verbose           print the summary line even when equal (text format)
      --version           print the version
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, isTerminal(os.Stdout)))
}

type stringList []string

func (l *stringList) String() string     { return strings.Join(*l, ", ") }
func (l *stringList) Set(v string) error { *l = append(*l, v); return nil }

// floatFlag remembers whether it was set so that it overrides the config
// only when given.
type floatFlag struct {
	v   float64
	set bool
}

func (f *floatFlag) String() string { return strconv.FormatFloat(f.v, 'g', -1, 64) }
func (f *floatFlag) Set(s string) error {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return errors.New("must be a number")
	}
	f.v, f.set = v, true
	return nil
}

type cliOptions struct {
	config    string
	abs, rel  floatFlag
	ignore    stringList
	unordered stringList
	format    string
	color     bool
	noColor   bool
	quiet     bool
	verbose   bool
	version   bool
	args      []string
}

func parseArgs(args []string, stderr io.Writer) (*cliOptions, error) {
	o := &cliOptions{}
	fs := flag.NewFlagSet("jsondiff-eps", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.config, "c", "", "")
	fs.StringVar(&o.config, "config", "", "")
	fs.Var(&o.abs, "abs", "")
	fs.Var(&o.rel, "rel", "")
	fs.Var(&o.ignore, "ignore", "")
	fs.Var(&o.unordered, "unordered", "")
	fs.StringVar(&o.format, "format", "text", "")
	fs.BoolVar(&o.color, "color", false, "")
	fs.BoolVar(&o.noColor, "no-color", false, "")
	fs.BoolVar(&o.quiet, "q", false, "")
	fs.BoolVar(&o.quiet, "quiet", false, "")
	fs.BoolVar(&o.verbose, "v", false, "")
	fs.BoolVar(&o.verbose, "verbose", false, "")
	fs.BoolVar(&o.version, "version", false, "")

	// Allow flags after the positional arguments, e.g. "a.json b.json -q".
	for {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				fmt.Fprint(stderr, usage)
			}
			return nil, err
		}
		if fs.NArg() == 0 {
			break
		}
		o.args = append(o.args, fs.Arg(0))
		args = fs.Args()[1:]
	}
	return o, nil
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer, tty bool) int {
	o, err := parseArgs(args, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitEqual
		}
		return fail(stderr, "%v\n\n%s", err, usage)
	}
	if o.version {
		fmt.Fprintln(stdout, "jsondiff-eps", currentVersion())
		return exitEqual
	}
	if len(o.args) != 2 {
		return fail(stderr, "expected 2 arguments, got %d\n\n%s", len(o.args), usage)
	}
	if o.args[0] == "-" && o.args[1] == "-" {
		return fail(stderr, "only one of <expected> and <actual> can be \"-\"")
	}
	if o.format != "text" && o.format != "json" {
		return fail(stderr, "--format must be text or json, got %q", o.format)
	}

	opts := &jsondiff.Options{}
	if o.config != "" {
		if opts, err = jsondiff.LoadConfig(o.config); err != nil {
			return fail(stderr, "config: %v", err)
		}
	}
	if o.abs.set {
		opts.Default.Abs = o.abs.v
	}
	if o.rel.set {
		opts.Default.Rel = o.rel.v
	}
	opts.Ignore = append(opts.Ignore, o.ignore...)
	opts.Unordered = append(opts.Unordered, o.unordered...)

	expected, err := readDoc(o.args[0], stdin)
	if err != nil {
		return fail(stderr, "expected: %v", err)
	}
	actual, err := readDoc(o.args[1], stdin)
	if err != nil {
		return fail(stderr, "actual: %v", err)
	}

	res, err := jsondiff.Compare(expected, actual, opts)
	if err != nil {
		return fail(stderr, "%v", err)
	}

	code := exitEqual
	if !res.Equal {
		code = exitDiff
	}
	if o.quiet {
		return code
	}
	switch {
	case o.format == "json":
		err = res.WriteJSON(stdout)
	case res.Equal && o.verbose:
		err = res.WriteSummary(stdout)
	default:
		color := tty && os.Getenv("NO_COLOR") == ""
		if o.color {
			color = true
		}
		if o.noColor {
			color = false
		}
		err = res.WriteText(stdout, color)
	}
	if err != nil {
		return fail(stderr, "%v", err)
	}
	return code
}

func readDoc(name string, stdin io.Reader) (any, error) {
	if name == "-" {
		return jsondiff.DecodeJSON(stdin)
	}
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	v, err := jsondiff.DecodeJSON(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return v, nil
}

func fail(stderr io.Writer, format string, a ...any) int {
	fmt.Fprintf(stderr, "jsondiff-eps: "+format+"\n", a...)
	return exitError
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
