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
			ID: "MDL303", Name: "inline-comment", Family: FamilyValues,
			Severity: diag.Warning, Fix: diag.Unsafe,
			Summary: "A `#` comment after a value is part of the value.",
			Why: "GLib has no end-of-line comments: in `threads=4 # four threads` the value is " +
				"`4 # four threads`, which an integer option rejects (mydumper aborts at startup), and " +
				"which silently changes a path, a name or a regular expression. In SQL values such as " +
				"`where`, a `#` outside quotes comments out the rest of the statement mydumper builds. " +
				"The fix moves the comment to its own line above, unless it contains `[` (which would " +
				"change how the pre-processor reads the next lines). [client] is not checked: the MySQL " +
				"client library does strip such comments.",
			Refs: []string{"K10"},
		},
		Check: func(p *Pass) {
			kinds := map[string]model.GroupKind{}
			for _, g := range p.Model.Groups {
				kinds[g.Name] = g.Kind
			}
			options := map[int]*optionKey{}
			keys := optionKeys(p)
			for i := range keys {
				options[keys[i].e.Line] = &keys[i]
			}
			for _, e := range p.KF.Entries {
				g := p.KF.Groups[e.Group]
				kind := kinds[g.Name]
				if !g.Valid || kind == model.GroupClient || kind == model.GroupSessionVariables || kind == model.GroupGlobalVariables {
					continue // libmysqlclient and the server strip such comments
				}
				sql := kind == model.GroupTable && sqlKey(e.Key)
				at := commentStart(e.Value, sql)
				if at < 0 {
					continue
				}
				comment := e.Value[at:]
				value := strings.TrimRight(e.Value[:at], " \t")
				start := e.ValueSpan.Start + len(value)
				d := diag.Diagnostic{
					Span:        diag.Span{Start: e.ValueSpan.Start + at, End: e.ValueSpan.End},
					Message:     fmt.Sprintf("`%s` is part of the value of `%s`: GLib has no end-of-line comments", comment, e.Key),
					Consequence: fmt.Sprintf("The value is %s, not %s.", quote([]byte(e.Value)), quote([]byte(value))),
				}
				if k := options[e.Line]; k != nil && k.entry != nil {
					if msg := k.ctx.Check(k.ref, k.name(), e.Value); msg != "" {
						d.Consequence = fatalConsequence(k, msg)
					}
				}
				if sql {
					d.Consequence = "The comment runs to the end of the SQL statement mydumper builds, cutting what follows it."
				}
				if !strings.Contains(comment, "[") {
					l := p.File.Line(e.Line)
					indent := p.File.Content(e.Line)[:firstSignificant(p.File.Content(e.Line))]
					d.Fix = &diag.Fix{
						Applicability: diag.Unsafe,
						Description:   "Move the comment to its own line above",
						Edits: []diag.Edit{
							{Start: l.Start, End: l.Start, New: string(indent) + comment + "\n"},
							{Start: start, End: e.ValueSpan.End},
						},
					}
				}
				p.Report(d)
			}
		},
	})
}
