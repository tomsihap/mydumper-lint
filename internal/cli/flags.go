package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

// listFlag is a repeatable flag; each occurrence may hold comma-separated
// values ("--select MDL1,MDL306 --select MDL401").
type listFlag struct {
	values []string
	set    bool
}

func (l *listFlag) String() string { return strings.Join(l.values, ",") }

func (l *listFlag) Set(v string) error {
	l.set = true
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			l.values = append(l.values, p)
		}
	}
	return nil
}

// repeatFlag is a repeatable flag whose values are kept whole
// ("--format sarif=out,with,commas.sarif").
type repeatFlag struct{ values []string }

func (r *repeatFlag) String() string     { return strings.Join(r.values, " ") }
func (r *repeatFlag) Set(v string) error { r.values = append(r.values, v); return nil }

// errHelp is returned when -h or --help is given.
var errHelp = errors.New("help requested")

// parse parses args with fs, accepting flags anywhere among the positional
// arguments; "--" ends flag parsing. It returns the positional arguments.
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	fs.SetOutput(io.Discard)
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			return append(pos, args[i+1:]...), nil
		}
		if len(a) < 2 || a[0] != '-' || a == "-" {
			pos = append(pos, a)
			continue
		}
		name := strings.TrimLeft(a, "-")
		value, hasValue := "", false
		if k, v, ok := strings.Cut(name, "="); ok {
			name, value, hasValue = k, v, true
		}
		if name == "h" || name == "help" {
			return nil, errHelp
		}
		f := fs.Lookup(name)
		if f == nil {
			if s := suggestFlag(fs, name); s != "" {
				return nil, fmt.Errorf("unknown flag --%s (did you mean --%s?)", name, s)
			}
			return nil, fmt.Errorf("unknown flag --%s", name)
		}
		if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && bf.IsBoolFlag() && !hasValue {
			value, hasValue = "true", true
		}
		if !hasValue {
			if i+1 >= len(args) {
				return nil, fmt.Errorf("flag --%s needs a value", name)
			}
			i++
			value = args[i]
		}
		if err := fs.Set(name, value); err != nil {
			return nil, fmt.Errorf("invalid value %q for --%s: %w", value, name, err)
		}
	}
	return pos, nil
}

func suggestFlag(fs *flag.FlagSet, name string) string {
	best, bestD := "", 3
	fs.VisitAll(func(f *flag.Flag) {
		if d := levenshtein(name, f.Name); d < bestD {
			best, bestD = f.Name, d
		}
	})
	return best
}

func levenshtein(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

// flagUsage renders the flags of fs for help output.
func flagUsage(fs *flag.FlagSet) string {
	var b strings.Builder
	fs.VisitAll(func(f *flag.Flag) {
		arg := ""
		if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); !ok || !bf.IsBoolFlag() {
			arg = " value"
			if name, _ := flag.UnquoteUsage(f); name != "" {
				arg = " " + name
			}
		}
		_, usage := flag.UnquoteUsage(f)
		fmt.Fprintf(&b, "  --%s%s\n        %s\n", f.Name, arg, usage)
	})
	return b.String()
}
