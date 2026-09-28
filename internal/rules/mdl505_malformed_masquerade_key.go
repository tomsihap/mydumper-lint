package rules

import (
	"fmt"
	"strings"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/model"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL505", Name: "malformed-masquerade-key", Family: FamilyTables,
			Severity: diag.Error, Fix: diag.Unsafe,
			Summary: "A masked column is missing its closing backtick.",
			Why: "mydumper treats a key as a masked column only when it starts with a backtick and " +
				"contains a second one: `` `email=… `` is ignored, and the column is dumped in plaintext.",
			Refs: []string{"F9"},
		},
		Check: func(p *Pass) {
			for _, e := range p.KF.Entries {
				g := p.KF.Groups[e.Group]
				if !g.Valid || !model.IsTableGroup(g.Name) || !strings.HasPrefix(e.Key, "`") || model.IsMaskedColumn(e.Key) {
					continue
				}
				d := diag.Diagnostic{
					Span:        e.KeySpan,
					Message:     fmt.Sprintf("%s is not a masked column: it has no closing backtick", quote([]byte(e.Key))),
					Consequence: "mydumper ignores the line: the column is dumped unmasked.",
				}
				if name := e.Key[1:]; name != "" && strings.Trim(name, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_$") == "" {
					d.Fix = &diag.Fix{
						Applicability: diag.Unsafe,
						Description:   "Add the closing backtick",
						Edits:         []diag.Edit{{Start: e.KeySpan.End, End: e.KeySpan.End, New: "`"}},
					}
				}
				p.Report(d)
			}
		},
	})
}
