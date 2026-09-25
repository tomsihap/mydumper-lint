// SPDX-License-Identifier: GPL-3.0-or-later
// Test-only tool. Contains a behavioural copy of load_config_file() from
// mydumper (GPL-3.0, https://github.com/mydumper/mydumper, src/common.c).
// It is NEVER linked into the mydumper-lint binary. Build:
//   cc -o oracle oracle.c $(pkg-config --cflags --libs glib-2.0)

// Oracle: reproduces mydumper's load_config_file() (src/common.c @ c01ca9d)
// byte for byte, then dumps what GLib's GKeyFile actually sees. It also runs
// GOption on argv vectors built the way mydumper builds them from a group.
// Usage (tools/oracle/README.md documents every mode and wire format):
//   oracle [ENV...] FILE                    -> "OK" + groups/keys/values, or "ERROR: ..."
//   oracle [ENV...] --json FILE             -> the same verdict as one JSON object
//   oracle [ENV...] --serve                 -> length-prefixed requests on stdin
//   oracle [ENV...] --goption               -> GOptionContext behaviour probes
//   oracle [ENV...] --goption-cases FILE... -> data-driven GOption cases, JSON lines
//   oracle --glib-version                   -> runtime GLib version
//   ENV: --setenv NAME=VALUE | --unsetenv NAME, applied in order before anything runs
#define GLIB_VERSION_MIN_REQUIRED GLIB_VERSION_2_68
#define GLIB_VERSION_MAX_ALLOWED GLIB_VERSION_2_68
#include <glib.h>
#include <locale.h>
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
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

// load_config_file() without the I/O: pre-process, then hand the result to
// GKeyFile. Returns NULL and sets *error when GLib rejects the content.
// contents[length] must be readable and hold the NUL that g_file_get_contents
// appends: when a '[' copy reaches the end of the buffer, the pre-processor
// copies that NUL too.
static GKeyFile *load_config_data(const gchar *contents, gsize length, GError **error)
{
  GString *pre = mydumper_preprocess(contents, length);
  GKeyFile *kf = g_key_file_new();
  gboolean ok = g_key_file_load_from_data(kf, pre->str, pre->len, G_KEY_FILE_KEEP_COMMENTS, error);
  g_string_free(pre, TRUE);
  if (!ok)
  {
    g_key_file_free(kf);
    return NULL;
  }
  return kf;
}

// ---------------------------------------------------------------- JSON output

// Appends s[0..len) as a JSON string in the byte-string encoding: byte b is
// the code point U+00bb. Printable ASCII is written as is, except '"' and '\'
// which are backslash-escaped; every other byte is written as \u00XX.
static void json_bytes(GString *out, const gchar *s, gsize len)
{
  static const char hex[] = "0123456789abcdef";
  g_string_append_c(out, '"');
  for (gsize i = 0; i < len; i++)
  {
    guchar b = (guchar)s[i];
    if (b == '"' || b == '\\')
    {
      g_string_append_c(out, '\\');
      g_string_append_c(out, (gchar)b);
    }
    else if (b >= 0x20 && b <= 0x7e)
      g_string_append_c(out, (gchar)b);
    else
    {
      g_string_append(out, "\\u00");
      g_string_append_c(out, hex[b >> 4]);
      g_string_append_c(out, hex[b & 0x0f]);
    }
  }
  g_string_append_c(out, '"');
}

// A NUL-terminated string, or null.
static void json_string(GString *out, const gchar *s)
{
  if (s == NULL)
    g_string_append(out, "null");
  else
    json_bytes(out, s, strlen(s));
}

static gboolean write_all(const GString *out)
{
  return fwrite(out->str, 1, out->len, stdout) == out->len && fflush(stdout) == 0;
}

static gchar *glib_version(void)
{
  return g_strdup_printf("%u.%u.%u", glib_major_version, glib_minor_version, glib_micro_version);
}

