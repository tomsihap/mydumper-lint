package rules

import (
	"fmt"
	"strings"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/keyfile"
	"github.com/tomsihap/mydumper-lint/internal/model"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL305", Name: "localized-key", Family: FamilyValues,
			Severity: diag.Warning,
			Summary:  "A key with a `[locale]` suffix depends on the runtime language.",
			Why: "GLib treats `threads[fr]=4` as a translation of `threads`: the key only exists when " +
				"the language list of the mydumper process (from LANGUAGE, LC_ALL, LC_MESSAGES and LANG) " +
				"contains `fr`. When it does, mydumper passes `--threads[fr]`, an unknown option: fatal " +
				"at startup up to v1.0.0-1, ignored afterwards. `[C]` is always in the list. Either way " +
				"`threads` itself is not set.",
			Refs: []string{"K14"},
		},
		Check: func(p *Pass) {
			languages := p.Languages
			if languages == nil {
				languages = []string{"C"}
			}
			kinds := map[string]model.GroupKind{}
			for _, g := range p.Model.Groups {
				kinds[g.Name] = g.Kind
			}
			for _, e := range p.KF.Entries {
				if e.Locale == "" || !p.KF.Groups[e.Group].Valid {
					continue
				}
				base := strings.TrimSuffix(e.Key, "["+e.Locale+"]")
				visible := keyfile.IsInterestingLocale(e.Locale, languages)
				d := diag.Diagnostic{Span: e.KeySpan}
				if visible {
					d.Message = fmt.Sprintf("`%s` is a translation of `%s` for the %s locale, which mydumper's language list contains", e.Key, base, e.Locale)
				} else {
					d.Message = fmt.Sprintf("`%s` is a translation of `%s`: it only exists when mydumper runs with a %s language", e.Key, base, e.Locale)
				}
				switch kind := kinds[p.KF.Groups[e.Group].Name]; kind {
				case model.GroupToolOptions, model.GroupProductOptions:
					d.Consequence = fmt.Sprintf("Where it is visible, mydumper passes the unknown option --%s (fatal at startup up to v1.0.0-1); `%s` is never set.", e.Key, base)
				case model.GroupUnknown, model.GroupSessionVariables, model.GroupGlobalVariables, model.GroupClient, model.GroupTable:
					d.Consequence = fmt.Sprintf("`%s` is never set by this line.", base)
				}
				p.Report(d)
			}
		},
	})
}
