package rules

import (
	"fmt"

	"github.com/tomsihap/mydumper-lint/internal/diag"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL603", Name: "client-group-overridden", Family: FamilyConnection,
			Severity: diag.Warning,
			Summary:  "An extra file makes the MySQL client library ignore the defaults file's [client].",
			Why: "mydumper hands one file to the MySQL client library for the connection. The extra " +
				"file (`--defaults-extra-file`) replaces the defaults file in that role when it has a " +
				"[mydumper] or [client] section, or when GLib rejects it. The library then reads that " +
				"file only: the [client] section of the defaults file, its host, user and password, is " +
				"ignored. Only load sets (design §9.3) let mydumper-lint see both files.",
			Refs: []string{"F14", "C2"},
			E2E:  []string{"mdl603-extra-file-tool-group-overrides-client", "mdl603-extra-file-client-group-overrides-client", "mdl603-control-extra-file-without-tool-or-client-group"},
		},
		Check: func(*Pass) {},
		CheckSet: func(s *Set) {
			d, x := s.Defaults, s.Extra
			clientLine := 0
			for _, g := range d.KF.Groups {
				if g.Valid && g.Name == "client" && clientLine == 0 {
					clientLine = g.Line
				}
			}
			if !d.KF.Loadable || clientLine == 0 {
				return
			}
			var why string
			span := diag.Span{}
			switch {
			case !x.KF.Loadable:
				why = "GLib rejects it"
			default:
				for _, g := range validHeaders(x) {
					if g.Name == "client" || g.Name == "mydumper" || g.Name == "myloader" {
						why, span = fmt.Sprintf("it has a [%s] section", g.Name), g.NameSpan
						break
					}
				}
			}
			if why == "" {
				return
			}
			s.Report(x, diag.Diagnostic{
				Span: span,
				Message: fmt.Sprintf("this extra file becomes the MySQL client library's defaults file because %s: "+
					"the [client] section of %s (line %d) is ignored", why, d.File.Path, clientLine),
				Consequence: "The connection uses only this file's settings: the host, user and password of the defaults file's [client] do not apply.",
			})
		},
	})
}