// The JSON verdict for one file content, with its trailing newline.
static void verdict_json(GString *out, const gchar *contents, gsize length)
{
  GError *error = NULL;
  GKeyFile *kf = load_config_data(contents, length, &error);
  gchar *version = glib_version();
  g_string_append(out, "{\"glib\":");
  json_string(out, version);
  g_free(version);
  if (kf == NULL)
  {
    g_string_append(out, ",\"loadable\":false,\"error\":");
    json_string(out, error->message);
    g_string_append(out, ",\"groups\":[]}\n");
    g_error_free(error);
    return;
  }
  g_string_append(out, ",\"loadable\":true,\"error\":null,\"groups\":[");
  gsize ng = 0;
  gchar **groups = g_key_file_get_groups(kf, &ng);
  for (gsize i = 0; i < ng; i++)
  {
    gsize nk = 0;
    gchar **keys = g_key_file_get_keys(kf, groups[i], &nk, NULL);
    g_string_append(out, i ? ",{\"name\":" : "{\"name\":");
    json_string(out, groups[i]);
    g_string_append(out, ",\"entries\":[");
    for (gsize j = 0; j < nk; j++)
    {
      gchar *value = g_key_file_get_value(kf, groups[i], keys[j], NULL);
      g_string_append(out, j ? ",{\"key\":" : "{\"key\":");
      json_string(out, keys[j]);
      g_string_append(out, ",\"value\":");
      json_string(out, value);
      g_string_append_c(out, '}');
      g_free(value);
    }
    g_string_append(out, "]}");
    g_strfreev(keys);
  }
  g_string_append(out, "]}\n");
  g_strfreev(groups);
  g_key_file_free(kf);
}

// ------------------------------------------------------- file and serve modes

static int run_text(const gchar *path)
{
  gchar *contents = NULL;
  gsize length = 0;
  GError *error = NULL;
  if (!g_file_get_contents(path, &contents, &length, &error))
  {
    printf("IOERROR: %s\n", error->message);
    return 2;
  }
  GKeyFile *kf = load_config_data(contents, length, &error);
  g_free(contents);
  if (kf == NULL)
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
    {
      gchar *value = g_key_file_get_value(kf, groups[i], keys[j], NULL);
      printf("    <%s>=<%s>\n", keys[j], value);
      g_free(value);
    }
    g_strfreev(keys);
  }
  g_strfreev(groups);
  g_key_file_free(kf);
  return 0;
}

static int run_json(const gchar *path)
{
  gchar *contents = NULL;
  gsize length = 0;
  GError *error = NULL;
  if (!g_file_get_contents(path, &contents, &length, &error))
  {
    fprintf(stderr, "oracle: %s\n", error->message);
    g_error_free(error);
    return 2;
  }
  GString *out = g_string_new(NULL);
  verdict_json(out, contents, length);
  g_free(contents);
  gboolean ok = write_all(out);
  g_string_free(out, TRUE);
  return ok ? 0 : 2;
}

static int serve_fail(const gchar *what)
{
  fprintf(stderr, "oracle --serve: %s\n", what);
  return 2;
}

// Request: 4-byte big-endian length N, then N bytes of file content.
// Response: 4-byte big-endian length, then the --json verdict (newline included).
static int run_serve(void)
{
  for (;;)
  {
    guchar header[4];
    size_t got = fread(header, 1, sizeof header, stdin);
    if (got == 0 && !ferror(stdin))
      return 0;
    if (got != sizeof header)
      return serve_fail(ferror(stdin) ? "read error" : "truncated request length");
    guint32 n = (guint32)header[0] << 24 | (guint32)header[1] << 16 | (guint32)header[2] << 8 | header[3];
    // N + 1 bytes, NUL-terminated like the buffer of g_file_get_contents.
    gchar *buf = g_try_malloc((gsize)n + 1);
    if (buf == NULL)
      return serve_fail("request too large");
    if (n > 0 && fread(buf, 1, n, stdin) != n)
    {
      g_free(buf);
      return serve_fail(ferror(stdin) ? "read error" : "truncated request body");
    }
    buf[n] = '\0';
    GString *out = g_string_new(NULL);
    verdict_json(out, buf, n);
    g_free(buf);
    if (out->len > G_MAXUINT32)
    {
      g_string_free(out, TRUE);
      return serve_fail("response too large");
    }
    guchar reply[4] = {(guchar)(out->len >> 24), (guchar)(out->len >> 16), (guchar)(out->len >> 8),
                       (guchar)out->len};
    gboolean ok = fwrite(reply, 1, sizeof reply, stdout) == sizeof reply && write_all(out);
    g_string_free(out, TRUE);
    if (!ok)
      return serve_fail("write error");
  }
}

// ------------------------------------------------ historical GOption probes

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

// --------------------------------------------- data-driven GOption cases

typedef union
{
  gboolean b;
  gint i;
  gint64 i64;
  gdouble d;
  gchar *s;
} OptValue;

