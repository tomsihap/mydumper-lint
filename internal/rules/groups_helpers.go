package rules

import (
	"strings"

	"github.com/tomsihap/mydumper-lint/internal/keyfile"
	"github.com/tomsihap/mydumper-lint/internal/model"
)

// products returns the product names of the target, or nil for the known
// list (model.ClassifyGroup).
func products(p *Pass) []string {
	if p.Target != nil {
		return p.Target.Products()
	}
	return nil
}

// knownGroup reports whether mydumper, myloader or the MySQL client library
// reads a group of that name (design §3.6).
func knownGroup(p *Pass, name string) bool {
	kind, _ := model.ClassifyGroup(name, products(p))
	return kind != model.GroupUnknown
}

// looksLikeTable reports whether a group name was probably meant as a table
// section: it contains a dot or a backtick.
func looksLikeTable(name string) bool {
	return strings.ContainsAny(name, ".`")
}

// validHeaders lists the group headers GLib accepted, in file order.
func validHeaders(p *Pass) []keyfile.Group {
	var out []keyfile.Group
	for _, g := range p.KF.Groups {
		if g.Valid {
			out = append(out, g)
		}
	}
	return out
}

// groupNames are the names suggested for a misspelled group.
var groupNames = []string{
	"mydumper", "myloader", "client",
	"mydumper_session_variables", "myloader_session_variables",
	"mydumper_global_variables", "myloader_global_variables",
}
