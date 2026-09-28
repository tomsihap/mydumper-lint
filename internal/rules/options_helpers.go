package rules

import (
	"fmt"
	"strings"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/goption"
	"github.com/tomsihap/mydumper-lint/internal/keyfile"
	"github.com/tomsihap/mydumper-lint/internal/model"
	"github.com/tomsihap/mydumper-lint/internal/optionsdb"
)

// optionKey is one key of an option group as GOption sees it (design §3.4).
type optionKey struct {
	g     *model.Group
	e     *model.Entry
	ke    *keyfile.Entry       // the line: spans and the value written there
	ctx   *goption.Context     // the tool's option table for the target
	ref   goption.Ref          // the option the key names, when entry is set
	entry *goption.Entry       // nil for an unknown option
	alias bool                 // named as --<group prefix>-<option>
	span  optionsdb.OptionSpan // knowledge-base facts about the option
}

// name is the option's long name as GLib writes it in messages.
func (k *optionKey) name() string { return "--" + k.entry.Long }

// effective reports whether the key's own line is the one GOption parses:
// an earlier duplicate is parsed with the last value (K13, G11).
func (k *optionKey) effective() bool { return !k.e.Shadowed }

// optionKeys lists the keys mydumper passes to GOption: those of [mydumper],
// [myloader] and their product groups, except host, user and password (F3)
// and localized keys (MDL305). Empty when no target is known or the file is
// rejected.
func optionKeys(p *Pass) []optionKey {
	if !p.optDone {
		p.optKeys, p.optDone = computeOptionKeys(p), true
	}
	return p.optKeys
}

func computeOptionKeys(p *Pass) []optionKey {
	if p.Model == nil {
		return nil
	}
	byLine := make(map[int]*keyfile.Entry, len(p.KF.Entries))
	for i := range p.KF.Entries {
		byLine[p.KF.Entries[i].Line] = &p.KF.Entries[i]
	}
	var out []optionKey
	for gi := range p.Model.Groups {
		g := &p.Model.Groups[gi]
		if g.GOption == nil {
			continue
		}
		for ei := range g.Entries {
			e := &g.Entries[ei]
			ke := byLine[e.Line]
			if e.Element == 0 || ke == nil || ke.Locale != "" {
				continue
			}
			k := optionKey{g: g, e: e, ke: ke, ctx: g.GOption.Context}
			if r, alias, ok := k.ctx.Lookup(e.Key); ok {
				k.ref, k.alias, k.entry = r, alias, k.ctx.Entry(r)
				k.span, _ = p.Target.Option(g.Tool, k.entry.Long)
			}
			out = append(out, k)
		}
	}
	return out
}

// fatalConsequence is what mydumper (or myloader) prints before aborting on
// a GOption error, with a note for groups read only for some servers.
func fatalConsequence(k *optionKey, msg string) string {
	s := fmt.Sprintf("%s aborts at startup: \"option parsing failed: %s, try --help\".", k.g.Tool, msg)
	return s + productNote(k.g)
}

// productNote explains when a product group is read (design §3.6).
func productNote(g *model.Group) string {
	if g.Kind != model.GroupProductOptions {
		return ""
	}
	return fmt.Sprintf(" [%s] is only read when the server matches it.", g.Name)
}

// valueSpan is the span of the value on the key's line, or its key when the
// value is empty.
func (k *optionKey) valueSpan() (start, end int) {
	if k.ke.ValueSpan.Empty() {
		return k.ke.KeySpan.Start, k.ke.KeySpan.End
	}
	return k.ke.ValueSpan.Start, k.ke.ValueSpan.End
}

// historian is implemented by targets that know an option's history
// (*optionsdb.View).
type historian interface {
	History(tool, name string) []optionsdb.OptionSpan
}

// optionHistory explains why a name is not an option of the target version
// although the knowledge base knows it: "added in v0.21.3-2", "removed after
// v0.21.4-1", "only in builds with WITH_SSL". "" when it never existed.
func optionHistory(p *Pass, tool, name string) string {
	h, ok := p.Target.(historian)
	if !ok || p.Version == "" {
		return ""
	}
	spans := h.History(tool, name)
	if len(spans) == 0 {
		return ""
	}
	target, err := optionsdb.ParseTag(p.Version)
	if err != nil {
		return ""
	}
	first, errFirst := optionsdb.ParseTag(spans[0].From)
	last := spans[len(spans)-1]
	switch {
	case errFirst == nil && target.Less(first):
		return fmt.Sprintf("--%s was added in %s; the target is %s", name, spans[0].From, p.Version)
	case last.To != "":
		if to, err := optionsdb.ParseTag(last.To); err == nil && to.Less(target) {
			return fmt.Sprintf("--%s was removed after %s; the target is %s", name, last.To, p.Version)
		}
	}
	for _, s := range spans {
		if s.Condition != "" {
			return fmt.Sprintf("--%s only exists in builds where %s holds (see mydumper-build in the configuration)", name, s.Condition)
		}
	}
	return ""
}

// otherTool returns the tool that is not tool.
func otherTool(tool string) string {
	if tool == "mydumper" {
		return "myloader"
	}
	return "mydumper"
}

// commentOut is an edit that turns line n into a comment.
func commentOut(p *Pass, n int) diag.Edit {
	at := p.File.Line(n).Start + firstSignificant(p.File.Content(n))
	return diag.Edit{Start: at, End: at, New: "#"}
}

// lowerASCII lowercases ASCII letters only, like g_ascii_strdown.
func lowerASCII(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + 'a' - 'A'
		}
		return r
	}, s)
}