typedef struct
{
  gchar *long_name;
  gchar short_name; // 0: none
  GOptionArg arg;
  GOptionFlags flags;
  guint group;      // 0: main entries; g > 0: GOptCase.groups[g - 1]
  OptValue init;    // the variable before parsing
  OptValue var;     // the variable GOption writes (unused for callbacks)
  GPtrArray *calls; // callbacks only: every value received, NULL when none
} GOptOption;

typedef struct
{
  gchar *name;
  guint line;         // line of the "case" directive
  gint strict;        // -1 until declared
  GPtrArray *groups;  // group names, declaration order
  GPtrArray *options; // GOptOption *, declaration order
  GPtrArray *args;    // argv, argv[0] included
} GOptCase;

typedef struct
{
  const gchar *file;
  guint line;
  const gchar *p, *end; // unread part of the current line
} Lexer;

typedef struct
{
  GString *text;
  gboolean quoted;
} Token;

enum
{
  MAX_TOKENS = 6 // more than any directive takes, to detect extra tokens
};

static gboolean syntax_error(const Lexer *lx, const gchar *fmt, ...) G_GNUC_PRINTF(2, 3);

static gboolean syntax_error(const Lexer *lx, const gchar *fmt, ...)
{
  va_list ap;
  va_start(ap, fmt);
  gchar *msg = g_strdup_vprintf(fmt, ap);
  va_end(ap);
  fprintf(stderr, "oracle: %s:%u: %s\n", lx->file, lx->line, msg);
  g_free(msg);
  return FALSE;
}

static gboolean is_blank(gchar c)
{
  return c == ' ' || c == '\t';
}

// Bare tokens are runs of printable ASCII other than '"' and '\'.
static gboolean is_bare(guchar c)
{
  return c > 0x20 && c < 0x7f && c != '"' && c != '\\';
}

static gboolean read_quoted(Lexer *lx, GString *text)
{
  lx->p++; // opening quote
  for (;;)
  {
    if (lx->p == lx->end)
      return syntax_error(lx, "unterminated string");
    guchar c = (guchar)*lx->p++;
    if (c == '"')
      break;
    if (c < 0x20 || c == 0x7f)
      return syntax_error(lx, "raw control byte 0x%02x in a string: use an escape", c);
    if (c != '\\')
    {
      g_string_append_c(text, (gchar)c);
      continue;
    }
    if (lx->p == lx->end)
      return syntax_error(lx, "unterminated escape");
    c = (guchar)*lx->p++;
    switch (c)
    {
    case '\\':
    case '"':
      g_string_append_c(text, (gchar)c);
      break;
    case 't':
      g_string_append_c(text, '\t');
      break;
    case 'n':
      g_string_append_c(text, '\n');
      break;
    case 'r':
      g_string_append_c(text, '\r');
      break;
    case 'v':
      g_string_append_c(text, '\v');
      break;
    case 'f':
      g_string_append_c(text, '\f');
      break;
    case 'x':
      if (lx->end - lx->p < 2 || !g_ascii_isxdigit(lx->p[0]) || !g_ascii_isxdigit(lx->p[1]))
        return syntax_error(lx, "\\x needs two hexadecimal digits");
      g_string_append_c(text, (gchar)(g_ascii_xdigit_value(lx->p[0]) << 4 | g_ascii_xdigit_value(lx->p[1])));
      lx->p += 2;
      break;
    default:
      return syntax_error(lx, "unknown escape: backslash followed by byte 0x%02x", c);
    }
  }
  if (lx->p < lx->end && !is_blank(*lx->p))
    return syntax_error(lx, "text after the closing quote");
  if (memchr(text->str, '\0', text->len) != NULL)
    return syntax_error(lx, "NUL byte in a string: GLib strings end at NUL");
  return TRUE;
}

// Reads the next token of the line. Returns 1, 0 at the end of the line, or
// -1 after reporting a syntax error.
static int next_token(Lexer *lx, Token *tok)
{
  g_string_truncate(tok->text, 0);
  tok->quoted = FALSE;
  while (lx->p < lx->end && is_blank(*lx->p))
    lx->p++;
  if (lx->p == lx->end)
    return 0;
  if (*lx->p == '"')
  {
    tok->quoted = TRUE;
    return read_quoted(lx, tok->text) ? 1 : -1;
  }
  for (; lx->p < lx->end && !is_blank(*lx->p); lx->p++)
  {
    guchar c = (guchar)*lx->p;
    if (!is_bare(c))
    {
      syntax_error(lx, "byte 0x%02x is not allowed in a bare token: use a quoted string", c);
      return -1;
    }
    g_string_append_c(tok->text, (gchar)c);
  }
  return 1;
}

