package rules

import "github.com/tomsihap/mydumper-lint/internal/diag"

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL101", Name: "utf8-bom", Family: FamilyLoading,
			Severity: diag.Error, Fix: diag.Safe, Unsuppressible: true,
			Summary: "The file starts with a UTF-8 byte order mark.",
			Why: "GLib does not skip a byte order mark. The first line becomes `\\ufeff[mydumper]`, " +
				"which is neither a group, a key/value pair nor a comment, so GLib rejects the file and " +
				"mydumper ignores all of it. Editors add a BOM when saving as \"UTF-8 with BOM\".",
			Refs: []string{"K15", "case 06"},
		},
		Check: func(p *Pass) {
			if !hasBOM(p.File.Bytes) {
				return
			}
			p.Report(diag.Diagnostic{
				Span:        diag.Span{Start: 0, End: len(bom)},
				Message:     "file starts with a UTF-8 byte order mark (BOM)",
				Consequence: rejectionConsequence(p),
				Fix: &diag.Fix{
					Applicability: diag.Safe,
					Description:   "Remove the byte order mark",
					Edits:         []diag.Edit{{Start: 0, End: len(bom)}},
				},
			})
		},
	})
}
