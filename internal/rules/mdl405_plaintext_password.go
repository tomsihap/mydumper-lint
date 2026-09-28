package rules

import (
	"github.com/tomsihap/mydumper-lint/internal/diag"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL405", Name: "plaintext-password", Family: FamilyOptions,
			Severity: diag.Warning,
			Summary:  "The file holds a password in plain text.",
			Why: "Configuration files are often versioned, copied between hosts and readable by more " +
				"people than the database account they unlock. Keep the password out of them: put it " +
				"in a separate file readable only by the backup user (passed with " +
				"`--defaults-extra-file`), or let your secret manager provide it at run time.",
			Refs: []string{"F3"},
		},
		Check: func(p *Pass) {
			for _, e := range p.KF.Entries {
				// Even under a rejected header: the secret is in the file, and
				// the MySQL client library may still read it (F12).
				if e.Key != "password" || e.Value == "" {
					continue
				}
				p.Report(diag.Diagnostic{
					Span:        e.ValueSpan,
					Message:     "password in plain text",
					Consequence: "Anyone who can read this file, or its history in version control, can connect to the server as this user.",
				})
			}
		},
	})
}
