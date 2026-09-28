package rules

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/model"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL508", Name: "masquerade-file-missing", Family: FamilyTables,
			Severity: diag.Warning, OptIn: true,
			Summary: "A `<file X>` of a masking format does not exist.",
			Why: "`random_format` reads `<file X>` when mydumper loads its table sections: a missing " +
				"file aborts mydumper at startup (\"File not open\"). Relative paths are resolved " +
				"against mydumper's working directory, which mydumper-lint cannot know: pass it with " +
				"`--base-dir`. Opt-in, because the files usually exist only on the backup host.",
			Refs: []string{"F11"},
		},
		Check: func(p *Pass) {
			if p.Target == nil {
				return
			}
			base := p.BaseDir
			if base == "" {
				base = "."
			}
			functions := p.Target.MasqueradeFunctions()
			for _, e := range p.KF.Entries {
				g := p.KF.Groups[e.Group]
				if !g.Valid || p.groupKind(e.Group) != model.GroupTable || !model.IsMaskedColumn(e.Key) ||
					model.MasqueradeFunction(e.Value, functions) != "random_format" {
					continue
				}
				v := e.Value
				for i := strings.Index(v, "<file "); i >= 0; {
					end := strings.IndexByte(v[i:], '>')
					if end < 0 {
						break
					}
					name := strings.TrimSpace(v[i+len("<file ") : i+end])
					path := name
					if !filepath.IsAbs(path) {
						path = filepath.Join(base, path)
					}
					if _, err := os.Stat(path); err != nil {
						p.Report(diag.Diagnostic{
							Span:        diag.Span{Start: e.ValueSpan.Start + i, End: e.ValueSpan.Start + i + end + 1},
							Message:     fmt.Sprintf("the file %s does not exist (resolved as %s)", quote([]byte(name)), filepath.ToSlash(path)),
							Consequence: "mydumper aborts at startup: \"File not open\".",
						})
					}
					next := strings.Index(v[i+end:], "<file ")
					if next < 0 {
						break
					}
					i += end + next
				}
			}
		},
	})
}
