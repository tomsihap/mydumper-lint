package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/tomsihap/mydumper-lint/internal/rules"
)

type ruleJSON struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Family         string   `json:"family"`
	Severity       string   `json:"severity"`
	Fix            string   `json:"fix"`
	OptIn          bool     `json:"opt_in"`
	Preview        bool     `json:"preview"`
	Unsuppressible bool     `json:"unsuppressible"`
	Summary        string   `json:"summary"`
	Why            string   `json:"why"`
	Refs           []string `json:"references"`
	URL            string   `json:"url"`
}

func toJSON(r *rules.Rule) ruleJSON {
	fix := "none"
	if r.Fix != 0 {
		fix = r.Fix.String()
	}
	return ruleJSON{
		ID: r.ID, Name: r.Name, Family: string(r.Family), Severity: r.Severity.String(), Fix: fix,
		OptIn: r.OptIn, Preview: r.Preview, Unsuppressible: r.Unsuppressible,
		Summary: r.Summary, Why: r.Why, Refs: nonNil(r.Refs),
		URL: "https://github.com/tomsihap/mydumper-lint/blob/main/docs/rules/" + r.ID + ".md",
	}
}

// nonNil makes JSON print [] rather than null.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func runRules(args []string, e *env) int {
	fs := flag.NewFlagSet("rules", flag.ContinueOnError)
	format := fs.String("format", "text", "Output `format`: text or json.")
	_, err := parse(fs, args)
	if errors.Is(err, errHelp) {
		fmt.Fprint(e.stdout, "Usage: mydumper-lint rules [--format text|json]\n\nList every rule.\n\nFlags:\n"+flagUsage(fs))
		return ExitOK
	}
	if err != nil {
		return usageError(e, "rules", err)
	}
	all := rules.All()
	switch *format {
	case "json":
		out := make([]ruleJSON, len(all))
		for i, r := range all {
			out[i] = toJSON(r)
		}
		enc := json.NewEncoder(e.stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			return ExitError
		}
	case "text":
		tw := tabwriter.NewWriter(e.stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tNAME\tSEVERITY\tFIX\tSUMMARY")
		for _, r := range all {
			j := toJSON(r)
			var tags []string
			if r.OptIn {
				tags = append(tags, "opt-in")
			}
			if r.Preview {
				tags = append(tags, "preview")
			}
			summary := r.Summary
			if len(tags) > 0 {
				summary = "(" + strings.Join(tags, ", ") + ") " + summary
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", r.ID, r.Name, j.Severity, j.Fix, summary)
		}
		if err := tw.Flush(); err != nil {
			return ExitError
		}
	default:
		return usageError(e, "rules", fmt.Errorf("--format must be text or json, not %q", *format))
	}
	return ExitOK
}

func runExplain(args []string, e *env) int {
	fs := flag.NewFlagSet("explain", flag.ContinueOnError)
	format := fs.String("format", "text", "Output `format`: text or json.")
	pos, err := parse(fs, args)
	if errors.Is(err, errHelp) {
		fmt.Fprint(e.stdout, "Usage: mydumper-lint explain RULE\n\nShow the documentation of a rule (ID such as MDL102, or name).\n\nFlags:\n"+flagUsage(fs))
		return ExitOK
	}
	if err != nil {
		return usageError(e, "explain", err)
	}
	if len(pos) != 1 {
		return usageError(e, "explain", errors.New("expected one rule ID or name"))
	}
	r, ok := rules.Lookup(pos[0])
	if !ok {
		msg := fmt.Sprintf("unknown rule %q", pos[0])
		if s := rules.Suggest(pos[0]); s != "" {
			msg += fmt.Sprintf(" (did you mean %q?)", s)
		}
		return usageError(e, "explain", errors.New(msg))
	}
	j := toJSON(r)
	if *format == "json" {
		enc := json.NewEncoder(e.stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(j); err != nil {
			return ExitError
		}
		return ExitOK
	}
	fmt.Fprintf(e.stdout, "%s %s\n\n%s\n\nDefault severity: %s. Fix: %s.", r.ID, r.Name, r.Summary, j.Severity, j.Fix)
	if r.OptIn {
		fmt.Fprint(e.stdout, " Opt-in: enable it with --extend-select "+r.ID+".")
	}
	if r.Unsuppressible {
		fmt.Fprint(e.stdout, " Cannot be suppressed by a comment.")
	}
	fmt.Fprintf(e.stdout, "\n\n%s\n", r.Why)
	if len(r.Refs) > 0 {
		fmt.Fprintf(e.stdout, "\nReferences (design document): %s\n", strings.Join(r.Refs, "; "))
	}
	fmt.Fprintf(e.stdout, "\nMore: %s\n", j.URL)
	return ExitOK
}
