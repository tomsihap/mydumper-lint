package rules

import (
	"bytes"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/keyfile"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL106", Name: "invalid-group-line", Family: FamilyLoading,
			Severity: diag.Error, Fix: diag.Safe, Unsuppressible: true,
			Summary: "A line starting with `[` is not a valid group header.",
			Why: "GLib only accepts `[name]` followed by optional spaces or tabs. Text after the `]` " +
				"(including a `# comment`), a missing `]`, an empty name, or a `[` or control character " +
				"(such as a tab) inside the name make GLib reject the file, and mydumper ignores all of " +
				"it. GLib has no end-of-line comments: a comment must be on its own line.",
			Refs: []string{"K4", "cases 09, 26, 42, 52, 53, 64, 68"},
		},
		Check: func(p *Pass) {
			forCause(p, keyfile.CauseInvalidGroupLine, func(n int, lc keyfile.LineClass) {
				c := content(p, n)
				ls := firstSignificant(c)
				d := diag.Diagnostic{
					Span:        lineSpan(p, n),
					Consequence: rejectionConsequence(p),
				}
				desc, fix := describeHeader(p, n, c, ls)
				d.Message = desc + " (" + glibSays(lc.Message) + ")"
				d.Fix = fix
				p.Report(d)
			})
		},
	})
}

// describeHeader explains what is wrong with a header line whose first
// significant byte (at ls) is '[', and offers a safe fix when the only
// problem is a trailing '#' comment without '['.
func describeHeader(p *Pass, n int, c []byte, ls int) (string, *diag.Fix) {
	h := c[ls:]
	closeAt := bytes.IndexByte(h, ']')
	if closeAt < 0 {
		return "group header has no closing `]`", nil
	}
	name, rest := h[1:closeAt], h[closeAt+1:]
	if trimmed := bytes.TrimSpace(rest); len(trimmed) > 0 {
		desc := "unexpected text after the group header: " + quote(trimmed) + "; GLib only allows spaces after `]`"
		if trimmed[0] != '#' || bytes.IndexByte(trimmed, '[') >= 0 {
			return desc, nil
		}
		start := p.File.Line(n).Start
		return desc, &diag.Fix{
			Applicability: diag.Safe,
			Description:   "Move the comment to its own line above the header",
			Edits: []diag.Edit{
				{Start: start, End: start, New: string(trimmed) + newline(p, n)},
				{Start: start + ls + closeAt + 1, End: start + len(c)},
			},
		}
	}
	switch {
	case len(name) == 0:
		return "empty group name `[]`", nil
	case bytes.IndexByte(name, '[') >= 0:
		return "group name " + quote(name) + " contains `[`", nil
	}
	for _, ch := range name {
		if ch < 0x20 || ch == 0x7f {
			return "group name " + quote(name) + " contains a control character (" + controlName(ch) + ")", nil
		}
	}
	return "invalid group header", nil
}
