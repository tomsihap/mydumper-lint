package model

import (
	"sort"

	"github.com/tomsihap/mydumper-lint/internal/goption"
)

// GOptionRun is how GLib parses one option group: mydumper turns the group
// into [group, --key1, value1, …] (host, user and password excluded, F3) and
// hands it to g_option_context_parse (design §3.4).
type GOptionRun struct {
	Tool    string
	Context *goption.Context
	Argv    []string
	Result  goption.Result
}

// Entry returns the group entry whose "--key" or value is element i of Argv,
// or -1.
func (r *GOptionRun) entryAt(g *Group, i int) int {
	for k := range g.Entries {
		if el := g.Entries[k].Element; el > 0 && (el == i || el+1 == i) {
			return k
		}
	}
	return -1
}

// OptionContext builds the GOption context of a tool for a target: the main
// group holds the options that belong to no group; the other groups follow
// in name order. The order never changes which option a key names, since
// option names are unique per tool.
func OptionContext(t Target, tool string, cs goption.Charset) *goption.Context {
	ctx := &goption.Context{Main: &goption.Group{Name: "main"}, IgnoreUnknown: t.IgnoreUnknownOptions(), Charset: cs}
	groups := map[string]*goption.Group{}
	for _, name := range t.OptionNames(tool) {
		s, ok := t.Option(tool, name)
		if !ok {
			continue
		}
		a, ok := goption.ParseArg(s.Arg)
		if !ok {
			continue // the knowledge base validates types; nothing to emulate
		}
		e := goption.Entry{Long: name, Arg: a}
		if s.Short != "" {
			e.Short = s.Short[0]
		}
		for _, f := range s.Flags {
			if fl, ok := goption.ParseFlag(f); ok {
				e.Flags |= fl
			}
		}
		g := ctx.Main
		if s.Group != "" {
			if g = groups[s.Group]; g == nil {
				g = &goption.Group{Name: s.Group}
				groups[s.Group] = g
				ctx.Groups = append(ctx.Groups, g)
			}
		}
		g.Entries = append(g.Entries, e)
	}
	sort.Slice(ctx.Groups, func(i, j int) bool { return ctx.Groups[i].Name < ctx.Groups[j].Name })
	return ctx
}

// applyGOption parses the option groups of a loadable file: tool groups
// first (mydumper reads [mydumper] before the product groups), then product
// groups. Entries get the reason GOption gives them; the first failure makes
// the file fatal at startup.
func applyGOption(m *Model, t Target, cs goption.Charset) {
	ctxs := map[string]*goption.Context{}
	for _, kind := range []GroupKind{GroupToolOptions, GroupProductOptions} {
		for i := range m.Groups {
			g := &m.Groups[i]
			if g.Kind != kind {
				continue
			}
			ctx := ctxs[g.Tool]
			if ctx == nil {
				ctx = OptionContext(t, g.Tool, cs)
				ctxs[g.Tool] = ctx
			}
			parseGroup(m, g, ctx)
		}
	}
}

func parseGroup(m *Model, g *Group, ctx *goption.Context) {
	run := &GOptionRun{Tool: g.Tool, Context: ctx, Argv: []string{g.Name}}
	for k := range g.Entries {
		e := &g.Entries[k]
		// get_keys lists every visible key, duplicates included (K13);
		// connection keys are not passed (F3).
		if e.Reason != ReasonEffective && e.Reason != ReasonShadowed {
			continue
		}
		e.Element = len(run.Argv)
		run.Argv = append(run.Argv, "--"+e.Key, e.Value)
	}
	run.Result = ctx.Parse(run.Argv, nil)
	g.GOption = run
	res := &run.Result
	for k := range g.Entries {
		e := &g.Entries[k]
		if e.Element == 0 {
			continue
		}
		switch res.Uses[e.Element] {
		case goption.UseValue:
			e.Reason = ReasonConsumedAsValue
		case goption.UseUnknown:
			e.Reason = ReasonUnknownOption
		case goption.UseAfterSeparator:
			e.Reason = ReasonAfterEndOfOptions
		case goption.UseNotReached, goption.UseOption, goption.UseLeftover, goption.UseSeparator:
		}
	}
	if !res.OK {
		if k := run.entryAt(g, res.ErrorAt); k >= 0 {
			g.Entries[k].Reason = ReasonFatalAtStartup
		}
		if m.Health == OK {
			m.Health = FatalAtStartup
			m.Fatal = g.Tool + ": option parsing failed: " + res.Error + ", try --help"
		}
	}
	for k := range g.Entries {
		g.Entries[k].Effective = g.Entries[k].Reason == ReasonEffective
	}
}
