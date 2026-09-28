package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/tomsihap/mydumper-lint/internal/keyfile"
	"github.com/tomsihap/mydumper-lint/internal/model"
	"github.com/tomsihap/mydumper-lint/internal/target"
)

// inspectSetDoc is the JSON document printed by inspect --load-set: what one
// invocation applies from a defaults file and an extra file (design §9.3).
type inspectSetDoc struct {
	MydumperVersion string              `json:"mydumper_version"`
	Preprocessor    bool                `json:"preprocessor"`
	Defaults        inspectSetFile      `json:"defaults_file"`
	Extra           inspectSetFile      `json:"extra_file"`
	Health          string              `json:"health"`
	Fatal           string              `json:"fatal,omitempty"`
	Model           []inspectSetGroup   `json:"model"`
	Connections     []inspectConnection `json:"connections"`
}

type inspectSetFile struct {
	Path     string        `json:"path"`
	Loadable bool          `json:"loadable"`
	Error    *inspectError `json:"error"`
}

type inspectSetGroup struct {
	Name          string            `json:"name"`
	Kind          string            `json:"kind"`
	Tool          string            `json:"tool,omitempty"`
	DefaultsLines []int             `json:"defaults_lines"`
	ExtraLines    []int             `json:"extra_lines"`
	Entries       []inspectSetEntry `json:"entries"`
}

type inspectSetEntry struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	File      string `json:"file"` // "defaults" or "extra"
	Line      int    `json:"line"`
	Effective bool   `json:"effective"`
	Reason    string `json:"reason"`
}

// inspectConnection says which file the MySQL client library reads when the
// tool connects: File is empty when it reads none.
type inspectConnection struct {
	Tool   string   `json:"tool"`
	File   string   `json:"file,omitempty"`
	Groups []string `json:"groups"`
}

func setFile(path string, kf *keyfile.Result) inspectSetFile {
	f := inspectSetFile{Path: path, Loadable: kf.Loadable}
	if kf.FirstError != nil {
		f.Error = &inspectError{Line: kf.FirstError.Line, Message: kf.FirstError.Message}
	}
	return f
}

func buildInspectSet(dPath string, dSrc []byte, xPath string, xSrc []byte, t target.Target, languages []string) inspectSetDoc {
	v := t.View.Version()
	dkf, xkf := parseForInspect(dPath, dSrc, v.Preprocessor), parseForInspect(xPath, xSrc, v.Preprocessor)
	s := model.BuildSet(dkf, xkf, model.Options{Languages: languages, Target: t.View})
	doc := inspectSetDoc{
		MydumperVersion: v.Tag, Preprocessor: v.Preprocessor,
		Defaults: setFile(dPath, dkf), Extra: setFile(xPath, xkf),
		Health: s.Health.String(), Fatal: s.Fatal, Model: []inspectSetGroup{},
	}
	for _, g := range s.Groups {
		ig := inspectSetGroup{
			Name: g.Name, Kind: g.Kind.String(), Tool: g.Tool,
			DefaultsLines: nonNil(g.DefaultsLines), ExtraLines: nonNil(g.ExtraLines), Entries: []inspectSetEntry{},
		}
		for _, e := range g.Entries {
			ig.Entries = append(ig.Entries, inspectSetEntry{
				Key: e.Key, Value: e.Value, File: e.Origin.String(), Line: e.Line, Effective: e.Effective, Reason: e.Reason.String(),
			})
		}
		doc.Model = append(doc.Model, ig)
	}
	for _, c := range s.Connections {
		ic := inspectConnection{Tool: c.Tool, Groups: []string{}}
		if c.Read {
			ic.File = dPath
			if c.From == model.FromExtra {
				ic.File = xPath
			}
			ic.Groups = append(ic.Groups, "client")
			if c.Group != "" {
				ic.Groups = append(ic.Groups, c.Group)
			}
		}
		doc.Connections = append(doc.Connections, ic)
	}
	return doc
}

func printInspectSet(w io.Writer, d inspectSetDoc) {
	fmt.Fprintf(w, "Load set: %s, then %s\n", d.Defaults.Path, d.Extra.Path)
	fmt.Fprintf(w, "  mydumper version: %s\n", d.MydumperVersion)
	if !d.Preprocessor {
		fmt.Fprintln(w, "  this version has no pre-processor: GLib reads the files as written")
	}
	for _, f := range []inspectSetFile{d.Defaults, d.Extra} {
		if f.Loadable {
			fmt.Fprintf(w, "  %s: GLib loads the file\n", f.Path)
			continue
		}
		fmt.Fprintf(w, "  %s: GLib REJECTS the file (line %d: %s)\n", f.Path, f.Error.Line, strings.ReplaceAll(f.Error.Message, "\r", `\r`))
	}
	if !d.Defaults.Loadable && d.Extra.Loadable {
		fmt.Fprintln(w, "  the extra file is merged into the rejected defaults file: its table sections, variable groups and per-product groups are ignored")
	}
	switch d.Health {
	case model.FatalAtStartup.String():
		fmt.Fprintf(w, "  mydumper aborts at startup: %s\n", d.Fatal)
	case model.Rejected.String():
		fmt.Fprintln(w, "  nothing applies: GLib rejects both files")
	}
	fmt.Fprintln(w, "\nEffective configuration of the invocation:")
	if len(d.Model) == 0 {
		fmt.Fprintln(w, "  (no groups)")
	}
	for _, g := range d.Model {
		tool := ""
		if g.Tool != "" {
			tool = ", " + g.Tool
		}
		fmt.Fprintf(w, "  [%s]  (%s%s)\n", g.Name, g.Kind, tool)
		for _, e := range g.Entries {
			file := d.Defaults.Path
			if e.File == model.FromExtra.String() {
				file = d.Extra.Path
			}
			mark, reason := "  ", ""
			if !e.Effective {
				mark, reason = "✗ ", "  <- "+e.Reason
			}
			fmt.Fprintf(w, "    %s%s = %q  (%s:%d)%s\n", mark, e.Key, e.Value, file, e.Line, reason)
		}
	}
	fmt.Fprintln(w, "\nConnection (the MySQL client library reads one file, with its own parser: a file GLib\nrejects can still provide [client]):")
	for _, c := range d.Connections {
		if c.File == "" {
			fmt.Fprintf(w, "  %s: no defaults file (neither file has [client] or [%s])\n", c.Tool, c.Tool)
			continue
		}
		fmt.Fprintf(w, "  %s: [%s] of %s\n", c.Tool, strings.Join(c.Groups, "] and ["), c.File)
	}
}
