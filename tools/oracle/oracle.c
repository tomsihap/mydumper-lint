// SPDX-License-Identifier: GPL-3.0-or-later
// Test-only tool. Contains a behavioural copy of load_config_file() from
// mydumper (GPL-3.0, https://github.com/mydumper/mydumper, src/common.c).
// It is NEVER linked into the mydumper-lint binary. Build:
//   cc -o oracle oracle.c $(pkg-config --cflags --libs glib-2.0)

// Oracle: reproduces mydumper's load_config_file() (src/common.c @ c01ca9d)
// byte for byte, then dumps what GLib's GKeyFile actually sees.
// Usage: oracle FILE            -> "OK" + groups/keys/values, or "ERROR: ..."
//        oracle --goption       -> GOptionContext behaviour probes
#include <glib.h>
#include <stdio.h>
#include <string.h>

static GString *mydumper_preprocess(const gchar *contents, gsize length)
{
  const gchar *current_contents = contents;
  GString *new_content = g_string_sized_new(length);
  gboolean equal_found = FALSE, new_line = TRUE;
  while ((unsigned int)(current_contents - contents) < length)
  {
    if (current_contents[0] == '[')
    {
      while (((unsigned int)(current_contents - contents) < length) && current_contents[0] != '\n')
      {
        g_string_append_c(new_content, current_contents[0]);
        current_contents++;
      }
    }
    else
    {
      if (current_contents[0] == '\n')
      {
        if (!equal_found && !new_line)
          g_string_append(new_content, "= 1");
        new_line = TRUE;
        equal_found = FALSE;
      }
      else
      {
        if (current_contents[0] == '=')
          equal_found = TRUE;
        new_line = FALSE;
      }
    }
    g_string_append_c(new_content, current_contents[0]);
    current_contents++;
  }
  return new_content;
}

static gchar *compress_val = (gchar *)"unset";
static gboolean cb(const gchar *n, const gchar *v, gpointer d, GError **e)
{ (void)n; (void)d; (void)e; compress_val = v ? g_strdup(v) : (gchar *)"NULL"; return TRUE; }
static int probe_goption(void)
{
  gboolean routines = FALSE, split = FALSE;
  gint chunk = -1;
  GOptionEntry entries[] = {
      {"routines", 'R', 0, G_OPTION_ARG_NONE, &routines, "", NULL},
      {"split-string-pk", 0, 0, G_OPTION_ARG_NONE, &split, "", NULL},
      {"chunk-filesize", 'F', 0, G_OPTION_ARG_INT, &chunk, "", NULL},
      {"compress", 'c', G_OPTION_FLAG_OPTIONAL_ARG, G_OPTION_ARG_CALLBACK, &cb, "", NULL},
      {NULL, 0, 0, G_OPTION_ARG_NONE, NULL, NULL, NULL}};
  const gchar *cases[][4] = {
      {"g", "--routines", "0", NULL},
      {"g", "--routines", "false", NULL},
      {"g", "--split_string_pk", "1", NULL},
      {"g", "--chunk_filesize", "10", NULL},
      {"g", "--Routines", "1", NULL},
      {"g", "--chunk-filesize", "10 ", NULL},
      {"g", "--compress", "ZSTD", NULL},
  };
  for (gsize i = 0; i < G_N_ELEMENTS(cases); i++)
  {
    routines = FALSE; split = FALSE; chunk = -1;
    GOptionContext *ctx = g_option_context_new("");
    g_option_context_add_main_entries(ctx, entries, NULL);
    g_option_context_set_ignore_unknown_options(ctx, TRUE);
    gchar **argv = g_strdupv((gchar **)cases[i]);
    GError *err = NULL;
    gboolean ok = g_option_context_parse_strv(ctx, &argv, &err);
    gchar *left = g_strjoinv("|", argv);
    printf("compress=%s ", compress_val); compress_val = (gchar *)"unset";
    printf("%-28s ok=%d routines=%d split=%d chunk=%d leftover=[%s] err=%s\n",
           g_strjoinv(" ", (gchar **)cases[i] + 1), ok, routines, split, chunk, left,
           err ? err->message : "-");
  }
  return 0;
}

int main(int argc, char **argv)
{
  if (argc == 2 && !strcmp(argv[1], "--goption"))
    return probe_goption();
  gchar *contents = NULL;
  gsize length = 0;
  GError *error = NULL;
  if (!g_file_get_contents(argv[1], &contents, &length, &error))
  {
    printf("IOERROR: %s\n", error->message);
    return 2;
  }
  GString *pre = mydumper_preprocess(contents, length);
  GKeyFile *kf = g_key_file_new();
  if (!g_key_file_load_from_data(kf, pre->str, pre->len, G_KEY_FILE_KEEP_COMMENTS, &error))
  {
    printf("ERROR: %s\n", error->message);
    return 1;
  }
  gsize ng = 0;
  gchar **groups = g_key_file_get_groups(kf, &ng);
  printf("OK groups=%zu\n", ng);
  for (gsize i = 0; i < ng; i++)
  {
    gsize nk = 0;
    gchar **keys = g_key_file_get_keys(kf, groups[i], &nk, NULL);
    printf("  [%s] keys=%zu\n", groups[i], nk);
    for (gsize j = 0; j < nk; j++)
      printf("    <%s>=<%s>\n", keys[j], g_key_file_get_value(kf, groups[i], keys[j], NULL));
  }
  return 0;
}
