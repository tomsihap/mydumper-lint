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
			ID: "MDL602", Name: "mysql-include-directive", Family: FamilyConnection,
			Severity: diag.Warning,
			Summary:  "`!include` or `!includedir`: a MySQL directive that mydumper does not follow.",
			Why: "MySQL option files accept `!include FILE` and `!includedir DIR`. mydumper reads its " +
				"configuration with GLib, which knows no such directive: the line is a key named " +
				"`!include FILE`. Only the MySQL client library, which re-reads the file for the " +
				"connection, follows it.\n\nIn [mydumper] or [myloader] the key is passed to GLib's option " +
				"parser as an unknown option: mydumper aborts at startup up to v1.0.0-1 and ignores it " +
				"afterwards (error). Elsewhere, such as [client], the connection settings of the " +
				"included file work, but any [mydumper] section or table section in it is ignored " +
				"(warning).",
			Refs: []string{"C3", "F6"},
			E2E:  []string{"mdl602-include-directive-in-tool-group", "mdl602-include-directive-in-client-group"},
		},
		Check: func(p *Pass) {
			kinds := map[string]model.GroupKind{}
			for _, g := range p.Model.Groups {
				kinds[g.Name] = g.Kind
			}
			for _, e := range p.KF.Entries {
				directive, ok := includeDirective(e.Key)
				if !ok {
					continue
				}
				d := diag.Diagnostic{
					Span: e.KeySpan,
					Message: fmt.Sprintf("`%s` is a MySQL directive: mydumper reads it as a key and never opens the "+
						"included file; only the MySQL client library does", directive),
					Consequence: "The connection settings of the included file apply, but any [mydumper], [myloader] or table section in it is ignored.",
				}
				group := p.KF.Groups[e.Group].Name
				if kind := kinds[group]; kind == model.GroupToolOptions || kind == model.GroupProductOptions {
					tool := strings.SplitN(group, "_", 2)[0]
					d.Severity = diag.Error
					fatal := fmt.Sprintf("%s aborts at startup: \"option parsing failed: Unknown option --%s, try --help\"", tool, e.Key)
					switch {
					case p.Target == nil:
						d.Consequence = fatal + " up to v1.0.0-1; later versions ignore the line."
					case p.Target.IgnoreUnknownOptions():
						d.Consequence = fmt.Sprintf("%s ignores the line as an unknown option; the included file only reaches the MySQL client library.", tool)
					default:
						d.Consequence = fatal + "."
					}
				}
				p.Report(d)
			}
		},
	})
}

// includeDirective reports whether a key is a MySQL include directive and
// returns the directive's name.
func includeDirective(key string) (string, bool) {
	for _, d := range []string{"!includedir", "!include"} {
		if rest, ok := strings.CutPrefix(key, d); ok && (rest == "" || rest[0] == ' ' || rest[0] == '\t') {
			return d, true
		}
	}
	return "", false
}