static gboolean is_word(const Token *t, const gchar *word)
{
  return !t->quoted && strcmp(t->text->str, word) == 0;
}

static GOptOption *find_long(const GOptCase *c, const gchar *name)
{
  for (guint k = 0; k < c->options->len; k++)
  {
    GOptOption *o = c->options->pdata[k];
    if (strcmp(o->long_name, name) == 0)
      return o;
  }
  return NULL;
}

static GOptOption *find_short(const GOptCase *c, gchar name)
{
  for (guint k = 0; k < c->options->len; k++)
  {
    GOptOption *o = c->options->pdata[k];
    if (o->short_name != 0 && o->short_name == name)
      return o;
  }
  return NULL;
}

static const struct
{
  const gchar *name;
  GOptionArg arg;
} option_types[] = {
    {"none", G_OPTION_ARG_NONE},   {"string", G_OPTION_ARG_STRING}, {"filename", G_OPTION_ARG_FILENAME},
    {"int", G_OPTION_ARG_INT},     {"int64", G_OPTION_ARG_INT64},   {"double", G_OPTION_ARG_DOUBLE},
    {"callback", G_OPTION_ARG_CALLBACK},
};

static const struct
{
  const gchar *name;
  GOptionFlags flag;
} option_flags[] = {
    {"optional_arg", G_OPTION_FLAG_OPTIONAL_ARG},
    {"reverse", G_OPTION_FLAG_REVERSE},
    {"no_arg", G_OPTION_FLAG_NO_ARG},
    {"hidden", G_OPTION_FLAG_HIDDEN},
};

static gboolean parse_flags(const Lexer *lx, const Token *t, GOptionFlags *flags)
{
  if (is_word(t, "-"))
    return TRUE;
  if (t->quoted)
    return syntax_error(lx, "flags are a bare token");
  gchar **names = g_strsplit(t->text->str, ",", -1);
  gboolean ok = TRUE;
  for (gchar **n = names; ok && *n != NULL; n++)
  {
    ok = FALSE;
    for (gsize i = 0; i < G_N_ELEMENTS(option_flags); i++)
      if (strcmp(*n, option_flags[i].name) == 0)
      {
        *flags |= option_flags[i].flag;
        ok = TRUE;
      }
  }
  g_strfreev(names);
  return ok || syntax_error(lx, "unknown flag (optional_arg, reverse, no_arg, hidden)");
}

static gboolean parse_init(const Lexer *lx, const Token *v, GOptOption *o)
{
  const gchar *text = v->text->str;
  gint64 n = 0;
  gchar *end = NULL;
  switch (o->arg)
  {
  case G_OPTION_ARG_NONE:
    if (!is_word(v, "true") && !is_word(v, "false"))
      return syntax_error(lx, "initial value of a none option: true or false");
    o->init.b = is_word(v, "true");
    return TRUE;
  case G_OPTION_ARG_INT:
    if (v->quoted || !g_ascii_string_to_signed(text, 10, G_MININT, G_MAXINT, &n, NULL))
      return syntax_error(lx, "initial value of an int option: a decimal integer");
    o->init.i = (gint)n;
    return TRUE;
  case G_OPTION_ARG_INT64:
    if (v->quoted || !g_ascii_string_to_signed(text, 10, G_MININT64, G_MAXINT64, &n, NULL))
      return syntax_error(lx, "initial value of an int64 option: a decimal integer");
    o->init.i64 = n;
    return TRUE;
  case G_OPTION_ARG_DOUBLE:
    o->init.d = g_ascii_strtod(text, &end);
    if (v->quoted || v->text->len == 0 || *end != '\0')
      return syntax_error(lx, "initial value of a double option: a number");
    return TRUE;
  case G_OPTION_ARG_STRING:
  case G_OPTION_ARG_FILENAME:
    o->init.s = is_word(v, "null") ? NULL : g_strdup(text);
    return TRUE;
  case G_OPTION_ARG_CALLBACK:
  default:
    return syntax_error(lx, "callbacks take no initial value");
  }
}

