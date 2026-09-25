package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// describeOptions renders the options of a tool as "name short arg flags
// group [cond]" lines.
func describeOptions(x *extraction, tool string) []string {
	var out []string
	for _, name := range sortedKeys(x.options[tool]) {
		for _, v := range x.options[tool][name] {
			out = append(out, fmt.Sprintf("%s short=%s arg=%s flags=%s group=%s cond=%s",
				name, v.def.Short, v.def.Arg, strings.Join(v.def.Flags, ","), v.def.Group, v.cond))
		}
	}
	return out
}

func TestExtractOptions(t *testing.T) {
	x := mustExtract(t, synthTree(nil))
	tests := []struct {
		tool string
		want []string
	}{
		{"mydumper", []string{
			"alpha short=a arg=none flags=noalias,reverse group= cond=",
			"beta short= arg=string flags= group= cond=",
			// the entry is split by #ifdef LIBMARIADB, but both halves
			// define the same option: only WITH_SSL matters
			"secure short= arg=string flags= group= cond=WITH_SSL",
			"shared-one short=s arg=int flags= group=sharedgrp cond=",
		}},
		{"myloader", []string{
			"loader-only short=L arg=callback flags=optional_arg group= cond=",
			"shared-one short=s arg=int flags= group=sharedgrp cond=",
		}},
	}
	for _, tt := range tests {
		if got := describeOptions(x, tt.tool); !slices.Equal(got, tt.want) {
			t.Errorf("%s options:\n got  %q\n want %q", tt.tool, got, tt.want)
		}
	}
	notes := strings.Join(x.notes, "\n")
	for _, want := range []string{"unused_entries", "dead_entries"} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes do not mention %s:\n%s", want, notes)
		}
	}
	for _, not := range []string{"ghost", "commented_entries", "after-end"} {
		if strings.Contains(notes, not) || len(x.options["mydumper"][not]) > 0 {
			t.Errorf("%s must be invisible (comment, or after the terminator)", not)
		}
	}
	if src := x.options["mydumper"]["alpha"][0].source; src != "src/mydumper/dumper.c:3" {
		t.Errorf("alpha source = %s", src)
	}
}

func TestExtractFacts(t *testing.T) {
	x := mustExtract(t, synthTree(nil))
	if got := factList(x.tableKeys); got != "alpha beta delta gamma_*" {
		t.Errorf("table keys = %q", got)
	}
	if got := factList(x.masquerade); got != "exact_b mask_a*" {
		t.Errorf("masking functions = %q", got)
	}
	if got := strings.Join(x.products, " "); got != "alpha beta" {
		t.Errorf("products = %q", got)
	}
	if x.ignoreUnknown {
		t.Error("ignoreUnknown = true without the call")
	}
	y := mustExtract(t, synthTree(map[string]string{"IGNORE_HOOK": "g_option_context_set_ignore_unknown_options(ctx, TRUE);"}))
	if !y.ignoreUnknown {
		t.Error("ignoreUnknown = false with the call in parse_key_file_group")
	}
	if len(x.fingerprint) != 64 || x.fingerprint == y.fingerprint {
		t.Errorf("fingerprints %s / %s: a token change in parse_key_file_group must show", x.fingerprint, y.fingerprint)
	}
	if x.funcPrints["load_config_file"] != y.funcPrints["load_config_file"] {
		t.Error("load_config_file did not change")
	}
}

func TestFingerprintIgnoresLayout(t *testing.T) {
	base := synthTree(nil)
	a := mustExtract(t, base)
	moved := synthTree(nil)
	moved["src/common.c"] = []byte(strings.Replace(string(moved["src/common.c"]),
		"GKeyFile *load_config_file(gchar *path)\n{\n  /* read the file */\n  return g_key_file_new();\n}",
		"GKeyFile *\nload_config_file (gchar *path) { // new comment\n\treturn   g_key_file_new ( ) ;}", 1))
	b := mustExtract(t, moved)
	if a.fingerprint != b.fingerprint {
		t.Error("layout and comments changed the fingerprint")
	}
	directive := synthTree(nil)
	directive["src/common.c"] = []byte(strings.Replace(string(directive["src/common.c"]),
		"  return g_key_file_new();", "#ifdef NEVER_SET\n  return NULL;\n#endif\n  return g_key_file_new();", 1))
	c := mustExtract(t, directive)
	if a.fingerprint == c.fingerprint {
		t.Error("a conditional block in load_config_file did not change the fingerprint")
	}
}

