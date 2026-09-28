package rules

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/keyfile"
)

// Suppression comments (design §6.4). GKeyFile has no end-of-line comments,
// so a directive sits on its own line:
//
//	# mydumper-lint: disable-next-line=MDL302,MDL303
//	# mydumper-lint: disable-file=MDL401
const directivePrefix = "mydumper-lint:"

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL001", Name: "unused-suppression", Family: FamilySuppressions,
			Severity: diag.Info, Fix: diag.Safe, Unsuppressible: true,
			Summary: "A suppression comment suppresses nothing.",
			Why: "A `# mydumper-lint: disable-…` comment that no longer matches a diagnostic hides " +
				"nothing today, and will silently hide a real problem tomorrow. The fix removes the " +
				"unused rules from the comment, or the whole comment.",
		},
		Check: func(*Pass) {}, // reported by Pass.suppress, after every other rule
	})
	register(&Rule{
		Meta: Meta{
			ID: "MDL002", Name: "invalid-suppression", Family: FamilySuppressions,
			Severity: diag.Warning, Unsuppressible: true,
			Summary: "A suppression comment is malformed, names an unknown rule, or targets MDL1xx.",
			Why: "Directives are `# mydumper-lint: disable-next-line=ID,…` and " +
				"`# mydumper-lint: disable-file=ID,…`, with rule IDs or names. A directive without rules, " +
				"with an unknown rule or with another verb does nothing. MDL1xx diagnostics mean mydumper " +
				"ignores the whole file: they cannot be suppressed by comments, only by the configuration " +
				"or `--ignore`. A directive must not contain `[`, which would change how mydumper's " +
				"pre-processor reads the following lines.",
		},
		Check: func(*Pass) {},
	})
}

// directive is one parsed suppression comment.
type directive struct {
	line int
	file bool // disable-file; else disable-next-line
	ids  []directiveID
	list diag.Span // the comma-separated list
}

type directiveID struct {
	rule *Rule
	span diag.Span
	used bool
}

// directives parses the suppression comments of the file. Malformed ones are
// returned as MDL002 diagnostics.
func (p *Pass) directives() ([]*directive, []diag.Diagnostic) {
	var out []*directive
	var bad []diag.Diagnostic
	for i, lc := range p.KF.Lines {
		if lc.Kind != keyfile.KindComment {
			continue
		}
		n := i + 1
		c := content(p, n)
		start := p.File.Line(n).Start
		j := firstSignificant(c)
		if j >= len(c) || c[j] != '#' {
			continue
		}
		k := j + 1
		for k < len(c) && (c[k] == ' ' || c[k] == '\t') {
			k++
		}
		if !bytes.HasPrefix(c[k:], []byte(directivePrefix)) {
			continue
		}
		whole := diag.Span{Start: start + j, End: start + len(c)}
		invalid := func(msg string) {
			bad = append(bad, diag.Diagnostic{RuleID: "MDL002", Span: whole, Message: msg})
		}
		if bytes.IndexByte(c, '[') >= 0 {
			invalid("a suppression comment must not contain `[`: mydumper's pre-processor would not reset its state after it")
			continue
		}
		k += len(directivePrefix)
		for k < len(c) && (c[k] == ' ' || c[k] == '\t') {
			k++
		}
		verb, list, ok := strings.Cut(string(c[k:]), "=")
		verb = strings.TrimSpace(verb)
		d := &directive{line: n}
		switch {
		case !ok || (verb != "disable-next-line" && verb != "disable-file"):
			invalid(fmt.Sprintf("unknown suppression %q: use `disable-next-line=ID,…` or `disable-file=ID,…`", strings.TrimSpace(string(c[k:]))))
			continue
		case strings.TrimSpace(list) == "":
			invalid("the suppression names no rule: list rule IDs or names after `=`")
			continue
		}
		d.file = verb == "disable-file"
		listStart := start + k + bytes.IndexByte(c[k:], '=') + 1
		d.list = diag.Span{Start: listStart, End: start + len(c)}
		off := 0
		valid := true
		for _, tok := range strings.Split(list, ",") {
			lead := len(tok) - len(strings.TrimLeft(tok, " \t"))
			name := strings.TrimSpace(tok)
			span := diag.Span{Start: listStart + off + lead, End: listStart + off + lead + len(name)}
			off += len(tok) + 1
			r, ok := Lookup(name)
			switch {
			case name == "":
				bad = append(bad, diag.Diagnostic{RuleID: "MDL002", Span: whole, Message: "empty rule name in the suppression list"})
				valid = false
			case !ok:
				msg := fmt.Sprintf("unknown rule %q in the suppression", name)
				if s := Suggest(name); s != "" {
					msg += fmt.Sprintf(" (did you mean %q?)", s)
				}
				bad = append(bad, diag.Diagnostic{RuleID: "MDL002", Span: span, Message: msg})
				valid = false
			case r.Unsuppressible:
				bad = append(bad, diag.Diagnostic{
					RuleID: "MDL002", Span: span,
					Message: fmt.Sprintf("%s cannot be suppressed by a comment: disable it in the configuration or with --ignore", r.ID),
				})
				valid = false
			default:
				d.ids = append(d.ids, directiveID{rule: r, span: span})
			}
		}
		if valid {
			out = append(out, d)
		}
	}
	return out, bad
}