// option LONG SHORT TYPE FLAGS [INIT]
static gboolean parse_option(const Lexer *lx, GOptCase *c, const Token *t, guint nt)
{
  if (nt != 4 && nt != 5)
    return syntax_error(lx, "usage: option LONG SHORT TYPE FLAGS [INIT]");
  if (t[0].text->len == 0)
    return syntax_error(lx, "empty long name");
  if (find_long(c, t[0].text->str) != NULL)
    return syntax_error(lx, "duplicate long name \"%s\"", t[0].text->str);
  GOptOption *o = g_new0(GOptOption, 1);
  o->long_name = g_strdup(t[0].text->str);
  o->group = c->groups->len;
  g_ptr_array_add(c->options, o);

  if (!is_word(&t[1], "-"))
  {
    gchar s = t[1].text->str[0];
    if (t[1].text->len != 1 || s == '-' || !g_ascii_isprint(s))
      return syntax_error(lx, "short name: one printable ASCII character other than '-', or - for none");
    if (find_short(c, s) != NULL)
      return syntax_error(lx, "duplicate short name '%c'", s);
    o->short_name = s;
  }

  gboolean known = FALSE;
  for (gsize i = 0; i < G_N_ELEMENTS(option_types); i++)
    if (is_word(&t[2], option_types[i].name))
    {
      o->arg = option_types[i].arg;
      known = TRUE;
    }
  if (!known)
    return syntax_error(lx, "unknown type (none, string, filename, int, int64, double, callback)");

  if (!parse_flags(lx, &t[3], &o->flags))
    return FALSE;
  // GLib drops these combinations with a warning; refuse them instead.
  if ((o->flags & G_OPTION_FLAG_REVERSE) && o->arg != G_OPTION_ARG_NONE)
    return syntax_error(lx, "reverse only applies to type none");
  if ((o->flags & (G_OPTION_FLAG_OPTIONAL_ARG | G_OPTION_FLAG_NO_ARG)) && o->arg != G_OPTION_ARG_CALLBACK)
    return syntax_error(lx, "optional_arg and no_arg only apply to type callback");

  if (o->arg == G_OPTION_ARG_CALLBACK)
    o->calls = g_ptr_array_new_with_free_func(g_free);
  return nt == 4 || parse_init(lx, &t[4], o);
}

static gboolean finish_case(const Lexer *lx, const GOptCase *c)
{
  if (c == NULL)
    return TRUE;
  if (c->strict < 0)
    return syntax_error(lx, "case \"%s\" (line %u) does not declare strict", c->name, c->line);
  if (c->args->len == 0)
    return syntax_error(lx, "case \"%s\" (line %u) has no arg: argv[0] is required", c->name, c->line);
  return TRUE;
}

static GOptCase *new_case(const gchar *name, guint line)
{
  GOptCase *c = g_new0(GOptCase, 1);
  c->name = g_strdup(name);
  c->line = line;
  c->strict = -1;
  c->groups = g_ptr_array_new_with_free_func(g_free);
  c->options = g_ptr_array_new();
  c->args = g_ptr_array_new_with_free_func(g_free);
  return c;
}

static gboolean has_group(const GOptCase *c, const gchar *name)
{
  for (guint g = 0; g < c->groups->len; g++)
    if (strcmp(c->groups->pdata[g], name) == 0)
      return TRUE;
  return FALSE;
}

// Runs one directive. *current is the case being declared.
static gboolean parse_directive(const Lexer *lx, const Token *kw, const Token *t, guint nt, GPtrArray *cases,
                                GHashTable *names, GOptCase **current)
{
  GOptCase *c = *current;
  if (kw->quoted)
    return syntax_error(lx, "a directive must be a bare word");
  const gchar *k = kw->text->str;
  if (strcmp(k, "case") == 0)
  {
    if (nt != 1 || t[0].text->len == 0)
      return syntax_error(lx, "usage: case NAME");
    if (!finish_case(lx, c))
      return FALSE;
    if (g_hash_table_contains(names, t[0].text->str))
      return syntax_error(lx, "duplicate case name \"%s\"", t[0].text->str);
    *current = new_case(t[0].text->str, lx->line);
    g_hash_table_add(names, (*current)->name);
    g_ptr_array_add(cases, *current);
    return TRUE;
  }
  if (c == NULL)
    return syntax_error(lx, "\"%s\" before the first case", k);
  if (strcmp(k, "strict") == 0)
  {
    if (nt != 1 || (!is_word(&t[0], "true") && !is_word(&t[0], "false")))
      return syntax_error(lx, "usage: strict true|false");
    if (c->strict >= 0)
      return syntax_error(lx, "strict declared twice");
    c->strict = is_word(&t[0], "true");
    return TRUE;
  }
  if (strcmp(k, "group") == 0)
  {
    if (nt != 1 || t[0].text->len == 0)
      return syntax_error(lx, "usage: group NAME");
    if (has_group(c, t[0].text->str))
      return syntax_error(lx, "duplicate group \"%s\"", t[0].text->str);
    g_ptr_array_add(c->groups, g_strdup(t[0].text->str));
    return TRUE;
  }
  if (strcmp(k, "option") == 0)
    return parse_option(lx, c, t, nt);
  if (strcmp(k, "arg") == 0)
  {
    if (nt != 1)
      return syntax_error(lx, "usage: arg VALUE (quote empty strings and spaces)");
    g_ptr_array_add(c->args, g_strdup(t[0].text->str));
    return TRUE;
  }
  return syntax_error(lx, "unknown directive \"%s\"", k);
}

