package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"slices"
	"text/tabwriter"

	"github.com/tomsihap/mydumper-lint/internal/optionsdb"
)

type versionJSON struct {
	Tag                  string `json:"tag"`
	Date                 string `json:"date,omitempty"`
	Prerelease           bool   `json:"prerelease"`
	IgnoreUnknownOptions bool   `json:"ignore_unknown_options"`
	Preprocessor         bool   `json:"preprocessor"`
	ImageVerified        bool   `json:"image_verified"`
	Default              bool   `json:"default"`
	// ProductOptionGroups lists the tools that read [<tool>_<product>…] (F16).
	ProductOptionGroups []string `json:"product_option_groups"`
	// TableSectionsIgnored lists the tools that lose every table section (F17).
	TableSectionsIgnored []string `json:"table_sections_ignored"`
}

// versionNote is the known issue of a version, or "-".
func versionNote(v versionJSON) string {
	if slices.Contains(v.TableSectionsIgnored, "mydumper") {
		return "mydumper loses every table section (MDL511)"
	}
	return "-"
}

func runVersions(args []string, e *env) int {
	fs := flag.NewFlagSet("versions", flag.ContinueOnError)
	format := fs.String("format", "text", "Output `format`: text or json.")
	_, err := parse(fs, args)
	if errors.Is(err, errHelp) {
		fmt.Fprint(e.stdout, "Usage: mydumper-lint versions [--format text|json]\n\n"+
			"List the mydumper versions mydumper-lint knows, and the default target.\n\nFlags:\n"+flagUsage(fs))
		return ExitOK
	}
	if err != nil {
		return usageError(e, "versions", err)
	}
	if *format != "text" && *format != "json" {
		return usageError(e, "versions", fmt.Errorf("--format must be text or json, not %q", *format))
	}
	db, err := optionsdb.Load()
	if err != nil {
		fmt.Fprintf(e.stderr, "mydumper-lint: %v\n", err)
		return ExitError
	}
	def, err := db.Resolve("latest")
	if err != nil {
		fmt.Fprintf(e.stderr, "mydumper-lint: %v\n", err)
		return ExitError
	}
	out := make([]versionJSON, 0, len(db.Versions))
	for i := len(db.Versions) - 1; i >= 0; i-- {
		v := db.Versions[i]
		out = append(out, versionJSON{
			Tag: v.Tag, Date: v.Date, Prerelease: v.Prerelease, IgnoreUnknownOptions: v.IgnoreUnknownOptions,
			Preprocessor: v.Preprocessor, ImageVerified: v.ImageVerified, Default: v.Tag == def.Version.Tag,
			ProductOptionGroups: nonNil(v.ProductOptionGroups), TableSectionsIgnored: nonNil(v.TableSectionsIgnored),
		})
	}
	if *format == "json" {
		enc := json.NewEncoder(e.stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			return ExitError
		}
		return ExitOK
	}
	w := tabwriter.NewWriter(e.stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "VERSION\tRELEASED\tSTATUS\tUNKNOWN OPTIONS\tPRE-PROCESSOR\tPRODUCT GROUPS\tIMAGE CHECKED\tKNOWN ISSUE")
	for _, v := range out {
		tag, status, unknown, pre, image := v.Tag, "stable", "fatal", "yes", "yes"
		if v.Default {
			tag += " *"
		}
		if v.Prerelease {
			status = "pre-release"
		}
		if v.IgnoreUnknownOptions {
			unknown = "ignored"
		}
		if !v.Preprocessor {
			pre = "no"
		}
		if !v.ImageVerified {
			image = "no"
		}
		date := v.Date
		if date == "" {
			date = "-"
		}
		product := "no"
		if slices.Contains(v.ProductOptionGroups, "mydumper") {
			product = "yes"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", tag, date, status, unknown, pre, product, image, versionNote(v))
	}
	if err := w.Flush(); err != nil {
		return ExitError
	}
	fmt.Fprintf(e.stdout, "\nPRODUCT GROUPS: whether mydumper reads [mydumper_<product>…]; myloader never reads its own.\n"+
		"* default target: %s, the latest stable release. Pin yours with --mydumper-version\n"+
		"  or `mydumper-version:` in .mydumper-lint.yaml; accepted forms: v0.19.3-3, 0.19.3, 0.19, latest,\n"+
		"  latest-prerelease.\n", def.Version.Tag)
	return ExitOK
}
