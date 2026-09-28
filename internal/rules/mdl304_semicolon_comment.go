package rules

import (
	"bytes"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/keyfile"
	"github.com/tomsihap/mydumper-lint/internal/model"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL304", Name: "semicolon-comment", Family: FamilyValues,
			Severity: diag.Error, Fix: diag.Safe,
			Summary: "A line starting with `;` is a key, not a comment.",
			Why: "MySQL option files accept `;` comments; GLib does not. `; nightly dump` is a key " +
				"(mydumper's pre-processor gives it the value 1): in [mydumper] it is an unknown option, " +
				"fatal at startup up to v1.0.0-1. Replacing `;` with `#` turns it into the comment it was " +
				"meant to be. The fix is safe when the key has no effect today (an unknown option that " +
				"the version ignores, a group nobody reads), unsafe otherwise.",
			Refs: []string{"K3", "F6"},
		},
		Check: func(p *Pass) {
			var reasons map[int]model.Reason // built on the first ';' line
			reasonAt := func(n int) model.Reason {
				if reasons == nil {
					reasons = map[int]model.Reason{}
					for _, g := range p.Model.Groups {
						for _, e := range g.Entries {
							reasons[e.Line] = e.Reason
						}
					}
				}
				return reasons[n]
			}
			for i, lc := range p.KF.Lines {
				n := i + 1
				c := p.File.Content(n)
				at := firstSignificant(c)
				if at >= len(c) || c[at] != ';' {
					continue
				}
				d := diag.Diagnostic{
					Span:    diag.Span{Start: p.File.Line(n).Start + at, End: p.File.Line(n).Start + at + 1},
					Message: "`;` does not start a comment for GLib: this line is a key",
				}
				if lc.Kind != keyfile.KindEntry {
					continue // rejected: MDL113 explains it
				}
				applicability := diag.Unsafe
				key := quote(bytes.TrimSpace(content(p, n)))
				switch reasonAt(n) {
				case model.ReasonUnknownGroup:
					applicability = diag.Safe
					d.Consequence = "The line is a key of a group nobody reads: harmless, but not a comment."
				case model.ReasonUnknownOption:
					applicability = diag.Safe
					d.Consequence = "mydumper passes it as an unknown option, which this version ignores."
				case model.ReasonFatalAtStartup:
					d.Consequence = "mydumper passes it as an unknown option and aborts at startup: \"option parsing failed: Unknown option\"."
				case model.ReasonEffective, model.ReasonFileRejected, model.ReasonLocalized, model.ReasonShadowed,
					model.ReasonConnectionKey, model.ReasonUnknownTableKey, model.ReasonMasqueradeIdentity,
					model.ReasonConsumedAsValue, model.ReasonAfterEndOfOptions:
					d.Consequence = "The line is read as the key " + key + "."
				}
				d.Fix = &diag.Fix{
					Applicability: applicability,
					Description:   "Replace `;` with `#`",
					Edits:         []diag.Edit{{Start: d.Span.Start, End: d.Span.End, New: "#"}},
				}
				p.Report(d)
			}
		},
	})
}