// Parses one case file, appending its cases. names holds every case name
// seen so far, across files.
static gboolean parse_cases(const gchar *file, const gchar *data, gsize len, GPtrArray *cases, GHashTable *names)
{
  Lexer lx = {file, 0, NULL, NULL};
  Token kw = {g_string_new(NULL), FALSE};
  Token t[MAX_TOKENS];
  for (guint i = 0; i < MAX_TOKENS; i++)
    t[i] = (Token){g_string_new(NULL), FALSE};
  GOptCase *c = NULL;
  gboolean ok = TRUE;
  const gchar *line = data, *data_end = data + len;
  while (ok && line < data_end)
  {
    const gchar *nl = memchr(line, '\n', (gsize)(data_end - line));
    lx.line++;
    lx.p = line;
    lx.end = nl ? nl : data_end;
    line = nl ? nl + 1 : data_end;
    if (lx.end > lx.p && lx.end[-1] == '\r')
    {
      ok = syntax_error(&lx, "carriage return at the end of the line: case files use LF line endings");
      break;
    }
    const gchar *q = lx.p;
    while (q < lx.end && is_blank(*q))
      q++;
    if (q == lx.end || *q == '#')
      continue; // blank line or comment
    int r = next_token(&lx, &kw);
    guint nt = 0;
    while (r > 0 && nt < MAX_TOKENS && (r = next_token(&lx, &t[nt])) > 0)
      nt++;
    if (r < 0)
      ok = FALSE;
    else if (nt == MAX_TOKENS)
      ok = syntax_error(&lx, "too many tokens");
    else
      ok = parse_directive(&lx, &kw, t, nt, cases, names, &c);
  }
  if (ok)
  {
    lx.line++;
    ok = finish_case(&lx, c);
  }
  g_string_free(kw.text, TRUE);
  for (guint i = 0; i < MAX_TOKENS; i++)
    g_string_free(t[i].text, TRUE);
  return ok;
}

static GOptCase *running_case;

// The GOptionArgFunc of every callback option. GLib passes the option as
// "--long" or "-c" (documented), which identifies it: names are unique.
static gboolean record_callback(const gchar *option_name, const gchar *value, gpointer data, GError **error)
{
  (void)data;
  (void)error;
  GOptOption *o = NULL;
  if (option_name[0] == '-' && option_name[1] == '-')
    o = find_long(running_case, option_name + 2);
  else if (option_name[0] == '-' && option_name[1] != '\0' && option_name[2] == '\0')
    o = find_short(running_case, option_name[1]);
  if (o == NULL || o->calls == NULL)
  {
    fprintf(stderr, "oracle: callback for an unexpected option %s\n", option_name);
    exit(2);
  }
  g_ptr_array_add(o->calls, g_strdup(value));
  return TRUE;
}

static void add_entries(GOptionContext *ctx, GOptCase *c, guint g)
{
  GArray *entries = g_array_new(TRUE, TRUE, sizeof(GOptionEntry)); // zero-terminated
  for (guint k = 0; k < c->options->len; k++)
  {
    GOptOption *o = c->options->pdata[k];
    if (o->group != g)
      continue;
    o->var = o->init;
    GOptionEntry e = {o->long_name, o->short_name, (gint)o->flags, o->arg, &o->var, "", NULL};
    if (o->arg == G_OPTION_ARG_CALLBACK)
      e.arg_data = (gpointer)record_callback;
    g_array_append_val(entries, e);
  }
  GOptionEntry *list = (GOptionEntry *)(void *)entries->data;
  if (g == 0)
    g_option_context_add_main_entries(ctx, list, NULL);
  else
  {
    const gchar *name = c->groups->pdata[g - 1];
    GOptionGroup *group = g_option_group_new(name, name, name, NULL, NULL);
    g_option_group_add_entries(group, list);
    g_option_context_add_group(ctx, group);
  }
  g_array_free(entries, TRUE); // GLib copied the entries
}

