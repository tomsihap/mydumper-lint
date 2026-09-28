package rules

import (
	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/model"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL504", Name: "masquerade-syntax", Family: FamilyTables,
			Severity: diag.Error,
			Summary:  "A masking function's arguments make mydumper stop, or misbehave.",
			Why: "mydumper parses masking arguments with hand-written loops. A missing quote or `>`, " +
				"an unknown `<tag>`, the wrong number of `apply` or `regex` arguments, an invalid " +
				"pattern, or `REPLACE_NULL 0` (from v0.21.4-1) abort mydumper at startup, while it loads " +
				"its table sections. A function without its arguments makes " +
				"mydumper read past the end of the value, and any token of 256 bytes or more overflows " +
				"a fixed buffer: the outcome is unpredictable.",
			Refs: []string{"F11"},
		},
		Check: func(p *Pass) {
			if p.Target == nil {
				return
			}
			functions := p.Target.MasqueradeFunctions()
			grammar := grammarFor(p)
			for _, e := range p.KF.Entries {
				g := p.KF.Groups[e.Group]
				if !g.Valid || p.groupKind(e.Group) != model.GroupTable || !model.IsMaskedColumn(e.Key) {
					continue
				}
				fn := model.MasqueradeFunction(e.Value, functions)
				if fn == "" {
					continue // MDL502
				}
				for _, is := range checkMasquerade(e.Value, fn, grammar) {
					consequence := "mydumper aborts at startup, while it loads its table sections."
					if is.undefined {
						consequence = "mydumper reads or writes out of bounds: the masking is unpredictable, and mydumper may crash."
					}
					p.Report(diag.Diagnostic{
						Span:        diag.Span{Start: e.ValueSpan.Start + is.start, End: e.ValueSpan.Start + is.end},
						Message:     is.msg,
						Consequence: consequence,
					})
				}
			}
		},
	})
}
