package rules

import (
	"fmt"
	"strings"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/goption"
	"github.com/tomsihap/mydumper-lint/internal/model"
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
			keys := optionKeys(p)
			type candidate struct {
				k               *optionKey
				hint, canonical string
			}
			var unknown []candidate
			for i := range keys {
				k := &keys[i]
				if _, include := includeDirective(k.e.Key); k.entry != nil || include {
					continue // include directives: MDL602
				}
				hint, canonical := unknownOptionHint(p, k)
				unknown = append(unknown, candidate{k, hint, canonical})
			}
			// The renames of one pass are applied together: keep a rename only
			// if the group still parses with it and every rename kept before.
			// Renaming turns an ignored key into a real option, which may
			// reject the value or change how the rest of the group is parsed
			// (a value that swallowed the next key may now be consumed, G8).
			fixable := map[*optionKey]bool{}
			argvs := map[*model.Group][]string{}
			for _, c := range unknown {
				if c.canonical == "" {
					continue
				}
				run := c.k.g.GOption
				argv := argvs[c.k.g]
				if argv == nil {
					argv = run.Argv
				}
				trial := append([]string(nil), argv...)
				trial[c.k.e.Element] = "--" + c.canonical
				if !run.Result.OK || c.k.ctx.Parse(trial, nil).OK {
					argvs[c.k.g], fixable[c.k] = trial, true
				}
			}
			for _, c := range unknown {
				k := c.k
				key := k.e.Key
				msg := fmt.Sprintf("%s is not a %s option", quote([]byte(key)), k.g.Tool)
				if p.Version != "" {
					msg += " in " + p.Version
				}
				if c.hint != "" {
					msg += ": " + c.hint
				}
				d := diag.Diagnostic{Span: k.ke.KeySpan, Message: msg}
				if k.ctx.IgnoreUnknown {
					d.Consequence = fmt.Sprintf("%s ignores it silently: this version skips unknown options, so the setting has no effect.", k.g.Tool) + productNote(k.g)
				} else {
					d.Consequence = fatalConsequence(k, "Unknown option --"+key)
				}
				if fixable[k] {
					d.Fix = &diag.Fix{
						Applicability: diag.Unsafe,
						Description:   "Rename the key to `" + c.canonical + "`",
						Edits:         []diag.Edit{{Start: k.ke.KeySpan.Start, End: k.ke.KeySpan.End, New: c.canonical}},
					}
				}
				p.Report(d)
			}
		},
	})
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
