package rules

import (
	"fmt"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/model"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL601", Name: "connection-keys-ignored", Family: FamilyConnection,
			Severity: diag.Error, Fix: diag.Unsafe,
			Summary: "`host`, `user` or `password` in a group where nobody reads them.",
			Why: "mydumper does not pass `host`, `user` and `password` to its option parser: the MySQL " +
				"client library reads them. It reads [mydumper] only while the file has no [client] " +
				"section: once [client] exists, mydumper tells the library to read the [client] family " +
				"of groups only, and the connection keys of [mydumper] are read by nobody. Keys in " +
				"per-product groups ([mydumper_mysql]) are never read. The fix moves them to [client], " +
				"unless [client] already sets them.",
			Refs: []string{"F3", "F13", "C1"},
			E2E:  []string{"mdl601-connection-keys-in-tool-group-ignored", "mdl601-control-connection-keys-without-client-group"},
		},
		Check: func(p *Pass) {
			if !p.KF.Loadable {
				return
			}
			kinds := map[string]model.GroupKind{}
			for _, g := range p.Model.Groups {
				kinds[g.Name] = g.Kind
			}
			client := map[string]bool{} // connection keys [client] sets
			clientEnd := -1             // offset after the last line of [client]
			for gi, g := range p.KF.Groups {
				if g.Valid && g.Name == "client" {
					clientEnd = p.File.Line(sectionEnd(p, g.Line)).End + 1
					for _, e := range p.KF.Entries {
						if e.Group == gi {
							client[e.Key] = true
						}
					}
				}
			}
			hasClient := clientEnd >= 0
			for _, e := range p.KF.Entries {
				g := p.KF.Groups[e.Group]
				if !g.Valid || !connectionKey(e.Key) {
					continue
				}
				kind := kinds[g.Name]
				var why string
				switch {
				case kind == model.GroupProductOptions:
					why = fmt.Sprintf("the MySQL client library never reads [%s]", g.Name)
				case kind == model.GroupToolOptions && hasClient:
					why = fmt.Sprintf("the file has a [client] section, so the MySQL client library reads [client], not [%s]", g.Name)
				default:
					continue
				}
				d := diag.Diagnostic{
					Span:        e.KeySpan,
					Message:     fmt.Sprintf("`%s` in [%s] is read by nobody: mydumper leaves connection keys to the MySQL client library, and %s", e.Key, g.Name, why),
					Consequence: fmt.Sprintf("The connection does not use this %s: it falls back to [client], the command line or the defaults.", e.Key),
				}
				if hasClient && !client[e.Key] && clientEnd <= len(p.File.Bytes) && p.File.Bytes[clientEnd-1] == '\n' {
					line := p.File.Line(e.Line)
					d.Fix = &diag.Fix{
						Applicability: diag.Unsafe,
						Description:   "Move it to [client]",
						Edits: []diag.Edit{
							{Start: clientEnd, End: clientEnd, New: string(p.File.Bytes[line.Start:line.End]) + "\n"},
							removeLine(p, e.Line),
						},
					}
				}
				p.Report(d)
			}
		},
	})
}

func connectionKey(k string) bool { return k == "host" || k == "user" || k == "password" }
