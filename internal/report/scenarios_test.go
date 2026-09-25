package report

import "github.com/tomsihap/mydumper-lint/internal/diag"

// oddResult has a BOM, invalid UTF-8, control characters (one of them in a
// message), a NUL byte, a tab before wide characters, and a diagnostic at the
// end of a file without a final newline.
func oddResult() FileResult {
	const src = "\xef\xbb\xbf[mydumper]\nwhere=caf\xc3\xa9 \xff\x1b[31m\nrout\x00ines=1\nname=\t\U000065e5\U0000672c\v\U00008a9e\nroutines"
	ds := []diag.Diagnostic{
		{RuleID: "MDL101", RuleName: "utf8-bom", Severity: diag.Error, Span: find(src, "\xef\xbb\xbf", 0),
			Message: "file starts with a UTF-8 byte order mark", Consequence: fatal,
			Fix: fix(diag.Safe, "remove the byte order mark", edit(find(src, "\xef\xbb\xbf", 0), ""))},
		{RuleID: "MDL311", RuleName: "invalid-utf8", Severity: diag.Warning, Span: find(src, "\xff", 0),
			Message: "invalid UTF-8 byte 0xff"},
		{RuleID: "MDL312", RuleName: "control-character", Severity: diag.Warning, Span: find(src, "\x1b", 0),
			Message: "value contains the control character \x1b (ESC)"},
		{RuleID: "MDL111", RuleName: "nul-byte", Severity: diag.Error, Span: find(src, "\x00", 0),
			Message: "NUL byte in a key", Consequence: fatal},
		{RuleID: "MDL312", RuleName: "control-character", Severity: diag.Warning, Span: find(src, "\v", 0),
			Message: `value contains the control character "\v"`},
		{RuleID: "MDL104", RuleName: "missing-final-newline", Severity: diag.Error, Span: point(len(src)),
			Message: "the last line is a flag without a final newline", Consequence: fatal,
			Fix: fix(diag.Safe, "append a newline", edit(point(len(src)), "\n"))},
	}
	diag.Sort(ds)
	return FileResult{Path: "conf/odd.cnf", Source: []byte(src), Version: "v0.19.3-3", Diagnostics: ds}
}

// tailResult reports on the virtual line after the final newline.
func tailResult() FileResult {
	const src = "[mydumper]\n# see [docs]\n"
	ds := []diag.Diagnostic{
		{RuleID: "MDL112", RuleName: "bracket-leak-risk", Severity: diag.Warning, Span: point(len(src)),
			Message: "an empty line appended here would break the file",
			Related: []diag.Related{{Span: find(src, "[", 1)}}},
	}
	return FileResult{Path: "conf/tail.cnf", Source: []byte(src), Loadable: true, Version: "v1.0.8-1", Diagnostics: ds}
}

// cleanResult has no diagnostic left after two fixes.
func cleanResult() FileResult {
	return FileResult{Path: "conf/clean.cnf", Source: []byte("[mydumper]\nthreads=4\n"), Loadable: true, Version: "v0.19.3-3", Fixed: 2}
}

// testRules is the rule metadata of the fixtures. MDL999 is not used by any
// diagnostic, and MDL310 has neither summary nor documentation.
func testRules() []RuleMeta {
	rules := []RuleMeta{
		{ID: "MDL101", Name: "utf8-bom", Summary: "BOM at the start of the file", Severity: diag.Error, Fix: "safe"},
		{ID: "MDL102", Name: "whitespace-only-line", Summary: "Line made only of spaces and tabs", Severity: diag.Error, Fix: "safe"},
		{ID: "MDL103", Name: "carriage-return", Summary: "Carriage return in the file", Severity: diag.Error, Fix: "safe"},
		{ID: "MDL104", Name: "missing-final-newline", Summary: "File not ending with a newline", Severity: diag.Error, Fix: "safe"},
		{ID: "MDL108", Name: "bracket-state-leak", Summary: "Pre-processor state leak", Severity: diag.Error, Fix: "safe"},
		{ID: "MDL111", Name: "nul-byte", Summary: "NUL byte", Severity: diag.Error},
		{ID: "MDL112", Name: "bracket-leak-risk", Summary: "Line after which an empty line would break the file", Severity: diag.Info},
		{ID: "MDL205", Name: "duplicate-group", Summary: "Group declared twice", Severity: diag.Warning, Fix: "unsafe"},
		{ID: "MDL302", Name: "trailing-whitespace-in-value", Summary: "Value ending with whitespace", Severity: diag.Error, Fix: "unsafe"},
		{ID: "MDL306", Name: "leading-whitespace", Summary: "Indented line", Severity: diag.Info, Fix: "safe"},
		{ID: "MDL307", Name: "spaces-around-equals", Summary: "Spaces around =", Severity: diag.Info, Fix: "safe"},
		{ID: "MDL308", Name: "flag-without-value", Summary: "Option without =", Severity: diag.Info, Fix: "safe"},
		{ID: "MDL310", Name: "blank-lines", Severity: diag.Info, Fix: "safe"},
		{ID: "MDL311", Name: "invalid-utf8", Summary: "Invalid UTF-8 sequence", Severity: diag.Warning},
		{ID: "MDL312", Name: "control-character", Summary: "Control character", Severity: diag.Warning},
		{ID: "MDL313", Name: "quoted-value", Summary: "Value wrapped in quotes", Severity: diag.Warning, Fix: "unsafe"},
		{ID: "MDL999", Name: "unused-rule", Summary: "A rule nothing triggers", Severity: diag.Off},
	}
	for i := range rules {
		if rules[i].Summary != "" {
			rules[i].DocsURL = "https://github.com/tomsihap/mydumper-lint/blob/main/docs/rules/" + rules[i].ID + ".md"
		}
	}
	return rules
}

// scenario is one input of the golden tests.
type scenario struct {
	name    string
	results []FileResult
}

func scenarios() []scenario {
	return []scenario{
		{"files", []FileResult{appResult(), crlfResult(), oddResult(), tailResult(), cleanResult()}},
		{"clean", []FileResult{cleanResult()}},
		{"none", nil},
	}
}

func testOptions() Options {
	return Options{ToolVersion: "1.2.3", Rules: testRules()}
}
