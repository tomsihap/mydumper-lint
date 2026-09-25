package report

import (
	"io"
	"strings"
)

// conciseFormat prints exactly one line per diagnostic, the header of the
// text format, and no summary.
type conciseFormat struct{}

func init() { Register(conciseFormat{}) }

func (conciseFormat) Name() string { return "concise" }

func (conciseFormat) Write(w io.Writer, results []FileResult, _ Summary, opt Options) error {
	p := painter(opt.Color)
	var b strings.Builder
	for _, r := range results {
		v := newFileView(r)
		for _, d := range r.Diagnostics {
			b.WriteString(header(v, d, p))
			b.WriteByte('\n')
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}