func TestExtractErrors(t *testing.T) {
	tests := []struct {
		name  string
		hooks map[string]string
		want  string
	}{
		{"ignore-unknown elsewhere", map[string]string{"EXTRA_HOOK": "g_option_context_set_ignore_unknown_options(ctx, TRUE);"},
			"not in parse_key_file_group"},
		{"ignore-unknown with a variable", map[string]string{"IGNORE_HOOK": "g_option_context_set_ignore_unknown_options(ctx, flag);"},
			"unexpected value"},
		{"undecidable condition in an array", map[string]string{"MAIN_HOOK": "#if SOME_CHECK(1)\n{\"cond\", 0, 0, G_OPTION_ARG_NONE, NULL, NULL, NULL},\n#endif"},
			"cannot evaluate"},
		{"unknown array", map[string]string{"EXTRA_HOOK": "g_option_group_add_entries(main_group, nowhere_entries);"},
			"no GOptionEntry array of that name"},
		{"duplicate option", map[string]string{"MAIN_HOOK": "{\"beta\", 0, 0, G_OPTION_ARG_INT, NULL, NULL, NULL},"},
			"registered twice"},
		{"unknown flag", map[string]string{"MAIN_HOOK": "{\"f\", 0, G_OPTION_FLAG_BOGUS, G_OPTION_ARG_NONE, NULL, NULL, NULL},"},
			"unknown flag"},
		{"unknown arg type", map[string]string{"MAIN_HOOK": "{\"f\", 0, 0, G_OPTION_ARG_BOGUS, NULL, NULL, NULL},"},
			"unknown argument type"},
		{"non-literal long name", map[string]string{"MAIN_HOOK": "{name_var, 0, 0, G_OPTION_ARG_NONE, NULL, NULL, NULL},"},
			"not a string"},
		{"case-insensitive table key", map[string]string{"TABLE_HOOK": "if (!g_ascii_strcasecmp(names[n], \"eps\")) { }"},
			"unsupported comparison"},
		{"inverted comparison", map[string]string{"TABLE_HOOK": "if (g_strcmp0(names[n], \"eps\")) { }"},
			"not used as an equality test"},
		{"group from a parameter", map[string]string{"EXTRA_HOOK": "dead_code(main_group);"},
			"is a parameter"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := extract("vTEST", synthTree(tt.hooks))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestExtractMissingTerminator(t *testing.T) {
	tree := synthTree(nil)
	tree["src/myloader/loader.c"] = []byte(strings.Replace(string(tree["src/myloader/loader.c"]), "  {NULL}\n", "", 1))
	_, err := extract("vTEST", tree)
	if err == nil || !strings.Contains(err.Error(), "no terminating entry") {
		t.Errorf("err = %v", err)
	}
}

func TestGroupNeverAdded(t *testing.T) {
	x := mustExtract(t, synthTree(map[string]string{"EXTRA_HOOK": `GOptionGroup *lonely = g_option_group_new("lonely", "d", "h", NULL, NULL);
  g_option_group_add_entries(lonely, unused_entries);`}))
	if _, ok := x.options["mydumper"]["unused"]; ok {
		t.Error("an array added to a group outside any context became an option")
	}
	if !strings.Contains(strings.Join(x.notes, "\n"), `"lonely", which never reaches an option context`) {
		t.Errorf("notes: %q", x.notes)
	}
}

func TestBinarySources(t *testing.T) {
	tree := synthTree(nil)
	var paths []string
	for p := range tree {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	srcs, notes, err := binarySources(tree, paths)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(srcs["mydumper"], " "); got != "src/common.c src/mydumper/dumper.c src/shared_opts.c" {
		t.Errorf("mydumper sources = %s", got)
	}
	// src/myloader/unbuilt.c is under src/myloader/ but not compiled
	if len(notes) != 1 || !strings.Contains(notes[0], "src/myloader/unbuilt.c") {
		t.Errorf("notes = %q", notes)
	}
	delete(tree, "CMakeLists.txt")
	if _, _, err := binarySources(tree, paths); err == nil {
		t.Error("missing CMakeLists.txt: no error")
	}
}

func TestPathTool(t *testing.T) {
	for p, want := range map[string]string{
		"src/mydumper/x.c": "mydumper", "src/myloader/y.c": "myloader", "src/common.c": "both", "src/sub/z.c": "both",
	} {
		if got := pathTool(p); got != want {
			t.Errorf("pathTool(%s) = %s, want %s", p, got, want)
		}
	}
}