static void append_value(GString *out, GOptOption *o)
{
  gchar number[G_ASCII_DTOSTR_BUF_SIZE];
  switch (o->arg)
  {
  case G_OPTION_ARG_NONE:
    g_string_append(out, o->var.b ? "true" : "false");
    break;
  case G_OPTION_ARG_INT:
    g_snprintf(number, sizeof number, "%d", o->var.i);
    json_string(out, number);
    break;
  case G_OPTION_ARG_INT64:
    g_snprintf(number, sizeof number, "%" G_GINT64_FORMAT, o->var.i64);
    json_string(out, number);
    break;
  case G_OPTION_ARG_DOUBLE:
    json_string(out, g_ascii_dtostr(number, sizeof number, o->var.d));
    break;
  case G_OPTION_ARG_STRING:
  case G_OPTION_ARG_FILENAME:
    json_string(out, o->var.s);
    if (o->var.s != o->init.s)
      g_free(o->var.s); // allocated by GOption
    break;
  default:
    g_assert_not_reached();
  }
}

static void run_goption_case(GOptCase *c, GString *out)
{
  GOptionContext *ctx = g_option_context_new("");
  // Like mydumper, which defines its own --help. Enabled, GLib's help would
  // print and exit(0) on --help, -h or -?.
  g_option_context_set_help_enabled(ctx, FALSE);
  if (!c->strict)
    g_option_context_set_ignore_unknown_options(ctx, TRUE);
  for (guint g = 0; g <= c->groups->len; g++)
    add_entries(ctx, c, g);

  gchar **argv = g_new0(gchar *, c->args->len + 1);
  for (guint i = 0; i < c->args->len; i++)
    argv[i] = g_strdup(c->args->pdata[i]);
  GError *error = NULL;
  running_case = c;
  gboolean ok = g_option_context_parse_strv(ctx, &argv, &error);
  running_case = NULL;
  g_option_context_free(ctx);

  g_string_append(out, "{\"name\":");
  json_string(out, c->name);
  g_string_append(out, ok ? ",\"ok\":true,\"error\":" : ",\"ok\":false,\"error\":");
  json_string(out, error ? error->message : NULL);
  g_string_append(out, ",\"values\":{");
  gboolean first = TRUE;
  for (guint k = 0; k < c->options->len; k++)
  {
    GOptOption *o = c->options->pdata[k];
    if (o->arg == G_OPTION_ARG_CALLBACK)
      continue;
    g_string_append(out, first ? "" : ",");
    first = FALSE;
    json_string(out, o->long_name);
    g_string_append_c(out, ':');
    append_value(out, o);
  }
  g_string_append(out, "},\"callbacks\":{");
  first = TRUE;
  for (guint k = 0; k < c->options->len; k++)
  {
    GOptOption *o = c->options->pdata[k];
    if (o->arg != G_OPTION_ARG_CALLBACK)
      continue;
    g_string_append(out, first ? "" : ",");
    first = FALSE;
    json_string(out, o->long_name);
    g_string_append(out, ":[");
    for (guint i = 0; i < o->calls->len; i++)
    {
      g_string_append(out, i ? "," : "");
      json_string(out, o->calls->pdata[i]);
    }
    g_string_append_c(out, ']');
  }
  g_string_append(out, "},\"leftover\":[");
  for (guint i = 0; argv[i] != NULL; i++)
  {
    g_string_append(out, i ? "," : "");
    json_string(out, argv[i]);
  }
  g_string_append(out, "]}\n");
  g_strfreev(argv);
  g_clear_error(&error);
}

static gboolean read_stdin(gchar **data, gsize *len)
{
  GString *buf = g_string_new(NULL);
  gchar chunk[65536];
  size_t n;
  while ((n = fread(chunk, 1, sizeof chunk, stdin)) > 0)
    g_string_append_len(buf, chunk, (gssize)n);
  gboolean ok = !ferror(stdin);
  *len = buf->len;
  *data = g_string_free(buf, FALSE);
  return ok;
}

