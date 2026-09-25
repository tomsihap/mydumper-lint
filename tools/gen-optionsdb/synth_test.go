package main

import (
	"strings"
	"testing"
)

// A small synthetic program with the shape the extractor expects: two
// binaries sharing files, option arrays registered in several ways, and the
// functions table keys, masking functions and products are read from. It is
// written for these tests and shares no code with mydumper.

const synthCMake = `# synthetic build
set(SHARED src/common.c src/shared_opts.c)
SET( DUMPER_SRCS src/mydumper/dumper.c ${SHARED} )
set(LOADER_SRCS src/myloader/loader.c ${SHARED})
add_executable(mydumper ${DUMPER_SRCS})
add_executable(myloader ${LOADER_SRCS})
`

const synthCommonH = `#ifndef SYNTH_COMMON_H
#define SYNTH_COMMON_H
#define KEY_BETA "beta"
#define MASK_PREFIX "mask_" "a"
extern GOptionEntry shared_entries[];
#endif
`

const synthCommon = `#include "common.h"
static int helper(void) { return 0; }

GKeyFile *load_config_file(gchar *path)
{
  /* read the file */
  return g_key_file_new();
}

void parse_key_file_group(GKeyFile *kf, GOptionContext *ctx, const gchar *group)
{
  IGNORE_HOOK
  g_option_context_parse(ctx, NULL, NULL, NULL);
}

void load_per_table_info_from_key_file(GKeyFile *kf)
{
  gchar **names = NULL;
  guint n = 0;
  names = g_key_file_get_keys(kf, "g", NULL, NULL);
  for (n = 0; names[n]; n++) {
    if (g_strcmp0(names[n], "alpha") == 0) { helper(); }
    if (!g_strcmp0(names[n], KEY_BETA)) { }
    if (0 == strcmp(names[n], "delta")) { }
    if (g_str_has_prefix(names[n], "gamma_")) { }
    TABLE_HOOK
  }
}

const gchar *get_product_name(void)
{
  switch (product) {
    case 1: return "Alpha";
    case 2: return "BETA";
    default: return "";
  }
}
`

const synthShared = `#include "common.h"
GOptionEntry shared_entries[] = {
    {"shared-one", 's', 0, G_OPTION_ARG_INT, &x, "desc "
      "continued", NULL},
    {NULL, 0, 0, G_OPTION_ARG_NONE, NULL, NULL, NULL}};

GOptionGroup *add_shared(GOptionContext *ctx)
{
  GOptionGroup *grp = g_option_group_new("sharedgrp", "d", "h", NULL, NULL);
  g_option_group_add_entries(grp, shared_entries);
  g_option_context_add_group(ctx, grp);
  return grp;
}
`

const synthDumper = `#include "common.h"
static GOptionEntry main_entries[] = {
  {"alpha", 'a', G_OPTION_FLAG_REVERSE | G_OPTION_FLAG_NOALIAS, G_OPTION_ARG_NONE, &a, "x", NULL},
  { .long_name = "beta", .arg = G_OPTION_ARG_STRING },
#ifdef WITH_SSL
  {"secure", 0, 0, G_OPTION_ARG_STRING, &s,
#ifdef LIBMARIADB
   "one", NULL},
#else
   "two", NULL},
#endif
#endif
  MAIN_HOOK
  G_OPTION_ENTRY_NULL,
  {"after-end", 0, 0, G_OPTION_ARG_NONE, &z, "x", NULL}
};

/* static GOptionEntry commented_entries[] = {
  {"ghost", 0, 0, G_OPTION_ARG_NONE, NULL, NULL, NULL}, {NULL}}; */

static GOptionEntry unused_entries[] = {{"unused", 0, 0, G_OPTION_ARG_NONE, NULL, NULL, NULL}, {NULL}};
static GOptionEntry dead_entries[] = {{"dead", 0, 0, G_OPTION_ARG_NONE, NULL, NULL, NULL}, {NULL}};

static void dead_code(GOptionGroup *g) { g_option_group_add_entries(g, dead_entries); }

fun_ptr get_function_pointer_for(gchar *spec)
{
  if (g_str_has_prefix(spec, MASK_PREFIX)) return 0;
  if (!g_strcmp0(spec, "exact_b")) return 0;
  if (!g_strcmp0(spec, "")) return 0;
  return 0;
}

static GOptionContext *build(void)
{
  GOptionContext *ctx = g_option_context_new("x");
  GOptionGroup *main_group = g_option_group_new("main", "d", "h", NULL, NULL);
  g_option_group_add_entries(main_group, main_entries);
  GOptionGroup *sg = add_shared(ctx);
  g_option_context_set_main_group(ctx, main_group);
  EXTRA_HOOK
  return ctx;
}

int main(int argc, char **argv)
{
  GOptionContext *c = build();
  parse_key_file_group(NULL, c, "g");
  return 0;
}
`

const synthLoader = `#include "common.h"
static GOptionEntry loader_entries[] = {
  {"loader-only", 'L', G_OPTION_FLAG_OPTIONAL_ARG, G_OPTION_ARG_CALLBACK, &cb, "x", NULL},
  {NULL}
};

int main(void)
{
  GOptionContext *ctx = g_option_context_new("y");
  g_option_context_add_main_entries(ctx, loader_entries, NULL);
  add_shared(ctx);
  parse_key_file_group(NULL, ctx, "g");
  return 0;
}
`

// synthTree returns the synthetic tree with the hooks replaced; hooks not
// listed are removed.
func synthTree(hooks map[string]string) srcTree {
	fill := func(s string) string {
		for _, h := range []string{"IGNORE_HOOK", "TABLE_HOOK", "MAIN_HOOK", "EXTRA_HOOK"} {
			s = strings.ReplaceAll(s, h, hooks[h])
		}
		return s
	}
	return srcTree{
		"CMakeLists.txt":         []byte(synthCMake),
		"src/common.h":           []byte(fill(synthCommonH)),
		"src/common.c":           []byte(fill(synthCommon)),
		"src/shared_opts.c":      []byte(fill(synthShared)),
		"src/mydumper/dumper.c":  []byte(fill(synthDumper)),
		"src/myloader/loader.c":  []byte(fill(synthLoader)),
		"src/myloader/unbuilt.c": []byte("int not_compiled(void) { return 0; }\n"),
	}
}

func mustExtract(t *testing.T, tree srcTree) *extraction {
	t.Helper()
	x, err := extract("vTEST", tree)
	if err != nil {
		t.Fatal(err)
	}
	return x
}