// suppress removes the diagnostics the directives cover, then reports MDL001
// and MDL002 when they are enabled.
func (p *Pass) suppress(enabled []Enabled) {
	dirs, bad := p.directives()
	sev := map[string]diag.Severity{}
	on := map[string]bool{}
	for _, e := range enabled {
		on[e.Rule.ID], sev[e.Rule.ID] = true, e.Severity
	}
	kept := p.out[:0]
	for _, d := range p.out {
		r, _ := Lookup(d.RuleID)
		if r == nil || r.Unsuppressible || !p.suppressed(dirs, d) {
			kept = append(kept, d)
		}
	}
	p.out = kept
	emit := func(r *Rule, d diag.Diagnostic) {
		if !on[r.ID] {
			return
		}
		d.RuleID, d.RuleName = r.ID, r.Name
		if d.Severity == diag.Off || sev[r.ID] != r.Severity {
			d.Severity = sev[r.ID]
		}
		p.out = append(p.out, d)
	}
	mdl001, _ := Lookup("MDL001")
	mdl002, _ := Lookup("MDL002")
	for _, d := range bad {
		emit(mdl002, d)
	}
	for _, d := range dirs {
		var unused, used []string
		for _, id := range d.ids {
			if !on[id.rule.ID] {
				used = append(used, id.rule.ID) // not run now: cannot tell
				continue
			}
			if id.used {
				used = append(used, id.rule.ID)
			} else {
				unused = append(unused, id.rule.ID)
			}
		}
		if len(unused) == 0 {
			continue
		}
		fix := &diag.Fix{Applicability: diag.Safe}
		if len(used) > 0 {
			fix.Description = "Remove " + strings.Join(unused, ", ") + " from the suppression"
			fix.Edits = []diag.Edit{{Start: d.list.Start, End: d.list.End, New: strings.Join(used, ",")}}
		} else {
			fix.Description = "Remove the suppression comment"
			fix.Edits = []diag.Edit{removeLine(p, d.line)}
		}
		emit(mdl001, diag.Diagnostic{
			Span:    d.list,
			Message: fmt.Sprintf("unused suppression: %s reported nothing here", strings.Join(unused, ", ")),
			Fix:     fix,
		})
	}
}

// suppressed reports whether a directive covers d, and marks it used.
func (p *Pass) suppressed(dirs []*directive, d diag.Diagnostic) bool {
	line := p.File.Position(d.Span.Start).Line
	hit := false
	for _, dir := range dirs {
		if !dir.file && dir.line+1 != line {
			continue
		}
		for i := range dir.ids {
			if dir.ids[i].rule.ID == d.RuleID {
				dir.ids[i].used, hit = true, true
			}
		}
	}
	return hit
}
