package report

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata/")

// render runs the format called name over results.
func render(t *testing.T, name string, results []FileResult, opt Options) string {
	t.Helper()
	f, err := Get(name)
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := f.Write(&b, results, Summarize(results), opt); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// golden compares got with testdata/<name>.golden, or rewrites the file with -update.
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run: go test ./internal/report -update)", err)
	}
	if got != string(want) {
		t.Errorf("output differs from %s (run: go test ./internal/report -update, then review the diff)\n--- got ---\n%s--- want ---\n%s", path, got, want)
	}
}

// TestGolden renders every scenario in every format. The "files" scenario is
// also rendered with colors and, in the text format, without summary. Color
// codes are stored as the text \x1b so that golden files stay readable.
func TestGolden(t *testing.T) {
	for _, name := range Names() {
		for _, sc := range scenarios() {
			variants := []string{""}
			if sc.name == "files" && (name == "text" || name == "concise") {
				variants = append(variants, "color")
			}
			if sc.name == "files" && name == "text" {
				variants = append(variants, "quiet")
			}
			for _, variant := range variants {
				file := sc.name + "." + name
				opt := testOptions()
				switch variant {
				case "color":
					opt.Color, file = true, file+"-color"
				case "quiet":
					opt.Quiet, file = true, file+"-quiet"
				}
				t.Run(file, func(t *testing.T) {
					got := render(t, name, sc.results, opt)
					if opt.Color {
						got = strings.ReplaceAll(got, "\x1b", `\x1b`)
					}
					golden(t, file, got)
				})
			}
		}
	}
}

// TestOutputIsTerminalSafe checks that no format lets a raw control
// character, an invisible code point or invalid UTF-8 through, whatever the
// file contains.
func TestOutputIsTerminalSafe(t *testing.T) {
	for _, name := range Names() {
		for _, sc := range scenarios() {
			out := render(t, name, sc.results, testOptions())
			if !utf8.ValidString(out) {
				t.Errorf("%s/%s: output is not valid UTF-8", name, sc.name)
			}
			for i, r := range out {
				if (r < 0x20 && r != '\n') || (r >= 0x7f && r <= 0x9f) || r == 0xfeff {
					t.Errorf("%s/%s: raw %U at byte %d", name, sc.name, r, i)
				}
			}
		}
	}
}

// TestColorsOnlyInTextFormats checks that Color and Quiet only affect the
// text formats, and that without Color no format emits an escape sequence.
func TestColorsOnlyInTextFormats(t *testing.T) {
	results := scenarios()[0].results
	for _, name := range Names() {
		plain := render(t, name, results, testOptions())
		if strings.Contains(plain, "\x1b") {
			t.Errorf("%s: escape sequence without Color", name)
		}
		opt := testOptions()
		opt.Color, opt.Quiet = true, true
		colored := render(t, name, results, opt)
		switch name {
		case "text", "concise":
			if !strings.Contains(colored, "\x1b[") {
				t.Errorf("%s: no color with Color", name)
			}
		default:
			if colored != plain {
				t.Errorf("%s: output depends on Color or Quiet", name)
			}
		}
	}
}