static int run_goption_cases(int nfiles, char **files)
{
  // mydumper calls setlocale(LC_ALL, "") before parsing: GOption converts
  // string and callback values from the locale's charset.
  setlocale(LC_ALL, "");
  GPtrArray *cases = g_ptr_array_new();
  GHashTable *names = g_hash_table_new(g_str_hash, g_str_equal);
  for (int f = 0; f < nfiles; f++)
  {
    gchar *data = NULL;
    gsize len = 0;
    GError *error = NULL;
    if (strcmp(files[f], "-") == 0)
    {
      if (!read_stdin(&data, &len))
      {
        fprintf(stderr, "oracle: error reading stdin\n");
        return 2;
      }
    }
    else if (!g_file_get_contents(files[f], &data, &len, &error))
    {
      fprintf(stderr, "oracle: %s\n", error->message);
      return 2;
    }
    gboolean ok = parse_cases(strcmp(files[f], "-") == 0 ? "<stdin>" : files[f], data, len, cases, names);
    g_free(data);
    if (!ok)
      return 2;
  }
  // Every file parses before any case runs: the output is all or nothing.
  GString *out = g_string_new(NULL);
  for (guint i = 0; i < cases->len; i++)
  {
    g_string_truncate(out, 0);
    run_goption_case(cases->pdata[i], out);
    if (!write_all(out))
      return 2;
  }
  return 0;
}

// ------------------------------------------------------------------------ main

static const char usage_text[] =
    "usage: oracle [ENV...] FILE                    text verdict\n"
    "       oracle [ENV...] --json FILE             JSON verdict\n"
    "       oracle [ENV...] --serve                 length-prefixed requests on stdin\n"
    "       oracle [ENV...] --goption               historical GOption probes\n"
    "       oracle [ENV...] --goption-cases FILE... data-driven GOption cases (- reads stdin)\n"
    "       oracle --glib-version                   runtime GLib version\n"
    "ENV:   --setenv NAME=VALUE | --unsetenv NAME   applied in order, before anything runs\n"
    "See tools/oracle/README.md.\n";

static int usage(FILE *to, int status)
{
  fputs(usage_text, to);
  return status;
}

static gboolean is_flag(const char *arg)
{
  static const char *const flags[] = {"--json",         "--serve", "--goption", "--goption-cases",
                                      "--glib-version", "--help",  "-h",        "--setenv",
                                      "--unsetenv",     "--"};
  for (gsize i = 0; i < G_N_ELEMENTS(flags); i++)
    if (strcmp(arg, flags[i]) == 0)
      return TRUE;
  return FALSE;
}

// --setenv NAME=VALUE
static gboolean set_env(const char *arg)
{
  const char *eq = strchr(arg, '=');
  if (eq == NULL || eq == arg)
  {
    fprintf(stderr, "oracle: --setenv expects NAME=VALUE, got \"%s\"\n", arg);
    return FALSE;
  }
  gchar *name = g_strndup(arg, (gsize)(eq - arg));
  gboolean ok = g_setenv(name, eq + 1, TRUE);
  g_free(name);
  if (!ok)
    fprintf(stderr, "oracle: cannot set \"%s\"\n", arg);
  return ok;
}

int main(int argc, char **argv)
{
  int i = 1;
  // The environment comes first: GLib reads LANGUAGE, LC_ALL, LC_MESSAGES and
  // LANG lazily, when the first key file is loaded.
  for (; i < argc && (strcmp(argv[i], "--setenv") == 0 || strcmp(argv[i], "--unsetenv") == 0); i += 2)
  {
    if (i + 1 >= argc)
      return usage(stderr, 2);
    if (strcmp(argv[i], "--unsetenv") == 0)
      g_unsetenv(argv[i + 1]);
    else if (!set_env(argv[i + 1]))
      return 2;
  }
  int rest = argc - i;
  char **args = argv + i;
  if (rest == 1 && strcmp(args[0], "--goption") == 0)
    return probe_goption();
  if (rest == 1 && strcmp(args[0], "--serve") == 0)
    return run_serve();
  if (rest == 1 && strcmp(args[0], "--glib-version") == 0)
  {
    gchar *version = glib_version();
    printf("%s\n", version);
    g_free(version);
    return 0;
  }
  if (rest == 1 && (strcmp(args[0], "--help") == 0 || strcmp(args[0], "-h") == 0))
    return usage(stdout, 0);
  if (rest == 2 && strcmp(args[0], "--json") == 0)
    return run_json(args[1]);
  if (rest >= 2 && strcmp(args[0], "--goption-cases") == 0)
    return run_goption_cases(rest - 1, args + 1);
  if (rest == 2 && strcmp(args[0], "--") == 0)
    return run_text(args[1]);
  if (rest == 1 && !is_flag(args[0]))
    return run_text(args[0]);
  return usage(stderr, 2);
}
