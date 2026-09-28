package model

import (
	"fmt"
	"strings"
	"testing"

	"github.com/tomsihap/mydumper-lint/internal/keyfile"
	"github.com/tomsihap/mydumper-lint/internal/preprocess"
	"github.com/tomsihap/mydumper-lint/internal/source"
)

func parse(s string) *keyfile.Result {
	f := source.New("t.cnf", []byte(s))
	return keyfile.Parse(f, preprocess.Run(f))
}

// renderSet prints a set one entry per line: "group origin:line key=value
// reason", then the health and the connections.
func renderSet(s *Set) string {
	var b strings.Builder
	for _, g := range s.Groups {
		for _, e := range g.Entries {
			fmt.Fprintf(&b, "[%s] %s:%d %s=%s %s\n", g.Name, e.Origin, e.Line, e.Key, e.Value, e.Reason)
		}
	}
	fmt.Fprintln(&b, strings.TrimSpace("health "+s.Health.String()+" "+s.Fatal))
	for _, c := range s.Connections {
		if !c.Read {
			fmt.Fprintf(&b, "%s: no file\n", c.Tool)
			continue
		}
		fmt.Fprintf(&b, "%s: %s [client] [%s]\n", c.Tool, c.From, c.Group)
	}
	return b.String()
}

func TestBuildSet(t *testing.T) {
	tests := []struct {
		name            string
		defaults, extra string
		ignore          bool
		want            string
	}{
		{
			name:     "defaults rejected: the extra file's tool group applies, its table section is lost",
			defaults: "[client]\nhost=h\n  \n[mydumper]\nthreads=2\n",
			extra:    "[mydumper]\nthreads=3\n[`app`.`users`]\n`email`=random_string\n",
			want: `[client] defaults:2 host=h file-rejected
[mydumper] defaults:5 threads=2 file-rejected
[mydumper] extra:2 threads=3 effective
` + "[`app`.`users`] extra:4 `email`=random_string defaults-file-rejected" + `
health ok
mydumper: extra [client] [mydumper]
myloader: defaults [client] []
`,
		},
		{
			name:     "tool groups: the extra file overrides options, not callbacks",
			defaults: "[mydumper]\nthreads=2\noutputdir=/a\ncompress=zstd\n",
			extra:    "[mydumper]\nthreads=3\ncompress=gzip\n",
			want: `[mydumper] defaults:2 threads=2 overridden-by-extra-file
[mydumper] defaults:3 outputdir=/a effective
[mydumper] defaults:4 compress=zstd effective
[mydumper] extra:2 threads=3 effective
[mydumper] extra:3 compress=gzip effective
health ok
mydumper: extra [client] [mydumper]
myloader: no file
`,
		},
		{
			name:     "table sections are merged key by key",
			defaults: "[`app`.`users`]\n`email`=random_string\nwhere=id>1\n",
			extra:    "[`app`.`users`]\n`email`=bogus\n`email`=constant x\n[`app`.`orders`]\nwhere=id>2\nbogus=1\n",
			want: "[`app`.`users`] defaults:2 `email`=random_string overridden-by-extra-file\n" +
				"[`app`.`users`] defaults:3 where=id>1 effective\n" +
				"[`app`.`users`] extra:2 `email`=constant x shadowed-by-duplicate\n" +
				"[`app`.`users`] extra:3 `email`=constant x effective\n" +
				"[`app`.`orders`] extra:5 where=id>2 effective\n" +
				"[`app`.`orders`] extra:6 bogus=1 unknown-table-key\n" +
				`health ok
mydumper: no file
myloader: no file
`,
		},
		{
			name:     "an unknown masking function set by the extra file",
			defaults: "[`app`.`users`]\n`email`=random_string\n",
			extra:    "[`app`.`users`]\n`email`=bogus\n",
			want: "[`app`.`users`] defaults:2 `email`=random_string overridden-by-extra-file\n" +
				"[`app`.`users`] extra:2 `email`=bogus masquerade-fallback-identity\n" +
				`health ok
mydumper: no file
myloader: no file
`,
		},
		{
			name:     "per-product groups are parsed from the merged file",
			defaults: "[mydumper_mysql]\nthreads=2\n",
			extra:    "[mydumper_mysql]\nbogus=1\n[myloader_mysql]\nthreads=2\n",
			want: `[mydumper_mysql] defaults:2 threads=2 effective
[mydumper_mysql] extra:2 bogus=1 fatal-at-startup
[myloader_mysql] extra:4 threads=2 unknown-group
health fatal-at-startup mydumper: option parsing failed: Unknown option --bogus, try --help
mydumper: no file
myloader: no file
`,
		},
		{
			name:     "the tool groups fail before the merged per-product groups",
			defaults: "[mydumper_mysql]\nbad=1\n",
			extra:    "[mydumper]\nbogus=1\n",
			want: `[mydumper_mysql] defaults:2 bad=1 fatal-at-startup
[mydumper] extra:2 bogus=1 fatal-at-startup
health fatal-at-startup mydumper: option parsing failed: Unknown option --bogus, try --help
mydumper: extra [client] [mydumper]
myloader: no file
`,
		},
		{
			name:     "extra file rejected: nothing merged, the client library reads it",
			defaults: "[client]\nhost=h\n[`app`.`users`]\n`email`=random_string\n",
			extra:    "[`app`.`users`]\n`email`=bogus\n  \n",
			want: `[client] defaults:2 host=h effective
` + "[`app`.`users`] defaults:4 `email`=random_string effective\n" +
				"[`app`.`users`] extra:2 `email`=bogus file-rejected\n" +
				`health ok
mydumper: extra [client] []
myloader: extra [client] []
`,
		},
		{
			name:     "an extra [client] replaces the group set by the defaults file",
			defaults: "[mydumper]\nthreads=2\n",
			extra:    "[client]\nuser=u\n[mydumper_session_variables]\nsql_mode=\n",
			want: `[mydumper] defaults:2 threads=2 effective
[client] extra:2 user=u effective
[mydumper_session_variables] extra:4 sql_mode= effective
health ok
mydumper: extra [client] []
myloader: extra [client] []
`,
		},
		{
			name:     "both files rejected",
			defaults: "  \n[mydumper]\n",
			extra:    "=1\n",
			want: `health rejected
mydumper: extra [client] []
myloader: extra [client] []
`,
		},
		{
			name:     "overrides need both files to load",
			defaults: "[mydumper]\nthreads=2\n",
			extra:    "[mydumper]\nthreads=3\n  \n",
			ignore:   true,
			want: `[mydumper] defaults:2 threads=2 effective
[mydumper] extra:2 threads=3 file-rejected
health ok
mydumper: extra [client] []
myloader: extra [client] []
`,
		},
		{
			name:     "a new key repeated in the extra file is merged once, with its last value",
			defaults: "[`app`.`users`]\nwhere=id>1\n",
			extra:    "[`app`.`orders`]\nwhere=1\nwhere=2\n",
			want: "[`app`.`users`] defaults:2 where=id>1 effective\n" +
				"[`app`.`orders`] extra:2 where=2 shadowed-by-duplicate\n" +
				"[`app`.`orders`] extra:3 where=2 effective\n" +
				`health ok
mydumper: no file
myloader: no file
`,
		},
		{
			name:     "keys localized for the default language list are merged",
			defaults: "[`app`.`users`]\nwhere[C]=1\n",
			extra:    "[`app`.`users`]\nwhere[C]=2\n",
			want: "[`app`.`users`] defaults:2 where[C]=1 overridden-by-extra-file\n" +
				"[`app`.`users`] extra:2 where[C]=2 unknown-table-key\n" +
				`health ok
mydumper: no file
myloader: no file
`,
		},
		{
			name:     "localized keys of the extra file are not merged",
			defaults: "[`app`.`users`]\nwhere=id>1\n",
			extra:    "[`app`.`users`]\nwhere[fr]=id>2\n",
			want: "[`app`.`users`] defaults:2 where=id>1 effective\n" +
				"[`app`.`users`] extra:2 where[fr]=id>2 localized\n" +
				`health ok
mydumper: no file
myloader: no file
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := BuildSet(parse(tt.defaults), parse(tt.extra), Options{Target: newFake(tt.ignore)})
			if got := renderSet(s); got != tt.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

func TestBuildSetWithoutTarget(t *testing.T) {
	s := BuildSet(parse("[mydumper]\nthreads=2\n"), parse("[mydumper]\nthreads=3\n"), Options{})
	want := `[mydumper] defaults:2 threads=2 effective
[mydumper] extra:2 threads=3 effective
health ok
mydumper: extra [client] [mydumper]
myloader: no file
`
	if got := renderSet(s); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	if FromDefaults.String() != "defaults" || FromExtra.String() != "extra" {
		t.Error("Origin.String")
	}
}

// FuzzBuildSet checks that any pair of files gives a set whose entries are
// exactly those of the two files, with Effective matching the reason.
func FuzzBuildSet(f *testing.F) {
	f.Add("[mydumper]\nthreads=2\n[`a`.`b`]\n`c`=random_string\n", "[mydumper]\nthreads=3\n[`a`.`b`]\n`c`=bogus\n")
	f.Add("  \n[mydumper_mysql]\nx=1\n", "[mydumper_mysql]\ny\n[client]\nuser=u\n")
	f.Add("[`a`.`b`]\nk=1\nk=2\nk[fr]=3\n", "[`a`.`b`]\nk=4\nk=5\n[`a`.`c`]\nk=6\n")
	f.Fuzz(func(t *testing.T, d, x string) {
		opt := Options{Target: newFake(len(d)%2 == 0)}
		dk, xk := parse(d), parse(x)
		s := BuildSet(dk, xk, opt)
		n := 0
		for _, g := range s.Groups {
			for _, e := range g.Entries {
				n++
				if e.Effective != (e.Reason == ReasonEffective) {
					t.Fatalf("%s: effective %v with reason %s", e.Key, e.Effective, e.Reason)
				}
			}
		}
		want := 0
		for _, m := range []*Model{Build(dk, opt), Build(xk, opt)} {
			for _, g := range m.Groups {
				want += len(g.Entries)
			}
		}
		if n != want || len(s.Connections) != 2 {
			t.Fatalf("%d entries, want %d; %d connections", n, want, len(s.Connections))
		}
	})
}
