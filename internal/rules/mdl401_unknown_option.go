package rules

import (
	"fmt"
	"strings"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/goption"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL401", Name: "unknown-option", Family: FamilyOptions,
			Severity: diag.Error, Fix: diag.Unsafe,
			Summary: "A key of [mydumper] or [myloader] is not an option of the target version.",
			Why: "mydumper passes every key of its group to GLib's option parser as `--key value`. A " +
				"name that is not one of the version's options is an unknown option: mydumper up to " +
				"v1.0.0-1 aborts at startup (`option parsing failed: Unknown option --key`), later " +
				"versions ignore it without a word, so the setting silently does nothing.\n\nCommon " +
				"causes: leading dashes (`--threads=4` becomes `----threads`), a short name (`t` instead " +
				"of `threads`), case (`Routines`), `_` instead of `-` (`split_string_pk`), an option of " +
				"the other tool (`outputdir` in [myloader]), or an option added or removed in another " +
				"version. The fix renames the key when exactly one option matches after normalization.",
			Refs: []string{"F6", "G3", "G4", "G13"},
			E2E:  []string{"mdl401-unknown-option-fatal-or-ignored"},
		},
		Check: func(p *Pass) {
			for _, k := range optionKeys(p) {
				if _, include := includeDirective(k.e.Key); k.entry != nil || include {
					continue // include directives: MDL602
				}
				key := k.e.Key
				msg := fmt.Sprintf("%s is not a %s option", quote([]byte(key)), k.g.Tool)
				if p.Version != "" {
					msg += " in " + p.Version
				}
				hint, canonical := unknownOptionHint(p, &k)
				if hint != "" {
					msg += ": " + hint
				}
				d := diag.Diagnostic{Span: k.ke.KeySpan, Message: msg}
				if k.ctx.IgnoreUnknown {
					d.Consequence = fmt.Sprintf("%s ignores it silently: this version skips unknown options, so the setting has no effect.", k.g.Tool) + productNote(k.g)
				} else {
					d.Consequence = fatalConsequence(&k, "Unknown option --"+key)
				}
				if canonical != "" && renameKeepsWorking(&k, canonical) {
					d.Fix = &diag.Fix{
						Applicability: diag.Unsafe,
						Description:   "Rename the key to `" + canonical + "`",
						Edits:         []diag.Edit{{Start: k.ke.KeySpan.Start, End: k.ke.KeySpan.End, New: canonical}},
					}
				}
				p.Report(d)
			}
		},
	})
}

// renameKeepsWorking reports whether renaming the key to canonical gives an
// option that accepts the line's value. Renaming turns an ignored key into
// a real option; if the option then rejected the value, or parsed a value
// starting with '-' as options (G8), the fix would make mydumper abort.
func renameKeepsWorking(k *optionKey, canonical string) bool {
	r, _, ok := k.ctx.Lookup(canonical)
	if !ok {
		return false
	}
	e, v := k.ctx.Entry(r), k.ke.Value
	if (e.NoArg() || e.OptionalArg()) && len(v) >= 2 && v[0] == '-' {
		return false
	}
	return k.ctx.Check(r, "--"+e.Long, v) == ""
}

// unknownOptionHint explains an unknown key and, when exactly one option
// matches after normalization, returns its name.
func unknownOptionHint(p *Pass, k *optionKey) (hint, canonical string) {
	key := k.e.Key
	known := func(name string) bool { _, _, ok := k.ctx.Lookup(name); return ok }
	if stripped := strings.TrimLeft(key, "-"); stripped != key && stripped != "" && known(stripped) {
		return fmt.Sprintf("write `%s` without the dashes: mydumper adds `--` itself", stripped), stripped
	}
	if len(key) == 1 {
		for _, g := range allGroups(k.ctx) {
			for _, e := range g.Entries {
				if e.Short == key[0] {
					return fmt.Sprintf("`%s` is the short name of `%s`; configuration files need the long name", key, e.Long), e.Long
				}
			}
		}
	}
	norm := strings.ReplaceAll(lowerASCII(strings.TrimLeft(key, "-")), "_", "-")
	if norm != key && known(norm) {
		var why []string
		if lowerASCII(key) != key {
			why = append(why, "option names are case-sensitive")
		}
		if strings.Contains(key, "_") {
			why = append(why, "GLib does not treat `_` and `-` alike")
		}
		if strings.HasPrefix(key, "-") {
			why = append(why, "mydumper adds `--` itself")
		}
		return fmt.Sprintf("the option is `%s` (%s)", norm, strings.Join(why, "; ")), norm
	}
	other := otherTool(k.g.Tool)
	if _, ok := p.Target.Option(other, key); ok {
		return fmt.Sprintf("`%s` is a %s option; put it in [%s]", key, other, other), ""
	}
	if h := optionHistory(p, k.g.Tool, key); h != "" {
		return h, ""
	}
	best, bestD := "", 3
	for _, g := range allGroups(k.ctx) {
		for _, e := range g.Entries {
			if d := Levenshtein(norm, e.Long); d < bestD {
				best, bestD = e.Long, d
			}
		}
	}
	if best != "" {
		return fmt.Sprintf("did you mean `%s`?", best), ""
	}
	return "", ""
}

// allGroups lists a context's main group, then its other groups.
func allGroups(ctx *goption.Context) []*goption.Group {
	return append([]*goption.Group{ctx.Main}, ctx.Groups...)
}
