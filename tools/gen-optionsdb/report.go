package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// printReport prints what a reviewer of the generated file needs: the
// version table, disagreements, extraction notes, the changes between
// versions and, after -verify-images, the cross-check table.
func printReport(w io.Writer, cands []candidate, verified map[string]bool, parity []string,
	xs []*extraction, rels []release, overlayWarnings []string, checked []imageRecord) {
	fmt.Fprintf(w, "Upstream tags since %s: %d\n", cands[0].tag, len(cands))
	for _, c := range cands {
		fmt.Fprintf(w, "  %s\n", c.describe(verified[c.tag]))
	}
	section(w, "Pre-release status vs upstream's even-PATCH rule", parity)

	var notes []string
	seen := map[string]string{}
	for _, x := range xs {
		for _, n := range x.notes {
			if first, ok := seen[n]; ok {
				_ = first
				continue
			}
			seen[n] = x.tag
			notes = append(notes, x.tag+": "+n)
		}
	}
	section(w, "Extraction notes (first version where each appears)", notes)
	section(w, "Changes between embedded versions", changes(rels, xs))
	section(w, "Overlay warnings", overlayWarnings)
	if len(checked) > 0 {
		fmt.Fprintf(w, "\nImage cross-check (official build: MySQL client, WITH_SSL):\n")
		fmt.Fprintf(w, "  %-11s %-11s %-11s %-8s %s\n", "tag", "mydumper", "myloader", "verified", "problems")
		for _, r := range checked {
			fmt.Fprintf(w, "  %-11s %-11s %-11s %-8s %s\n", r.Tag, orDash(r.Mydumper), orDash(r.Myloader), yesNo(r.Verified), joinProblems(r.Problems))
		}
	}
	fmt.Fprintln(w)
}

func section(w io.Writer, title string, lines []string) {
	if len(lines) == 0 {
		return
	}
	fmt.Fprintf(w, "\n%s:\n", title)
	for _, l := range lines {
		fmt.Fprintf(w, "  %s\n", l)
	}
}

func joinProblems(ps []string) string {
	if len(ps) == 0 {
		return "-"
	}
	return strings.Join(ps, "; ")
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// encodeJSON renders a committed JSON file: 2-space indentation, final
// newline, and no HTML escaping (conditions contain "&&").
func encodeJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
