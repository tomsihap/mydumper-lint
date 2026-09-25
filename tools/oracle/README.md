# GLib oracle

`oracle` is the test oracle of mydumper-lint. It is a small C program linked against GLib
that runs a config file through the same code path as mydumper, then prints what GLib
sees:

- **key files:** mydumper's `load_config_file()` pre-processor (it appends `= 1` to
  valueless lines), then `g_key_file_load_from_data()`;
- **options:** `g_option_context_parse_strv()` on an argv built the way mydumper builds it
  from a config group (`[group, --key, value, …]`).

The Go emulators are tested against it: oracle conformance and differential fuzzing
(design document, §11.2 and §11.4).

## License

The oracle is licensed under **GPL-3.0-or-later** ([LICENSE](LICENSE)): `oracle.c`
contains a behavioral copy of `load_config_file()` from
[mydumper](https://github.com/mydumper/mydumper) (GPL-3.0).

It is a **test-only tool**. It is never compiled into, linked into or shipped with the
Apache-licensed mydumper-lint binary. The Go test suite only runs it as a separate
process. The Docker images carry the label `org.opencontainers.image.licenses=GPL-3.0-or-later`.

## Build and run

```sh
make oracle             # bin/oracle, needs cc, pkg-config and the GLib headers
make oracle-images      # the three Docker images below
make oracle-conformance # every image against the expected files
```

`make oracle` runs `cc -O2 -Wall -Wextra -o bin/oracle tools/oracle/oracle.c $(pkg-config --cflags --libs glib-2.0)`.
The source only uses the GLib 2.68 API (`GLIB_VERSION_MAX_ALLOWED`), the oldest version
the images cover.

## Modes

| Invocation | Output | Exit status |
|---|---|---|
| `oracle FILE` | text verdict: `OK groups=N` then groups and `<key>=<value>` lines, or `ERROR: <GLib message>` | 0 OK, 1 ERROR, 2 I/O error (`IOERROR: …`) |
| `oracle --json FILE` | the same verdict as one JSON line | 0 whether the file loads or not, 2 on an I/O error |
| `oracle --serve` | JSON verdicts for length-prefixed requests on stdin | 0 at end of input, 2 on a truncated request |
| `oracle --goption` | the historical GOption probe (`testdata/goption-expected.txt`) | 0 |
| `oracle --goption-cases FILE...` | one JSON line per GOption case (`-` reads stdin) | 0 after all cases, 2 on a syntax or I/O error |
| `oracle --glib-version` | the runtime GLib version, e.g. `2.68.4` | 0 |
| `oracle --help` | usage | 0 |

`oracle -- FILE` forces the text mode for a file name that looks like an option.

**`--plain`.** Placed after the environment options and before `FILE`, `--json` or
`--serve`, it loads files without mydumper's pre-processor, the way mydumper v0.19.1-x
does (`g_key_file_load_from_file` on the file as written). The pre-processor appeared in
v0.19.3-1; the knowledge base records it per version (`preprocessor`).

**Environment options.** `--setenv NAME=VALUE` and `--unsetenv NAME` come before the mode
and are applied in order before anything else runs. They exist because the result
depends on the environment and a `docker run IMAGE ARGS` command cannot set it otherwise:

- GKeyFile builds its language list from `LANGUAGE`, `LC_ALL`, `LC_MESSAGES` and `LANG`.
  A localized key `key[locale]` is only visible when its locale is in that list (fact K14).
  The locale does not need to be installed.
- `--goption-cases` calls `setlocale(LC_ALL, "")` first, like mydumper: GOption converts
  string and callback values from the locale's charset, so `LC_CTYPE` and friends matter
  there too. In the C locale, a non-ASCII value is fatal.

The key-file modes never call `setlocale()`: GLib's messages stay untranslated, and
GKeyFile does not depend on the C library locale.

## Byte-string encoding

Every JSON string the oracle writes (group names, keys, values, error messages, argv
elements, case names) encodes **byte b as the code point U+00bb**:

- bytes 0x20–0x7E are written as is, except `"` → `\"` and `\` → `\\`;
- every other byte (controls, NUL, DEL, each byte of a UTF-8 sequence) is written as
  `\u00XX`, with lowercase hexadecimal.

The encoding is lossless for any byte string, and the output is pure ASCII. `é` (C3 A9) is
written `"Ã©"`; GLib's `“` is `"â\u0080\u009c"`. To decode, parse the JSON,
then map each rune (always ≤ U+00FF) back to one byte:

```go
func decodeBytes(s string) []byte {
	b := make([]byte, 0, len(s))
	for _, r := range s {
		b = append(b, byte(r))
	}
	return b
}
```

## JSON verdict (`--json`, `--serve`)

One compact line, keys in this order, ending with `\n`:

```json
{"glib":"2.88.3","loadable":true,"error":null,"groups":[{"name":"mydumper","entries":[{"key":"routines","value":"1"}]}]}
```

- `glib`: the runtime GLib version (`glib_major_version` etc.).
- `loadable`, `error`: when GLib rejects the file, `loadable` is `false`, `error` is its
  message and `groups` is `[]`.
- `groups`: in `g_key_file_get_groups()` order. `entries`: in `g_key_file_get_keys()`
  order, with the value of `g_key_file_get_value()`, exactly what the text mode prints.
  A duplicate key appears twice, both times with the last value (K13).

## `--serve` wire format

The oracle reads requests from stdin until end of input:

```text
request:  N (4 bytes, big-endian, unsigned)  then N bytes of file content
response: M (4 bytes, big-endian, unsigned)  then M bytes: the --json verdict, "\n" included
```

Responses come in request order and are flushed one by one. `N = 0` is an empty file. End
of input between two requests exits 0. A truncated request exits 2 with a message on
stderr. The environment is set once per process, with `--setenv` at startup.

Fidelity detail: mydumper reads the file with `g_file_get_contents()`, which appends a NUL,
and its pre-processor copies that NUL when a line that starts with `[` runs to the end of
the input. The oracle copies each request into an N+1-byte buffer ending in NUL, so the
same happens: `[x` without a final newline is rejected as `“[x�”`.

## GOption cases (`--goption-cases`)

A line-based format; each non-blank line is one directive. Lines whose first non-blank
character is `#` are comments. Lines end with LF; a CR is an error.

**Tokens** are separated by spaces or tabs. A bare token is printable ASCII other than `"`
and `\`. Anything else goes in a quoted string: `"…"`, with the escapes `\\`, `\"`, `\t`,
`\n`, `\r`, `\v`, `\f` and `\xHH` (two hexadecimal digits). Raw bytes ≥ 0x80 are allowed in
a quoted string; raw control bytes are not (use an escape). `\x00` is refused: GLib
strings end at NUL. Quote the empty string (`""`) and anything with spaces.

| Directive | Meaning |
|---|---|
| `case NAME` | Starts a case. Names are unique across all the files of one run. |
| `strict true` \| `strict false` | Required. `true`: `g_option_context_set_ignore_unknown_options()` is not called, unknown options are fatal (mydumper v0.19.3-3). `false`: it is called with `TRUE` (v1.0.8-1, master). |
| `group NAME` | Optional. The options declared after it go into a `GOptionGroup` named `NAME`, added with `g_option_context_add_group()`. Options declared before the first `group` are main entries. mydumper uses groups, and GOption accepts `--<prefix of the group name>-<option>` for their options. |
| `option LONG SHORT TYPE FLAGS [INIT]` | Declares an entry. `LONG`: the long name, unique. `SHORT`: one printable ASCII character other than `-`, unique, or `-` for none. `TYPE`: `none`, `string`, `filename`, `int`, `int64`, `double` or `callback`. `FLAGS`: `-`, or a comma-separated list of `optional_arg`, `reverse`, `no_arg`, `hidden` (`reverse` needs `none`; `optional_arg` and `no_arg` need `callback`). `INIT`: the variable before parsing: `true`/`false` for `none` (default `false`), a decimal integer for `int`/`int64` (default 0), a number for `double` (default 0), a token or the bare word `null` for `string`/`filename` (default `null`). Callbacks take no `INIT`. |
| `arg VALUE` | Appends one argv element. At least one is required: argv[0], which GOption skips (mydumper passes the group name). |

Each case runs in a fresh `GOptionContext` with GLib's built-in help disabled, like
mydumper (which defines its own `--help`; GLib's would print and exit).

```text
case example
strict false
option threads t int -
option compress - callback optional_arg
arg mydumper
arg --compress
arg -x
arg --threads
arg 4
```

prints:

```json
{"name":"example","ok":true,"error":null,"values":{"threads":"4"},"callbacks":{"compress":[null]},"leftover":["mydumper","-x"]}
```

- `ok`, `error`: the result of `g_option_context_parse_strv()` and its message.
- `values`: every non-callback option, in declaration order, after parsing. `none`:
  `true` or `false`. `int`, `int64`: decimal string. `double`: `g_ascii_dtostr()` string.
  `string`, `filename`: string or `null`. When parsing fails, GOption restores the initial
  values.
- `callbacks`: every callback option, with the values it received in call order (`null`
  when called without a value). Calls are not undone when parsing fails.
- `leftover`: argv after parsing, argv[0] included. Unchanged when parsing fails.

Every file is parsed before any case runs. On an error, nothing is printed; stderr says
`oracle: FILE:LINE: message` and the exit status is 2.

## Runner and expected files

`tools/oracle/run-cases.sh MODE ORACLE_CMD...` runs the cases through any oracle command.
Run it from the repository root: case paths are passed relative to it, so a container must
mount the repository as its working directory.

```sh
tools/oracle/run-cases.sh check bin/oracle
tools/oracle/run-cases.sh check docker run --rm -i -v "$PWD:/w:ro" -w /w mydumper-lint-oracle:alma9
```

| Mode | Runs | Prints the format of |
|---|---|---|
| `text` | `testdata/oracle-cases/*.cnf` | `testdata/oracle-cases/expected.txt`: `### NAME`, then the text verdict |
| `json` | `testdata/oracle-cases/*.cnf` with `--json` | `testdata/oracle-cases/expected.jsonl` |
| `goption` | `testdata/goption-cases/*.cases` | `testdata/goption-cases/expected.jsonl` |
| `check` | the three above, plus `--goption` against `testdata/goption-expected.txt` | prints the GLib version and `ok` or a diff per check; exits 1 on any difference |

Cases run in file-name order. Before each case, the runner passes `--unsetenv` for `LANG`,
`LANGUAGE`, `LC_ALL`, `LC_MESSAGES`, `LC_CTYPE` and `CHARSET`, then `--setenv` for each
`NAME=value` line of the case's sidecar file `NN_name.env`, if there is one (blank lines
and `#` comments are ignored). Case 55 uses one to run with `LANG=fr_FR.UTF-8`.

`expected.jsonl` has no `glib` field: the `json` mode drops it and adds `"case":"NAME"` as
the first key. The expected files are therefore the same for every GLib version; `check`
prints the version separately.

To add a case, write the file with `printf` or a script (never an editor: every byte
counts, and `.gitattributes` marks `testdata/**` as `-text`), regenerate, review the diff,
then run the conformance on every image:

```sh
make oracle
tools/oracle/run-cases.sh text bin/oracle > testdata/oracle-cases/expected.txt
tools/oracle/run-cases.sh json bin/oracle > testdata/oracle-cases/expected.jsonl
tools/oracle/run-cases.sh goption bin/oracle > testdata/goption-cases/expected.jsonl
make oracle-conformance
```

## Docker images

| Image | Base | GLib | Why |
|---|---|---|---|
| `mydumper-lint-oracle:alma9` | `almalinux:9` | 2.68.4 | base of the official mydumper images |
| `mydumper-lint-oracle:noble` | `ubuntu:24.04` | 2.80.0 | distribution packages |
| `mydumper-lint-oracle:edge` | `fedora:rawhide` | newest (2.90.0 in September 2026) | glibc like the other two, so only GLib varies |

The Dockerfiles live in [`docker/`](docker/); the build context is `tools/oracle`, reduced
to `oracle.c` by `.dockerignore`. Each base image is pinned as `image:tag@sha256:…`, with
the tag also named in a comment, so that Dependabot's `docker` ecosystem can bump the
digests. A build step fails if the runtime GLib differs from the version the oracle was
compiled against. The oracle is the entrypoint and runs as user 65534. Pass `-i` to
`docker run` for `--serve` and for `--goption-cases -`.
