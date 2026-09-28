# mydumper-lint — Design

> **Status:** draft for review · 2026-09-24 · owner: @tomsihap
>
> This document is the single source of truth for mydumper-lint. It supersedes the internal
> draft specification (written in French, kept out of the repository) and records every
> amendment made after checking that draft against mydumper's source code, GLib, and the
> official mydumper Docker images. Appendix C lists the amendments.

## Contents

1. [Summary](#1-summary)
2. [Goals, non-goals, decisions](#2-goals-non-goals-decisions)
3. [Reference behavior](#3-reference-behavior)
4. [Architecture](#4-architecture)
5. [Knowledge base (optionsdb)](#5-knowledge-base-optionsdb)
6. [Rules](#6-rules)
7. [Fixer](#7-fixer)
8. [CLI](#8-cli)
9. [Configuration](#9-configuration)
10. [Output formats](#10-output-formats)
11. [Testing strategy](#11-testing-strategy)
12. [Project infrastructure](#12-project-infrastructure)
13. [Versioning and compatibility](#13-versioning-and-compatibility)
14. [Milestones](#14-milestones)
15. [Open questions](#15-open-questions)
- [Appendix A — Design probes (oracle cases 50–69)](#appendix-a--design-probes-oracle-cases-5069)
- [Appendix B — GOption probes](#appendix-b--goption-probes)
- [Appendix C — Changes from the draft spec](#appendix-c--changes-from-the-draft-spec)

---

## 1. Summary

mydumper-lint is a linter and fixer, written in Go, for the configuration files that
[mydumper and myloader](https://github.com/mydumper/mydumper) read through `--defaults-file`
and `--defaults-extra-file`.

**Why it exists: mydumper fails open.** When GLib cannot parse the file, mydumper logs a
single `WARNING: Failed to load config file …`, ignores the entire file, and keeps running
with defaults (a successful dump still exits 0). mydumper still hands the same file to the
MySQL client library, so `[client]` credentials typically keep working and the run looks
healthy (F12). If that file carried the anonymization rules, the dump is written in
plaintext. Other mistakes are just as quiet: a misspelled option is ignored, an unknown
masking function falls back to identity, `routines=0` *enables* routines, `threads=010`
means 8 threads.

**Guiding principle: emulate exactly what mydumper does with the file.** That means
mydumper's own pre-processor, GLib's GKeyFile parser, GLib's GOption parser, mydumper's
interpretation of table sections and masking functions, and the hand-off to libmysqlclient,
all for the exact mydumper version and build the user runs. Every blocking rule is
justified by a verified behavior (§3). Every runtime consequence a rule claims is proven
by an end-to-end test against the real mydumper binary (§11.6).

## 2. Goals, non-goals, decisions

### 2.1 Goals

1. Detect every file mydumper or myloader would reject entirely (MDL1xx), with no false
   negatives by construction (§6.2).
2. Detect configurations that load but are silently wrong: ignored keys and groups,
   altered values, options that do the opposite of what they say, anonymization that does
   not apply (MDL2xx–MDL6xx).
3. Fix automatically what can be fixed, with a formal guarantee for "safe" fixes (§7).
4. Target the exact mydumper version: every tag since v0.19.1-1 with an official image is
   embedded (§5).
5. CI-ready output: text, concise, JSON, SARIF 2.1.0, GitHub annotations, JUnit XML,
   GitLab Code Quality.
6. Distribution: one static binary (`CGO_ENABLED=0`), Docker image, pre-commit hooks,
   GitHub Action.
7. Opt-in project conventions (MDL9xx).

### 2.2 Non-goals

- Validating against a live database (existence of tables and columns).
- Formatting beyond rule fixes. mydumper-lint is not an opinionated formatter.
- Linting generic MySQL `my.cnf` files. Only the mydumper dialect is in scope; the MySQL
  client parser is modeled only where it creates mydumper-specific traps (MDL6xx).
- Running mydumper. The linter never executes it; only the test suite does (§11.6).

### 2.3 Decision log

| # | Decision | Status |
|---|---|---|
| D1 | Module path `github.com/tomsihap/mydumper-lint` | decided (git remote) |
| D2 | License Apache-2.0. `tools/oracle/` stays GPL-3.0-or-later and is never linked into the binary | proposed; employer clearance required before the repository goes public |
| D3 | Embed every mydumper tag ≥ v0.19.1-1 that has an official image (28 of 29 tags today); default target = latest stable release | decided 2026-09-24 |
| D4 | Independent SemVer for mydumper-lint; one binary supports every embedded mydumper version | decided 2026-09-24 |
| D5 | Full Docker end-to-end suite: MySQL plus the official mydumper images prove every runtime consequence | decided 2026-09-24 |
| D6 | Targeted MDL6xx family for libmysqlclient traps; no full MySQL option-file parser | decided 2026-09-24 |
| D7 | Rule ID prefix `MDL` | decided |
| D8 | Design documents in English under `docs/design/`; the French draft stays local and is never committed | proposed |
| D9 | No third-party runtime dependency beyond `github.com/dlclark/regexp2` and `go.yaml.in/yaml/v3` | proposed |

---

## 3. Reference behavior

This section is the source of truth for the rules. Every fact names its evidence:

- `case NN`: oracle case `testdata/oracle-cases/NN_*.cnf`. Cases 01–49 exist. Cases 50–69
  come from the design probes and are added in M1; Appendix A gives their exact bytes and
  oracle output.
- `gopt:NN`: GOption probe (Appendix B). `goption-expected.txt`: the existing GOption file.
- `src`: upstream source read at `v0.19.3-3`, `v1.0.8-1` and `master c01ca9d`
  (2026-09-17). The source is GPL-3.0: we read it to learn behavior and never copy it.

Status of each fact:

- **verified**: reproduced with the oracle or the real binary;
- **source**: read in the upstream code, not yet exercised end to end;
- **to verify**: a hypothesis. The named test must confirm it before any rule relies on it.

**GLib stability [verified].** The 49 existing cases and the 20 design probes produce
identical oracle output with GLib 2.68.4 (AlmaLinux 9, the base of the official mydumper
images), 2.80.0 (Ubuntu 24.04 "noble", used by the distribution packages) and 2.88.3
(latest). One Go emulation serves every supported environment; CI keeps checking all
three (§11.4).

### 3.1 Loading pipeline

```text
file ─► g_file_get_contents                      (I/O error ⇒ g_error: fatal)
     ─► mydumper pre-processor (§3.2)            (appends "= 1" to valueless lines)
     ─► g_key_file_load_from_data(KEEP_COMMENTS)
          └─ failure ⇒ g_warning("Failed to load config file %s: %s")
                     ⇒ the WHOLE file is ignored; execution continues
     ─► [mydumper] / [myloader] of this file ─► parse_key_file_group ─► GOption (§3.4)
     ─► the extra file: the same steps, then merged into this file (F15)
     ─► per-product option groups, mydumper only (F16) ─► GOption
     ─► [<app>_session_variables…] / [<app>_global_variables…] ─► server variables
     ─► table groups [`db`.`table`] ─► per-table settings and masking (§3.5)
     ─► libmysqlclient re-reads the same file for the connection (§3.7)
```

| ID | Fact | Evidence |
|---|---|---|
| F1 | A load failure is not fatal: `g_warning` then `return NULL`; the program continues with defaults. | src (v0.19.3-3, master) |
| F2 | `--defaults-file` (default `/etc/mydumper.cnf`) loads first, then `--defaults-extra-file`, through the same loader. Relative paths are resolved against the working directory. | src |
| F3 | In option groups, `host`, `user` and `password` are not passed to GOption. The MySQL client library reads them (§3.7). | src |
| F4 | Values are read raw with `g_key_file_get_value`: no escape processing (`\s`, `\n` stay literal) and no quote removal. | verified: cases 27, 65 |
| F5 | Group lookup is case-sensitive (`g_key_file_has_group`): `[MyDumper]` is never read. master defines a case-insensitive helper, but nothing calls it. | verified: case 34; src |
| F6 | Unknown options: versions that call `g_option_context_set_ignore_unknown_options(…, TRUE)` ignore them silently; older versions abort at startup (`option parsing failed: Unknown option …`). v0.19.3-3 aborts; v1.0.8-1 and master ignore. The generator records the behavior per tag. | verified: gopt:18; src |
| F15 | **NEW.** mydumper parses the tool group of each file on its own, the defaults file first, then the extra file, with the same option context: an option the extra file sets again replaces the defaults file's value. It then merges the extra file into the defaults file key by key (`g_key_file_set_value`: a key present in both takes the extra file's value, a new key or group is appended) and reads everything else from the merged file: per-product option groups (F16), server variable groups and table sections. When GLib rejects the defaults file there is nothing to merge into: the extra file's table sections, variable groups and per-product option groups are ignored, while its tool group still applies. Without a defaults file (and no `/etc/mydumper.cnf`), the extra file is loaded as the defaults file. Identical in every tag. | src (every tag); verified: e2e mdl510-defaults-rejected-drops-extra-masking, mdl510-defaults-rejected-extra-options-still-apply, mdl603-control-extra-file-without-tool-or-client-group |

### 3.2 The pre-processor

Before handing the content to GLib, mydumper rewrites it byte by byte so that valueless
boolean options (`routines`) become `routines= 1`. The function (`load_config_file`) is
token-identical in v0.19.3-3, v1.0.8-1 and master [verified: normalized diff]; the
generator fingerprints it for every tag (§5.3). Behavioral description (re-implemented
from this description, never from the GPL code):

**AMENDED (2026-09-25).** The pre-processor appeared in v0.19.3-1. v0.19.1-1 to v0.19.1-3
load the file with `g_key_file_load_from_file`, unchanged; a load failure is logged and
ignored the same way (F1) [verified: source of each tag]. The generator records a
`preprocessor` boolean per version (§5.3). For those versions the emulation runs in
*passthrough* mode: no `= 1`, no state leak; a line of spaces or an empty CRLF line is
harmless, while a valueless flag gets the file rejected (MDL113). Passthrough mode is
checked against GLib like the rest (`oracle --plain`, differential fuzzing).

```text
state: equal_found = false, new_line = true
for each byte c:
  if c == '[':
      copy every byte up to the next '\n' (excluded) or EOF, without updating the state;
      the terminating '\n' is then copied WITHOUT going through the '\n' branch
  else if c == '\n':
      if !equal_found && !new_line: emit "= 1"          (before the '\n')
      new_line = true; equal_found = false
  else:
      if c == '=': equal_found = true
      new_line = false
  emit c
```

- **P0.** The pre-processor only ever inserts `"= 1"` immediately before a `\n`. Line
  numbers are preserved, and each pre-processed line equals the original line plus an
  optional suffix. The emulator therefore never builds the pre-processed buffer (§4.3).
- **P1 (state leak).** After a line that contains `[`, the state is not reset: the next
  line starts with whatever state existed before the `[`.

| Situation | Effect | Evidence |
|---|---|---|
| Line of spaces or tabs followed by `\n` | becomes `␠␠= 1` (empty key) ⇒ file rejected | cases 01, 04 |
| Same line as the last line, without `\n` | loads, but is rejected as soon as anything is appended | cases 02, 03 |
| Empty line after CRLF endings (`\r\n` alone) | `\r` counts as a character ⇒ `\r= 1` ⇒ rejected | cases 36, 47 |
| A line containing `[` with no `=` before it (`# see [docs]`, `␠␠[mydumper]`, `key[fr]=v`), followed by an empty line | frozen `new_line=false` ⇒ the empty line becomes `= 1` ⇒ rejected | cases 13, 16, 37, 48 |
| The leak crosses a valid header at column 0 (`# see [docs]`, then `[myloader]`, then an empty line) | rejected: the header line is copied without resetting the state | case 58 |
| A line with `=` before `[` (`regex=^(a[bc])`) followed by a flag without `=` | frozen `equal_found=true` ⇒ no `= 1` ⇒ rejected | case 12 |
| The same line followed by an empty line | harmless: `equal_found=true` suppresses the insertion | cases 38, 62 |
| Flag without `=` on the last line, no final `\n` | no `= 1` ⇒ rejected | case 18 |
| Flag without `=` anywhere else | `routines= 1` ⇒ value `1` | cases 19, 45 |
| Comment without `=` | `#x= 1`, still a comment | case 28 |

### 3.3 GKeyFile (after pre-processing)

| ID | Behavior | Evidence |
|---|---|---|
| K1 | Lines split on `\n`; one `\r` immediately before `\n` is removed. | case 05 |
| K2 | Leading whitespace is skipped before classifying a line: space, `\t`, `\n`, `\f`, `\r` (GLib's `g_ascii_isspace`). `\v` is **not** whitespace: `\vroutines=1` defines the key `\vroutines`. | cases 15, 17, 60 |
| K3 | A line whose first significant byte is `#` or NUL, or an empty line, is a comment. `;` is **not** a comment: `; a comment` is the key `; a comment` with value `1`. | cases 07, 31, 66 |
| K4 | Group line = `[` name `]`, optionally followed by spaces or tabs. The name ends at the first `]`. Any other text after `]` ⇒ rejected. Empty name, `[` in the name, or an ASCII control character in the name (TAB, DEL…) ⇒ rejected (`Invalid group name`). A NUL inside the brackets hides the `]` ⇒ rejected. Missing `]` ⇒ rejected. | cases 09, 26, 40, 42, 52, 53, 64, 67, 68 |
| K5 | `[ mydumper ]` is valid but names the group `" mydumper "`, which mydumper never reads. | case 49 |
| K6 | A key before any group ⇒ rejected (`Key file does not start with a group`). | case 08 |
| K7 | A key/value line contains `=` somewhere other than its first significant position; `=1` ⇒ rejected. | case 23 |
| K8 | Key: trailing whitespace removed; inner spaces allowed (`chunk filesize`). | cases 20, 25 |
| K9 | Value: leading whitespace removed, trailing whitespace **kept**. | cases 14, 20 |
| K10 | No end-of-line comments: `threads=4 # four` ⇒ value `4 # four`. | case 21 |
| K11 | Only the first `=` splits: `where=a=b` ⇒ `a=b`. | case 46 |
| K12 | Duplicate group ⇒ silent merge. | case 10 |
| K13 | Duplicate key ⇒ `get_keys` returns both entries, `get_value` returns the last value for both; the first value is lost. | case 11 |
| K14 | A localized key `key[locale]` appears in `get_keys` only if `locale` is in the mydumper process's language list (`g_get_language_names()`, built from `LANGUAGE`, `LC_ALL`, `LC_MESSAGES`, `LANG`, and always containing `C`). mydumper then passes `--key[locale]`, an unknown option. Otherwise the key is invisible. | cases 41, 54, 55, 69 |
| K15 | A UTF-8 BOM makes the first line unparsable ⇒ rejected. | case 06 |
| K16 | Invalid UTF-8 in a key, value or group name is accepted as-is. | cases 24, 43, 44 |
| K17 | NUL bytes (lines are classified as C strings): NUL as first significant byte ⇒ the line is a comment and its entry silently disappears; NUL before `=` ⇒ rejected; NUL in a value ⇒ value truncated at the NUL; NUL in a group header ⇒ rejected. | cases 31, 56, 57, 66, 67 |
| K18 | Empty file, or comments only ⇒ loads with zero groups. | cases 29, 30 |
| K19 | Key names: a `]` anywhere, or a `[` that does not start a final locale suffix `[`(alphanumerics, `-`, `_`, `.`, `@`)`]`, ⇒ rejected (`Invalid key name`). This applies to masked columns too: `` `c[0]` ``. | cases 50, 51, 61, 69 |
| K20 | GLib stops at the first error; mydumper only ever sees that error. | src |

GLib messages the emulator reproduces verbatim (MDL1xx quote them):

- `Key file contains line “%s” which is not a key-value pair, group, or comment`
- `Key file does not start with a group`
- `Invalid group name: %s`
- `Invalid key name: %s`

### 3.4 GOption (each key becomes `--key value`)

mydumper builds `argv = [group, --k1, v1, --k2, v2, …]` in `get_keys` order and parses it
with `g_option_context_parse_strv`.

| ID | Input | Result | Evidence |
|---|---|---|---|
| G1 | `routines=0` (option without argument) | routines **enabled**; `0` left as a stray argument | goption-expected.txt |
| G2 | `routines=false` | routines **enabled** | goption-expected.txt |
| G3 | `split_string_pk=1` (real name `split-string-pk`) | unknown option: `_` and `-` are not interchangeable | goption-expected.txt |
| G4 | `Routines=1` | unknown option: case matters | goption-expected.txt |
| G5 | `chunk-filesize=10␠` (integer option) | `Cannot parse integer value “10 ”` ⇒ fatal | goption-expected.txt |
| G6 | `compress=ZSTD` (callback with optional argument) | value received | goption-expected.txt |
| G7 | Integers are parsed like C `strtol` in base 0: `010` ⇒ 8 (octal), `0x10` ⇒ 16, `+5` and `-1` accepted; `08`, `1e3`, `10␠` and the empty string ⇒ `Cannot parse integer value` ⇒ fatal; overflow ⇒ `out of range` ⇒ fatal. | gopt:01–10 |
| G8 | A value starting with `-` after an option without argument, or after an optional-argument option, is parsed as option(s). `routines=-t` makes `-t` (short for `threads`) consume the next key name as its value ⇒ fatal. | gopt:11, 13 |
| G9 | A value `--` after an option without argument ends option parsing: every following key of the group is silently ignored. | gopt:12 |
| G10 | Options with a required argument take the next element verbatim, even if it starts with `-` (`outputdir=-x` ⇒ `-x`). | gopt:15 |
| G11 | A repeated option (duplicate key, K13) is parsed twice with the last value. | gopt:16 |
| G12 | `REVERSE` flag: presence sets the option to false whatever the value (`nodata=1` and `nodata=0` both disable data). | gopt:17 |
| G13 | Unknown option: ignored or fatal depending on the version (F6). | gopt:18 |
| G14 | Argument types used by mydumper from v0.19.3-3 to master: NONE, STRING, FILENAME, INT, CALLBACK; flags OPTIONAL_ARG and REVERSE. The emulator supports every GLib scalar type, so future versions need no emulator change. | src |
| G15 | CALLBACK options (26 in v0.19.3-3, 28 in master) validate values with option-specific code (`compress`, `rows`, …). GOption only hands over the raw value. | src |
| G16 | **NEW.** GOption converts the values of STRING options and of callbacks without the FILENAME flag from the locale's charset (`g_locale_to_utf8`); mydumper calls `setlocale(LC_ALL, "")`. In the C locale (LANG unset: cron, the official images) any byte ≥ 0x80 fails: `Invalid byte sequence in conversion input` ⇒ fatal. In a UTF-8 locale only invalid UTF-8 fails. An optional-argument callback whose value fails to convert is called with no value, and parsing continues. FILENAME options are never converted. The model assumes the C locale, like K14's language list. | oracle: x:string-non-ascii-c-locale, random differential; to verify: e2e |
| G17 | **NEW.** When parsing fails, GLib reverts what the parse changed in its own way: flags touched become false (their previous value is never saved), numbers get the value before their last assignment, strings and arrays their value before the parse. Callback calls are not undone. Irrelevant to mydumper (it aborts), reproduced for conformance. | oracle: random differential |

### 3.5 Table sections and masking

| ID | Fact | Evidence |
|---|---|---|
| F7 | A group is a table section if and only if its name starts with `` ` ``, contains `` `.` `` and ends with `` ` ``: ``[`db`.`table`]``, ```[`db`.``]``` (every table of `db`), ```[``.`table`]``` (that table in every database). `[db.table]` and ``[`db.table`]`` are ignored. | src |
| F8 | Keys recognized in a table section (master): `where`, `limit`, `num_threads`, `columns_on_select`, `columns_on_insert`, `object_to_export`, `object_to_import`, `rows`, `partition_regex`, `columns_on_select_replace` and its ``columns_on_select_replace_`col` `` variants, `skip-{index,table,view,trigger,data,database,routine,event}-checksums`. Any other key is ignored silently. The exact list per version comes from the generator. | case 33; src |
| F9 | A key that starts with `` ` `` and contains a second `` ` `` is a masked column. A key starting with `` ` `` without a second one falls through to the generic branch and is ignored. | src |
| F10 | The masking function is selected by **prefix** among `random_format`, `random_string`, `random_int`, `random_uuid`, `apply`, `constant`, `regex`, and `null` (in master, absent from v0.19.3-3). An unknown function logs `Function not found: Using default` (at `--verbose 3`) and falls back to the identity function. **AMENDED:** up to v0.21.2-4 identity returns the value: **the column is exported in plaintext**; from v0.21.3-1 (new masking API) identity writes nothing: **the column is exported empty**, silently. | src; e2e mdl502-* |
| F11 | Syntax errors in masking arguments (`<file …>`, `<string N>`, `<number N>`, `<regex '…'>`, a missing quote or `>`, the element count of `apply` and `regex`) and in the modifiers `WITH_MEM`, `REPLACE_NULL`, `UNIQUE`, `MAX_LENGTH` trigger `g_error`: the dump aborts. | src |

### 3.6 Recognized groups

| Group | Read by | Notes |
|---|---|---|
| `mydumper`, `myloader` | GOption (tool options) | exact case (F5) |
| `<app>_<product>`, then `_<major>`, `_<secondary>`, `_<revision>` appended cumulatively: `mydumper_mysql`, `mydumper_mysql_8`, `mydumper_mysql_8_0`, `mydumper_mysql_8_0_36` | GOption, **mydumper only, from v0.21.2-2** (F16), only when the server matches; more specific groups are parsed later and override | `<product>` is `get_product_name()` in lowercase: `mysql`, `percona`, `mariadb`, `tidb`, `rds`, `google`, `clickhouse`, `dolt`, `unknown` (src) |
| `<app>_session_variables`, `<app>_global_variables`, with the same product and version suffixes | server variables; any key accepted | src |
| `client` | libmysqlclient | §3.7 |
| table sections | per-table settings, masking | §3.5 |
| anything else | nobody: ignored | |

| ID | Fact | Evidence |
|---|---|---|
| F16 | **NEW.** Per-product option groups are read by mydumper only, from v0.21.2-2 (`load_options_for_product_from_key_file`, after connecting, from the merged file of F15). mydumper up to v0.21.2-1 ignores them, and no myloader version reads `[myloader_<product>…]`: MDL201 reports such groups. Variable groups with product suffixes are read by both tools in every version. The generator records the readers per version (`product_option_groups`, §5.3). | src (every tag); verified: e2e f16-product-option-group-mydumper, f16-product-option-group-myloader |

### 3.7 The second parser: libmysqlclient

mydumper also hands the file to the MySQL client library (`MYSQL_READ_DEFAULT_FILE` and
`MYSQL_READ_DEFAULT_GROUP` in `connection.c`), which parses it again with the MySQL
option-file syntax, a different grammar.

| ID | Fact | Evidence |
|---|---|---|
| F12 | When a file fails to load (F1), mydumper still registers it as the client defaults file, with no group. `[client]` credentials keep working while every mydumper option is dropped, so the run looks healthy. | src; to verify: e2e |
| F13 | When a loaded file contains the tool group, it becomes the client defaults file with that group. If it also contains `[client]`, the group is reset to none. | src |
| F14 | The extra file goes through the same logic after the defaults file: it replaces the defaults file as the client defaults file when it contains the tool group or `[client]`, or when it fails to load. | src |
| C1 | With no group set, libmysqlclient reads only the `[client]` family of groups. Combined with F3 and F13: `host`, `user` and `password` in `[mydumper]` are read by nobody when the file also has `[client]`. | to verify: e2e |
| C2 | `MYSQL_READ_DEFAULT_FILE` reads that one file only. Combined with F14: the defaults file's `[client]` is ignored once the extra file takes over. | to verify: e2e |
| C3 | `!include` and `!includedir` are MySQL directives. GKeyFile loads them as a key (`!include /x.cnf` = `1`), which mydumper sees as an unknown option in its option groups. libmysqlclient follows the directive: the included file's connection settings apply. | verified: case 59; e2e mdl602-include-directive-in-{tool,client}-group |
| C4 | The MySQL parser strips quotes and interprets escapes; GKeyFile does neither (F4). The same line means different things to the two parsers. | to verify: e2e |
| C5 | **NEW.** The MySQL client library rejects a file with an option before the first group, and a UTF-8 BOM makes the first line such an option (`Found option without preceding group … Fatal error in defaults handling`). Every connection setting of the file is then lost: F12 only holds when the client library accepts the file (a whitespace-only or CRLF empty line is fine for it). | verified: e2e mdl101-bom-drops-masking, mdl102-whitespace-line-drops-masking |

### 3.8 Versions, builds and images

| ID | Fact | Evidence (checked 2026-09-24) |
|---|---|---|
| V1 | 29 tags ≥ v0.19.1-1. A "stable" release is a tag with a non-pre-release GitHub release. Latest stable per branch: v0.19.3-3, v0.20.1-2, v0.21.3-1, v1.0.5-1. Pre-releases include v0.21.4-1 and v1.0.8-1. Tags without a GitHub release (v0.20.2-1, v1.0.1-3) count as pre-releases. | GitHub releases API |
| V2 | Official images `mydumper/mydumper:<tag>` exist for every tag except v0.20.2-1, plus builds without a git tag (v0.19.2-x, v0.19.4-x). They are AlmaLinux 9 builds (GLib 2.68.4) against the MySQL 8.4 client, with SSL. | Docker Hub; image inspection |
| V3 | Image tags are not reliable: `mydumper/mydumper:v1.0.5-1` contains a `v1.0.3-1` binary. | `mydumper --version` in the image |
| V4 | `--help-all` does not exist (fatal `Unknown option`); `--help` lists every option, grouped in sections. | official image |
| V5 | The option set depends on build flags: `WITH_SSL` adds `ssl`, `ssl-mode`, `key`, `cert`, `ca`, `capath`, `cipher`, `tls-version`; `LIBMARIADB` changes `ssl-mode`. Distribution packages may be built differently from the official image. | src |
| V6 | The option set changes between releases (roughly 170 option entries in v0.19.3-3, 200 in master). | src |

---

## 4. Architecture

### 4.1 Approach

**Faithful pure-Go emulation built around a lossless line table.** Two alternatives were
rejected:

- *Rules that each re-scan the text* (editorconfig-checker style): quick to start, but no
  guarantee that every rejected file is caught, and every rule re-implements the quirks
  its own way.
- *cgo bindings to GLib*: GKeyFile fidelity for free, but it breaks the static binary and
  Windows cross-compilation, and the pre-processor and GOption logic would still need
  re-implementing. The C oracle plays this role, in tests only.

### 4.2 Pipeline

```text
[]byte
 ─► source.Parse        lossless line table
 ─► preprocess.Run      real + ideal pre-processing per line; leak origins
 ─► keyfile.Parse       GKeyFile emulation: first error (what mydumper sees)
                        + recovered reading of every line
 ─► model.Build         effective configuration for the target version and build, per tool
                        (uses goption for argv semantics and optionsdb for option tables)
 ─► rules.Run           pure functions over every layer ─► []Diagnostic
 ─► report.*            text | concise | json | sarif | github | junit | gitlab
 └► fix.Apply           byte edits ─► fixpoint ─► self-check ─► atomic write
```

### 4.3 Packages

| Package | Responsibility |
|---|---|
| `internal/source` | Line table: byte span, content and terminator (LF, CRLF, none) of every line; byte ↔ (line, column) conversions. |
| `internal/preprocess` | Real and ideal pre-processors (§3.2), per line: `= 1` appended by the real one? by the ideal one? state inherited from which line (leak origin)? |
| `internal/keyfile` | GKeyFile emulation on (original line + optional `= 1`), using P0: classification of every line, first GLib error with the exact message, rejection cause taxonomy (§6.2), groups and entries with spans in the original file, duplicates and localized keys preserved. |
| `internal/goption` | GOption emulation: from option definitions and a mydumper-style argv, the final option state, stray arguments, and the fatal error if any (G1–G13). |
| `internal/optionsdb` | Embedded knowledge base (§5); version and build resolution. |
| `internal/model` | Effective configuration: every entry marked effective or not, with a reason (§4.5). |
| `internal/sqlscan` | Tolerant SQL scanner: quotes (`'`, `"`, `` ` ``, with doubled and backslash escapes), comments, parentheses; top-level comma splitting. |
| `internal/masquerade` | Versioned grammar of masking functions and modifiers (F10, F11). |
| `internal/rules` | Rule registry, metadata, one file per rule, suppressions. |
| `internal/fix` | Edits, conflict resolution, fixpoint, self-check, atomic write. |
| `internal/textdiff` | Unified diff for `--diff` (port of Go's `internal/diff`, BSD-3-Clause, notice kept). |
| `internal/config` | `.mydumper-lint.yaml`: discovery, strict decoding, overrides, JSON Schema generation. |
| `internal/report` | Output formats (§10). |
| `internal/cli` | Commands and flag parsing: stdlib `flag`, plus support for flags placed after paths. |
| `cmd/mydumper-lint` | `main`. |

### 4.4 Core data model (illustrative)

```go
// source
type Line struct {
    Num        int        // 1-based
    Start, End int        // byte span of the content, terminator excluded
    Term       Terminator // LF, CRLF, None
}

// preprocess
type LineState struct {
    AppendsEqOne      bool // the real pre-processor inserts "= 1" before this line's '\n'
    IdealAppendsEqOne bool // a correct pre-processor would
    LeakOrigin        int  // 0, or the line whose '[' copy froze the state this line starts with
}

// keyfile
type Result struct {
    Loadable   bool
    FirstError *GLibError  // exactly what mydumper logs: message and line
    Lines      []LineClass // every line, including those after the first error
    Groups     []Group     // file order, duplicates kept
}
type LineClass struct {
    Kind  Kind  // Blank, Comment, Group, Entry, Rejected
    Cause Cause // Rejected lines only: BOM, WhitespaceOnly, CarriageReturn, LeakBlank, LeakFlag, …
}

// rules
type Diagnostic struct {
    Rule        RuleID
    Severity    Severity
    Span        Span       // byte offsets in the original file
    Message     string
    Consequence string     // what mydumper does, for the target version
    Related     []Location // e.g. the origin line of a bracket leak
    Fix         *Fix
}
type Fix struct {
    Applicability Applicability // Safe or Unsafe
    Description   string
    Edits         []Edit        // applied together
}
type Edit struct {
    Start, End int // byte range in the original file
    New        []byte
}
```

### 4.5 Two readings of a file, and the effective model

GLib stops at the first error (K20), and mydumper only ever sees that error.
`keyfile.Parse` therefore returns two readings:

1. **The GLib reading:** `Loadable` and the first error, with GLib's exact message.
2. **The recovered reading:** every line classified, including lines after errors, as if
   each earlier rejected line had been repaired by its documented recovery rule (§7.2).
   Users get every problem in one run instead of one per run. This reading also defines
   what the author meant, which the fixer relies on (§7.3).

`model.Build` turns a reading into an **effective configuration** for the target version
and build. For each tool it contains the final GOption state (option ⇒ value), the table
sections and their masking functions, the server variables, and the raw `[client]`
entries. Every entry that does not reach mydumper carries a reason:

`file-rejected`, `unknown-group`, `unknown-option`, `fatal-at-startup`,
`shadowed-by-duplicate`, `localized`, `connection-key`, `conditional` (product or version
group, applied only to matching servers), `unknown-table-key`,
`masquerade-fallback-identity`, `consumed-as-option-value` (G8), `after-end-of-options` (G9).

`inspect` prints this model (§8.3), and the fixer's self-check compares models (§7.3).

### 4.6 Positions

Offsets are bytes internally. Outputs use 1-based lines and 1-based columns counted in
Unicode code points; an invalid UTF-8 byte counts as one column. JSON also carries byte
offsets, SARIF declares `columnKind: "unicodeCodePoints"`, and the future LSP server will
convert to UTF-16.

### 4.7 Determinism and performance

Rules are pure: no I/O, no global state. Files are processed in parallel by a worker
pool, and output is sorted by file, line, column and rule ID. Each layer is a single pass
without regular expressions on hot paths. Target (from the draft spec): 1,000 files of
100 KB in under one second on a recent laptop, measured in CI (§11.9).

### 4.8 Repository layout

```text
cmd/mydumper-lint/
internal/...                              packages of §4.3
internal/optionsdb/data/optionsdb.json    generated, embedded
internal/rules/testdata/<ID>/*.txtar      golden rule tests
tools/oracle/                             C oracle (GPL-3.0-or-later), Dockerfiles per GLib version
tools/gen-optionsdb/                      knowledge-base generator
tools/gendocs/                            rule docs, README tables, JSON Schemas
e2e/                                      Docker end-to-end suite (build tag e2e)
testdata/oracle-cases/                    byte-exact GKeyFile cases + expected.txt
testdata/goption-cases/                   GOption cases + expected output
schemas/                                  config.v1.json, output.v1.json (generated)
docs/design/                              this document
docs/rules/MDLxxx.md                      generated
.github/                                  workflows, templates, dependabot
action.yml  .pre-commit-hooks.yaml  .goreleaser.yaml  Dockerfile  Makefile
```

### 4.9 Dependencies

Allowed at runtime: the Go standard library, `github.com/dlclark/regexp2` (PCRE-like
regular expressions, lookarounds included, used to validate regex values) and
`go.yaml.in/yaml/v3`, the maintained successor of `gopkg.in/yaml.v3`, whose last release
dates from 2022. `golang.org/x/*` packages only if a concrete need appears, such as
terminal detection on Windows. No CLI framework, no test framework, no Docker SDK: the
e2e suite drives the `docker` CLI. Development tools (golangci-lint, goreleaser, gremlins)
run from pinned Docker images and pinned CI actions; they are never dependencies of the
module.

---

## 5. Knowledge base (optionsdb)

### 5.1 Content

For each embedded mydumper version: tag, commit, release date, stable or pre-release,
`ignore_unknown_options` (F6), options (long name, short name, tool — mydumper, myloader
or both —, argument type, flags, build condition, `is_regex`, callback validator), table
keys (F8), masking functions (F10) with their grammar version (F11), products (§3.6), and
fingerprints of the loader code the emulators depend on, and whether the loader runs the
pre-processor (§3.2, §5.3).

### 5.2 One consolidated history file

Instead of one JSON file per version, the generator writes a single `optionsdb.json` in
which every fact carries the version ranges where it holds:

```json
{"name": "some-option", "short": "", "tool": "mydumper",
 "spans": [{"from": "v0.20.1-1", "to": null, "arg": "none", "flags": [], "condition": null}]}
```

Why:

- the 28 embedded versions share most facts;
- adding a release produces a small, reviewable diff ("`--foo` added in v1.0.9-1");
- messages can cite history: "`--foo` was added in v0.20.1-1; your target is v0.19.3-3".

The file is sorted and fully deterministic.

### 5.3 Generator (`go run ./tools/gen-optionsdb`)

1. List upstream tags ≥ v0.19.1-1 and their GitHub release status.
2. For each tag (cached): fetch the source archive; extract every
   `GOptionEntry … [] = { … }` array in `src/**` with a small C tokenizer that handles
   string concatenation, `|`-combined flags, and `#if`/`#ifdef` blocks (recorded as build
   conditions). Attribute the tool by path: `src/mydumper/` ⇒ mydumper, `src/myloader/` ⇒
   myloader, other `src/` files ⇒ both. Detect
   `g_option_context_set_ignore_unknown_options(…, TRUE)`, and whether `load_config_file`
   runs the pre-processor (its `"= 1"` literal: `preprocessor`, §3.2), and which tools
   call `load_options_for_product_from_key_file` (`product_option_groups`, F16). Extract table keys (the
   comparisons in `load_per_table_info_from_key_file` and the `#define`s they use), masking
   functions (`get_function_pointer_for`) and products (`get_product_name`). Fingerprint
   `load_config_file` and `parse_key_file_group`: a hash of their tokens with whitespace
   and comments removed. The emulators encode these functions' behavior, so a changed
   fingerprint must never go unnoticed.
3. Merge the per-tag results into version ranges and write sorted JSON.
4. Apply a small, hand-maintained overlay (`tools/gen-optionsdb/overlay.yaml`) for what
   the extractor cannot infer, such as `is_regex` and callback validators. Every overlay
   entry cites its evidence.

### 5.4 Cross-check against the real binaries

For every version, CI runs `mydumper --version` and `--help` (and myloader's) from the
official image:

- the version must match the tag (V3). A mismatch marks the image unusable; it is never
  silently accepted;
- the long and short option names must equal the generated set for the official build
  (`WITH_SSL`, MySQL client).

Any difference fails the generator run. A tag with no usable image (v0.20.2-1 today) is
not embedded; targeting it resolves to the nearest lower version with a warning (§5.6).

### 5.5 Build variants

The default build is the official image's: MySQL client, SSL enabled. The config key
`mydumper-build: {client: mysql|mariadb, ssl: true|false}` selects the conditional options
of other builds (V5).

### 5.6 Version resolution and default

`--mydumper-version` (or `mydumper-version:` in the config) accepts `v0.19.3-3`,
`0.19.3-3`, `0.19.3` (latest build of 0.19.3), `0.19` (latest stable 0.19.x), `latest`
(latest stable) and `latest-prerelease`. A version absent from the knowledge base (image
builds without a git tag, such as v0.19.4-23) resolves to the nearest lower embedded
version, with a warning. A version newer than every embedded one resolves to the newest,
with a warning that mydumper-lint may be outdated. A version older than v0.19.1-1 is a
usage error (exit 2).

Default when nothing is configured: the latest stable release, plus a one-line notice in
text output recommending to pin the version, because behavior differs sharply between
versions (F6, F10).

### 5.7 Keeping up with upstream

A scheduled workflow (daily) detects new upstream tags, runs the generator, the
cross-check (§5.4) and the end-to-end suite for the new version, then opens a pull request
summarizing the changes: options added, removed or retyped, `ignore_unknown_options`, new
masking functions. A changed loader fingerprint (§5.3) marks the pull request as needing
a manual review of the emulators before merge. Merging it ships a minor release (§13).

---

## 6. Rules

### 6.1 Conventions

- ID `MDL` + 3 digits and a stable kebab-case name. Both are accepted everywhere
  (`--select`, config, suppressions). IDs are never reused; a renamed rule keeps its old
  name as an alias.
- Severities: `error` (file rejected, config ignored, data leaked in plaintext, abort at
  runtime), `warning` (probably unintended), `info` (hygiene). Configurable per rule,
  `off` included.
- Fix applicability: `safe`, `unsafe`, or none (§7.1).
- Every diagnostic carries: file, range, rule ID and name, severity, message, the
  mydumper consequence in one sentence for the target version, related locations, fix.
- Every rule carries metadata: ID, name, family, default severity, fix applicability,
  applicable versions, opt-in and preview flags, runtime consequence with the e2e scenario
  that proves it, and documentation (summary, why, ❌ example, ✅ fix). Documentation
  examples are taken from the rule's golden tests, so they are always correct.
- **Preview:** new rules ship as preview (enabled with `--preview` or `preview: true`) and
  become stable in a later minor release, so upgrading never breaks CI unexpectedly (§13).
- **Opt-in:** disabled by default, enabled with `extend-select`.

### 6.2 Loadability by construction

The keyfile emulator assigns each rejected line exactly one **cause**, and each cause maps
to exactly one rule among MDL101–MDL111. MDL109 is the fallback for a cause the taxonomy does not name —
unreachable by design; a test fails if the fuzzer ever reaches it. Every unloadable file
therefore gets at least one MDL1xx error: no false negatives. MDL1xx rules also report
**latent** risks: files that load today but break as soon as content is appended or
edited. MDL1xx diagnostics cannot be suppressed by comments.

### 6.3 Catalog

**NEW** = not in the draft spec. **AMENDED** = changed after verification.

#### MDL0xx — Suppressions

| ID | Name | Severity | Trigger | Fix |
|---|---|---|---|---|
| MDL001 | `unused-suppression` | info | a suppression that suppressed nothing | safe: remove it, or its unused IDs |
| MDL002 | `invalid-suppression` | warning | **NEW.** Unknown ID or name, malformed directive, directive without IDs, or a directive targeting an MDL1xx rule | — |

#### MDL1xx — The whole file would be rejected

All `error`, except MDL112.

| ID | Name | Trigger | Fix | Ref. |
|---|---|---|---|---|
| MDL101 | `utf8-bom` | BOM at the start of the file | safe: remove | K15 |
| MDL102 | `whitespace-only-line` | non-empty line made only of spaces and tabs, including a last line without `\n` (latent) | safe: empty the line | §3.2 |
| MDL103 | `carriage-return` | `\r` present; stronger message when an empty line makes it fatal | safe: `\r\n` → `\n`. Lone `\r`: none | K1, §3.2 |
| MDL104 | `missing-final-newline` | non-empty file not ending with `\n` (fatal when the last line is a flag, latent otherwise) | safe: append `\n` | §3.2 |
| MDL105 | `key-before-group` | the first significant line is not a group | — | K6 |
| MDL106 | `invalid-group-line` | **AMENDED.** Text after `]`, missing `]`, empty name, `[` or a control character in the name, NUL in the header | safe only when the text after `]` is a `#` comment without `[` (moved to its own line above the header); otherwise none | K4 |
| MDL107 | `empty-key` | line whose first significant character is `=` | — | K7 |
| MDL108 | `bracket-state-leak` | **AMENDED.** A line that the real and ideal pre-processors transform differently: (a) an empty line becoming `= 1`, (b) a flag not receiving `= 1`. Primary location: the affected line. Related location: the leak origin, possibly several lines above (case 58). The message explains the mechanism. | safe: (a) delete the affected empty line(s); (b) `key` → `key=1` | P1 |
| MDL109 | `unparsable-line` | fallback: a rejected line without a named cause. Quotes GLib's message. | — | — |
| MDL110 | `invalid-key-name` | **NEW.** `]` in a key, or a `[` that does not form a valid locale suffix: `foo]=1`, `foo[bar baz]=1`, `` `c[0]`=… `` | — | K19 |
| MDL111 | `nul-byte` | **NEW.** A NUL byte that makes a line unparsable | — | K17 |
| MDL112 | `bracket-leak-risk` | **NEW.** A line after which an empty line would become `= 1` (P1): a comment containing `[`, an indented header, a localized key. **warning** when it is the last line of the file (a script appending a block would break the file), **info** otherwise | — | P1 |
| MDL113 | `unsupported-flag-without-value` | **NEW.** A line without `=` in a version without the pre-processor (v0.19.1-x): GLib rejects it and mydumper ignores the file. A line starting with `;` gets a message explaining that `;` is not a comment | safe, only when the line is a bare option name: `key` → `key=1` | §3.2 |

#### MDL2xx — Groups

| ID | Name | Severity | Trigger | Fix | Ref. |
|---|---|---|---|---|---|
| MDL201 | `unknown-group` | warning | group not recognized (§3.6), hence ignored; "did you mean" | — | §3.6 |
| MDL202 | `group-name-case` | error | matches a known group only when ignoring case (`[MyDumper]`) | unsafe: fix the case | F5 |
| MDL203 | `group-name-whitespace` | error | spaces at the start or end of the name (`[ mydumper ]`) | unsafe: trim | K5 |
| MDL204 | `malformed-table-group` | error | looks like a table (contains `.` or `` ` ``) but fails F7 (`[db.table]`, ``[`db.table`]``) | unsafe when ``[`db`.`table`]`` is unambiguous | F7 |
| MDL205 | `duplicate-group` | warning | the same group declared twice (silent merge) | unsafe: merge into the first occurrence | K12 |

#### MDL3xx — Lines, keys, values

| ID | Name | Severity | Trigger | Fix | Ref. |
|---|---|---|---|---|---|
| MDL301 | `duplicate-key` | warning; error if the values differ | the same key twice in a group: the last value wins and is applied twice (G11); the first is lost | unsafe: delete the earlier occurrences | K13 |
| MDL302 | `trailing-whitespace-in-value` | error | value ending with a space or tab: integers abort (G5), paths and regexes silently differ (K9) | unsafe (can be promoted with `fix.extend-safe`): trim | K9, G5 |
| MDL303 | `inline-comment` | warning | ` #` or `\t#` in a value (K10); for SQL-valued keys, only outside quotes (sqlscan) | unsafe: move the comment to its own line above, unless it contains `[` | K10 |
| MDL304 | `semicolon-comment` | error | line starting with `;`: a stray key, fatal wherever unknown options are fatal | **AMENDED.** `;` → `#`: safe when the key has no effect (ignored unknown option, unread group), unsafe otherwise, since removing an effect changes what mydumper does | K3, F6 |
| MDL305 | `localized-key` | warning | **AMENDED.** `name[xx]`: invisible, or passed as the unknown option `--name[xx]`, depending on the runtime locale (K14); `[C]` is always passed | — | K14 |
| MDL306 | `leading-whitespace` | info | indentation before a key, comment or group: tolerated by GLib, but an indented group header is a leak origin | safe: remove | K2, P1 |
| MDL307 | `spaces-around-equals` | info | `key = value` | safe: `key=value` | K8, K9 |
| MDL308 | `flag-without-value` | info | option without `=` (`routines`): valid, but fragile (cases 12, 18) | safe: `routines=1` | §3.2 |
| MDL309 | `trailing-whitespace` | warning | trailing spaces on a comment or group line | safe: remove | K4 |
| MDL310 | `blank-lines` | info | several consecutive empty lines, or empty lines at the end of the file | safe: collapse | — |
| MDL311 | `invalid-utf8` | warning | invalid UTF-8 sequence | — | K16 |
| MDL312 | `control-character` | **AMENDED.** warning; error when a NUL changes the configuration (drops the line when at line start, truncates a value) | C0 control character other than `\t`, `\n`, `\r`, including `\v`, which GLib does not treat as whitespace (K2) | — | K2, K17 |
| MDL313 | `quoted-value` | **NEW.** warning | value wrapped in quotes for a non-SQL option: mydumper keeps them, so `outputdir="/backup"` names a directory `"/backup"`, quotes included (F4, unlike the MySQL client, C4) | unsafe: remove the quotes | F4 |

#### MDL4xx — mydumper and myloader options

Applies to `[mydumper]`, `[myloader]` and their product and version variants (§3.6). The
group determines the tool; the knowledge base (§5) provides the option table for the
target version and build.

| ID | Name | Severity | Trigger | Fix | Ref. |
|---|---|---|---|---|---|
| MDL401 | `unknown-option` | error | key absent from the tool's option table. The message explains: leading dashes (`--threads`), short names (`t` → `threads`), case and `_`/`-` (G3, G4), the other tool (`outputdir` in `[myloader]`), version history ("added in v0.20.1-1"), and a suggestion (Levenshtein ≤ 2). The consequence depends on the version (F6): "silently ignored" or "fatal at startup". | unsafe when exactly one candidate matches after normalization | F6, G3, G4 |
| MDL402 | `boolean-flag-value` | error | option without argument given a false-looking value (`0`, `false`, `no`, `off`, any case): the flag is **enabled** anyway (G1, G2); `REVERSE` flags likewise (G12). info for `true`, `yes`, `on` | unsafe: comment the line out | G1, G2, G12 |
| MDL403 | `invalid-number` | error | integer option whose value `strtol` base 0 rejects (`08`, `1e3`, `10␠`) or that overflows | — (trailing spaces: MDL302) | G5, G7 |
| MDL404 | `empty-option-value` | **AMENDED.** error for integer options (fatal, G7); warning otherwise | `key=` on an option that expects a value | — | G7 |
| MDL405 | `plaintext-password` | warning | `password` in any group: a secret in a file that is often versioned. Recommends `~/.mylogin.cnf`, environment variables, or a file outside the repository | — | F3 |
| MDL406 | `invalid-regex` | warning | regex-typed option that does not compile with regexp2 (best effort: mydumper compiles it with PCRE). Raw value (F4) | — | F4 |
| MDL407 | `octal-integer` | **NEW.** warning | integer written as `0` followed by octal digits: `010` means 8 (`08` and `09` are MDL403) | — (both readings are plausible) | G7 |
| MDL408 | `value-parsed-as-option` | **NEW.** error | value starting with `-` for an option without argument or with an optional argument: GOption parses it as options (G8); the value `--` silently drops every following key of the group (G9) | — | G8, G9 |
| MDL409 | `non-ascii-option-value` | **NEW.** warning; error when the value is not valid UTF-8 | value of a converted option (G16) with non-ASCII bytes: mydumper aborts at startup without a UTF-8 locale; an optional-value callback silently receives no value | — | G16 |

#### MDL5xx — Table sections and masking

| ID | Name | Severity | Trigger | Fix | Ref. |
|---|---|---|---|---|---|
| MDL501 | `unknown-table-key` | error | key not recognized in a table section (F8), hence silently ignored; "did you mean" | unsafe when the match is unique | F8 |
| MDL502 | `unknown-masquerade-function` | error | `` `col`=value `` whose value starts with no function known to the version ⇒ identity ⇒ **column exported in plaintext** | unsafe when the match is unique (distance ≤ 2) | F10 |
| MDL503 | `masquerade-function-prefix` | warning | the value starts with a known name followed by something other than a space or the end (`constantly`, `random_intx`): selection by prefix is surprising | — | F10 |
| MDL504 | `masquerade-syntax` | error | invalid arguments or modifiers for the function: the dump aborts | — | F11 |
| MDL505 | `malformed-masquerade-key` | error | key starting with `` ` `` without a closing `` ` ``: ignored | unsafe: add the closing backtick when unambiguous | F9 |
| MDL506 | `sql-unbalanced` | error | SQL-valued keys (`columns_on_select`, `columns_on_insert`, `where`, `columns_on_select_replace*`): unbalanced parentheses, quotes or `/* */` comments | — | — |
| MDL507 | `columns-count-mismatch` | warning | different numbers of top-level columns in `columns_on_select` and `columns_on_insert` of the same section | — | — |
| MDL508 | `masquerade-file-missing` | warning, opt-in | `<file X>` not found relative to `--base-dir` | — | F11 |
| MDL509 | `table-group-not-dumped` | info, opt-in | table section whose `db.table` does not match the `regex` of `[mydumper]` in the same file or load set (§9.3) | — | — |

#### MDL6xx — Connection (libmysqlclient) — NEW

Each rule ships only after an e2e scenario proves its consequence (§11.6). A rule whose
hypothesis turns out false is dropped.

| ID | Name | Severity | Trigger | Fix | Ref. |
|---|---|---|---|---|---|
| MDL601 | `connection-keys-ignored` | error | `host`, `user` or `password` in a tool group of a file that also has `[client]`: read by nobody | unsafe: move them to `[client]` | F3, F13, C1 |
| MDL602 | `mysql-include-directive` | **AMENDED.** error in option groups; warning elsewhere | `!include` or `!includedir`: a MySQL directive. In option groups mydumper sees an unknown option (F6); elsewhere only libmysqlclient follows it, so [mydumper] or table sections of the included file are ignored | — | C3, F6 |
| MDL603 | `client-group-overridden` | warning | cross-file (load set, §9.3): the extra file becomes the client defaults file, so the defaults file's `[client]` is ignored | — | F14, C2 |

#### MDL9xx — Project conventions

All opt-in, driven by the `conventions` config section; severity `error` once enabled.
They generalize real needs: keeping the file name, `outputdir`, `regex` and table sections
consistent so that a copy-pasted file does not overwrite another dump.

| ID | Name | Trigger |
|---|---|---|
| MDL901 | `filename-pattern` | the file name does not match `conventions.filename-pattern` (Go regex with named groups, e.g. `name`, `schema`) |
| MDL902 | `value-template` | a value does not match its template (`{name}`, `{schema}` substituted from the named groups, then compiled) |
| MDL903 | `table-schema-template` | the `db` of a table section differs from the template |
| MDL904 | `required-key` | a required key is missing from a group |
| MDL905 | `forbidden-key` | a forbidden key or group is present |

### 6.4 Suppressions

GKeyFile has no end-of-line comments (K10), so suppressions sit on their own line:

- `# mydumper-lint: disable-next-line=MDL302,MDL303`
- `# mydumper-lint: disable-file=MDL401` (anywhere in the file)

IDs or names are required; a directive without them is MDL002. MDL1xx cannot be
suppressed by comments, only by the config or `--ignore`, with a warning. An unused
suppression is MDL001. Directives are comments containing `=` and no `[`, so they never
alter pre-processing.

---

## 7. Fixer

### 7.1 Fix classes

- **safe:** preserves the recovered effective model (§7.3) and never makes the file's
  health worse.
- **unsafe:** changes the effective model (trimming a value, renaming a group, deleting a
  duplicate, commenting a line out). Applied only with `--unsafe-fixes`, or when the rule
  is listed in `fix.extend-safe`.

Applicability is decided per rule; the invariant of §7.3 is the mechanical check every
safe fix must pass. A fix can satisfy the invariant and still be classified unsafe when it
discards something the author wrote that might be what they meant, such as the first
value of a duplicate key (MDL301).

### 7.2 Recovery rules

Each rejection cause has a recovery rule. The recovered reading (§4.5) uses it, and the
cause's safe fix produces exactly the same result:

| Cause | Recovered as | Safe fix |
|---|---|---|
| BOM | first line without the BOM | remove the BOM |
| whitespace-only line | empty line | empty the line |
| `\r` line | empty line | `\r\n` → `\n` |
| leak: empty line → `= 1` | empty line | delete the empty line |
| leak: flag without `= 1`, or flag on the last line without `\n` | `key=1` | `key` → `key=1`, or append `\n` |
| text after `]` that is a `#` comment without `[` | header line plus a comment line | move the comment above the header |
| any other cause (MDL105, 107, 109, 110, 111, other MDL106 cases) | line dropped; entries under an invalid header belong to no group | none |

Entries that make mydumper abort at startup (an unknown option in a strict version, an
invalid number, G8) are left out of the recovered model. That is why turning `; comment`
into `# comment` (MDL304) is safe.

### 7.3 Invariants and self-check

Health is ordered: `rejected` < `fatal-at-startup` < `ok`. Let R(x) be the recovered
effective model of file x. Before writing, the fixer checks:

- with safe fixes only: R(after) = R(before) and health(after) ≥ health(before);
- with unsafe fixes: health(after) ≥ health(before).

A violation means a fixer bug: nothing is written, and the command exits 2 with the model
diff. For a loadable file, R is its real effective model, so "safe" means "mydumper
behaves exactly the same". For a rejected file, it means "mydumper now applies exactly
what the author wrote".

**AMENDED (health guard).** Unsafe fixes can also interact with GOption: renaming
[MyDumper] makes mydumper read the group, whose invalid key then aborts startup; two
renames that are harmless alone can be fatal together, by un-swallowing a key (G8). Each
pass is therefore guarded: when its fixes, applied together, would lower the health, the
pass is replayed one fix at a time and each fix that lowers it is dropped for good and
reported (`not applied: … which would make mydumper fail`). The self-check stays as the
last line of defense.

The check exists because fixes interact with the pre-processor. For example, moving the
trailing comment of `[mydumper] # see [docs]` above the header would create a leak
origin (P1), and the empty line after the header would then break the file. MDL106 does
not offer that fix, and the self-check would catch it if it did.

### 7.4 Mechanics

1. Edits are byte ranges on the original file. Two edits conflict when their ranges
   overlap or touch. The lower rule ID wins; the other waits for the next pass.
2. Fixpoint: lint and fix again until stable, at most 10 passes. Beyond that: internal
   error, exit 2.
3. Idempotence: `fix(fix(x)) == fix(x)`, tested on the whole corpus.
4. A fix never inserts a line that is a leak origin (P1). Inserted lines use the
   terminator of the line they attach to.
5. Atomic write: temporary file in the same directory, `fsync`, `rename`. Mode and owner
   preserved when permitted. Symlinks resolved: the target is written.
6. Nothing outside the fix ranges changes: comments, order and empty lines stay intact
   unless a rule explicitly targets them.
7. `--diff` prints exactly what `--fix` would write, as a unified diff, and exits 1 if
   the diff is not empty.
8. With stdin, `--fix` writes the result to stdout.

---

## 8. CLI

### 8.1 Commands

```text
mydumper-lint check [flags] [PATH ...]           default command
mydumper-lint rules [--format text|json]         list rules (stable, preview, opt-in)
mydumper-lint explain <ID|name>                  full documentation of a rule
mydumper-lint inspect <FILE>                     what mydumper sees (effective model)
mydumper-lint versions                           embedded mydumper versions, default target
mydumper-lint config {path|show|schema} [FILE]   resolved configuration, JSON Schema
mydumper-lint completion {bash|zsh|fish|powershell}
mydumper-lint version
```

`PATH` is a file, a directory (recursive, filtered by `include`, default `**/*.cnf`), or
`-` for stdin (with `--stdin-filename`). Flags may come before or after paths
(`mydumper-lint check . --fix` works); `--` ends flag parsing.

### 8.2 `check` flags

| Flag | Default | Effect |
|---|---|---|
| `--fix` | off | apply safe fixes |
| `--unsafe-fixes` | off | include unsafe fixes (with `--fix` or `--diff`) |
| `--diff` | off | print fixes as a unified diff, write nothing; exit 1 if there are any |
| `--format NAME[=PATH]` | `text` | repeatable: `text`, `concise`, `json`, `sarif`, `github`, `junit`, `gitlab`. `=PATH` writes to a file: `--format github --format sarif=results.sarif` |
| `--statistics` | off | counts per rule instead of individual diagnostics |
| `--mydumper-version` | config, else latest stable | §5.6 |
| `--select`, `--extend-select`, `--ignore` | every stable, non-opt-in rule | IDs, names or prefixes (`MDL1`) |
| `--preview` | off | enable preview rules |
| `--fail-on` | `error` | `error`, `warning`, `info` or `none` |
| `--config`, `--no-config` | discovery | explicit config file; disable discovery |
| `--exclude` | — | additional globs |
| `--stdin-filename` | — | name used in messages and for config matching |
| `--base-dir` | working directory | base for MDL508 |
| `--color` | `auto` | `auto`, `always`, `never`; honors `NO_COLOR` and `FORCE_COLOR` |
| `--quiet` | off | diagnostics only, no summary |

Exit codes:

- `0`: nothing at or above `--fail-on` (after fixing, with `--fix`);
- `1`: diagnostics at or above the threshold, or `--diff` with changes;
- `2`: usage, config or I/O error, or a failed fixer self-check.

### 8.3 `inspect`

Prints (JSON or text): `loadable`, the GLib error, the pre-processed lines (insertions and
leaks highlighted), groups and entries, and for each entry `effective` with its reason
(§4.5), plus the resolved version and build. Its core fields match the oracle's `--json`
output, which drives differential testing (§11.4).

---

## 9. Configuration

### 9.1 File and discovery

`.mydumper-lint.yaml` (or `.yml`) is searched upward from each linted file, up to the
repository root (the directory containing `.git`) or `$HOME`; the nearest file wins.
Decoding is strict: an unknown key is an error (exit 2) with a suggestion. A JSON Schema
generated from the Go definition is published (`schemas/config.v1.json`, and
`mydumper-lint config schema`) for editor completion.

### 9.2 Schema

```yaml
mydumper-version: "0.19.3-3"
mydumper-build: {client: mysql, ssl: true}
include: ["**/*.cnf"]
exclude: ["**/*.deleteme"]
fail-on: error
preview: false

rules:
  select: ["ALL"]            # ALL = every stable, non-opt-in rule
  extend-select: [MDL509]    # enable opt-in rules
  ignore: [MDL310]
  severity:
    MDL308: off
    MDL303: error

fix:
  extend-safe: [MDL302]      # treat this unsafe fix as safe

conventions:                 # enables MDL901–905
  filename-pattern: '^(?P<schema>[a-z_]+)-extra-file\.cnf$'
  values:
    mydumper.outputdir: '/{schema}$'
    mydumper.regex: '^\^\({schema}\\\.'
  table-schema: '{schema}'
  required:
    mydumper: [outputdir, logfile]
  forbidden:
    "*": [password]
    groups: [client]

load-sets:                   # cross-file analysis (§9.3)
  - defaults-file: defaults-file.cnf
    extra-files: ["*-extra-file.cnf"]

overrides:
  - files: ["legacy/*.cnf"]
    mydumper-version: "0.19.3-3"
    conventions: off
```

### 9.3 Load sets (cross-file analysis)

One mydumper invocation loads a defaults file and, optionally, an extra file. A load set
declares such pairs so that rules can analyze what the invocation actually sees: MDL603
(client group overridden), MDL509 (table sections against the `regex` set in the defaults
file), and `inspect --load-set` for the merged model. Without load sets, every file is
linted on its own.

---

## 10. Output formats

- **text:** `path:line:col: severity MDL102[whitespace-only-line] message`, then the
  consequence, a source excerpt with a caret, related locations, and fix availability
  (`(fixable)` or `(unsafe fix)`). Ends with a summary: `N errors, M warnings, K fixable`.
- **concise:** the first line only.
- **json:** `{"version":1,"files":[{"path","loadable","mydumper_version","diagnostics":[{"id","name","severity","message","consequence","range":{"start":{"line","col","offset"},"end":{…}},"related":[…],"fix":{"applicability","description","edits":[…]}}]}],"summary":{…}}`.
  Documented by `schemas/output.v1.json`. Changes within version 1 are additive only.
- **sarif:** SARIF 2.1.0. `tool.driver.rules` comes from rule metadata (`helpUri` points to
  `docs/rules/…`). Results carry `relatedLocations`, `fixes`, and `partialFingerprints`,
  so that GitHub code scanning keeps alerts stable across commits.
- **github:** workflow commands such as
  `::error file=…,line=…,col=…,endLine=…,endColumn=…,title=MDL102 whitespace-only-line::message`,
  escaped according to GitHub's rules.
- **junit:** one test suite per file, one failing test case per diagnostic. For CircleCI
  `store_test_results`, Jenkins, GitLab.
- **gitlab:** Code Quality report (fingerprint, severity mapping, `location.lines.begin`).

---

## 11. Testing strategy

The linter is the last line of defense, so every layer has its own independent oracle.

### 11.1 Unit tests

Table-driven, per package. Every GLib-visible branch of the emulators has a named test.

### 11.2 Oracle conformance

`testdata/oracle-cases/` holds byte-exact inputs; `.gitattributes` marks them `-text` so
git never rewrites their line endings. `preprocess` + `keyfile` must reproduce
`expected.txt` exactly: verdict, error message, groups, keys, values. M1 adds cases 50–69
(Appendix A). The oracle gains three modes:

- `--json`: the same core schema as `inspect`;
- `--serve`: length-prefixed requests on stdin, for fuzzing throughput;
- a data-driven `--goption` mode that reads its cases from `testdata/goption-cases/`.

### 11.3 Golden rule tests (txtar)

One file per case in `internal/rules/testdata/<ID>/`, in Go's txtar format:

```text
# MDL102: whitespace-only line at the end of the file, no final newline (latent).
-- input.cnf.esc --
[mydumper]\nroutines=1\n\n␠␠
-- config.yaml --
mydumper-version: v0.19.3-3
-- diagnostics --
input.cnf:4:1: error MDL102[whitespace-only-line] …
-- fixed.cnf.esc --
[mydumper]\nroutines=1\n\n
```

(`␠` stands for a space here.) Sections whose name ends in `.esc` hold a single line of Go
string escapes. They are used whenever bytes matter: CR, NUL, BOM, a missing final
newline, trailing spaces. Other sections are literal. Expected sections are regenerated
with `go test ./... -update` and reviewed in the diff. Every rule has at least one positive
and one negative case; fixable rules also have their safe and unsafe outputs.

### 11.4 Differential fuzzing against the oracle

- Native Go fuzzing (`go test -fuzz`), build tag `oracle`. `FuzzLoad` sends each input to
  the oracle (`--serve`) and compares the verdict, the error and the model with the Go
  emulation. A structured fuzzer builds files from grammar tokens (headers, flags,
  key/value lines, comments, empty and whitespace-only lines, CRLF, BOM, NUL, `\v`, `[`,
  `=`, `;`, backticks) to reach deep states quickly. `FuzzGOption` does the same for argv
  semantics.
- Seed corpus: every oracle case and golden input. Minimized divergences are committed to
  `testdata/fuzz/`.
- GLib matrix: 2.68.4 (AlmaLinux 9, the official images), 2.80.0 (Ubuntu noble), and the
  newest GLib (a rolling distribution image, pinned by digest and bumped by Dependabot).
- Budget: 60 s per fuzzer on pull requests; 10 min per fuzzer and GLib version nightly.
- Non-differential fuzzers: `FuzzFix` (no panic, idempotence, the invariants of §7.3) and
  `FuzzCheck` (no panic, deterministic output).

### 11.5 Fixer properties

On the whole corpus (oracle cases, golden inputs, fuzz corpus): idempotence, the
invariants of §7.3, a fixpoint within 10 passes, and `--diff` equal to what `--fix`
writes.

### 11.6 End-to-end suite (Docker)

Proves every runtime consequence a rule claims, against the real binaries. Build tag
`e2e`; the Go test drives `docker compose` and `docker run` through the CLI.

- **Infrastructure:** `e2e/compose.yaml` runs MySQL 8.4 seeded with a small deterministic
  schema: `app.users(id, email, name)`, `app.orders`, a view, a trigger, a routine, an
  event. mydumper and myloader run from the official image of each version, pinned by
  digest in `e2e/images.lock`.
- **Image guard:** before use, `mydumper --version` must match the tag (V3). A mismatch
  fails loudly and is recorded; it is never silently accepted.
- **Scenarios:** txtar files with the cnf, extra CLI arguments, the expected lint
  diagnostics, and runtime expectations:

  ```text
  -- scenario.yaml --
  tool: mydumper
  versions: ">= v0.19.1-1"
  lint: [MDL502]
  expect:
    config-loaded: true
    columns: {app.users.email: plaintext}
  -- defaults.cnf --
  [mydumper]
  …
  ```

- **Observations:** whether the config loaded (GLib's warning on stderr), fatal startup
  (exit code and message), option effects through artifacts (routines in the schema
  files, data files present or not, …), masking (data files compared with the seeded
  values), myloader effects after restoring into a fresh schema.
- **Consistency:** the harness also lints each scenario's cnf for that version and asserts
  that the diagnostics equal `lint:`. A rule whose metadata declares a runtime consequence
  must be referenced by at least one scenario (meta-test, §11.8).
- **Matrix:** pull requests touching rules, model, goption or optionsdb run the latest
  stable release of each branch (4 versions). Nightly runs and upstream-sync pull requests
  run every embedded version.

### 11.7 Upstream integration

The upstream `mydumper.cnf` and `myloader.cnf` of every embedded tag, downloaded at test
time and never vendored (they are GPL), must lint with 0 errors against their own
version. Build tag `integration`.

### 11.8 Meta-tests

Rule registry invariants: unique IDs and names; complete metadata; documentation examples
present and generated docs up to date; positive and negative golden cases; fixed outputs
for fixable rules; e2e scenario references for runtime consequences; a total mapping from
rejection causes to MDL1xx rules.

### 11.9 Quality gates

- **Coverage:** ≥ 95 % of statements on `preprocess`, `keyfile`, `goption`, `model` and
  `fix`; ≥ 90 % on `rules`; ≥ 85 % overall, "overall" being the product (`internal/`
  except `oracletest`, which only runs with an oracle). Enforced in CI without external
  services (`tools/covercheck`, `make cover-check`).
- **Mutation testing** (gremlins), nightly on the core packages. The score is tracked from
  M2 and becomes a blocking threshold at v0.1.0.
- **Static analysis:** `go vet`; golangci-lint v2 with, among others, `exhaustive` (every
  switch over causes, kinds and reasons stays complete), `gosec`, `errorlint`, `revive`,
  `gocritic`, `nolintlint`; `govulncheck`.
- **Race detector** on every unit test.
- **Benchmarks:** `BenchmarkCheck` on a generated corpus (1,000 files × 100 KB). Pull
  requests report the delta with `benchstat` (non-blocking).

---

## 12. Project infrastructure

### 12.1 Local workflow

A `Makefile` wraps everything. Tools run from pinned Docker images, so contributors only
need Go and Docker: `make build`, `test`, `lint`, `fuzz`, `oracle`, `e2e`, `gen`, `docs`,
`release-snapshot`.

### 12.2 CI (GitHub Actions)

| Workflow | Trigger | Content |
|---|---|---|
| `ci.yml` | push, pull request | build (Linux, macOS, Windows); unit and golden tests with `-race`; coverage gates; golangci-lint; govulncheck; generated files up to date; 60 s fuzz smoke; oracle conformance on GLib 2.68 |
| `oracle.yml` | pull requests touching the emulators, the oracle or testdata | conformance and 60 s differential fuzzing on the three GLib versions |
| `e2e.yml` | pull requests touching rules, model, goption, optionsdb or e2e | e2e on the 4 sentinel versions |
| `nightly.yml` | schedule | long fuzzing, mutation testing, full e2e matrix, upstream integration |
| `upstream-sync.yml` | daily schedule | new mydumper tags ⇒ generator, cross-check, e2e ⇒ pull request |
| `release.yml` | tag | goreleaser, signatures, attestations, Docker image |
| `scorecard.yml`, `codeql.yml` | schedule, pull request | OpenSSF Scorecard, CodeQL |

Hardening: actions pinned by commit SHA; `permissions: {}` by default, with least
privilege per job; `persist-credentials: false`; Dependabot for Go modules, actions and
Docker base images.

### 12.3 Releases

- release-please turns conventional commits into a release pull request (version bump and
  `CHANGELOG.md`); merging it tags the release.
- goreleaser: Linux, macOS and Windows × amd64 and arm64; `CGO_ENABLED=0`, `-trimpath`,
  reproducible timestamps; checksums; SBOM (SPDX); cosign keyless signatures; GitHub
  artifact attestations, verifiable with `gh attestation verify`.
- Docker: `ghcr.io/tomsihap/mydumper-lint`, `FROM scratch`, non-root, multi-arch, signed.

### 12.4 Distribution

- GitHub Releases.
- `go install github.com/tomsihap/mydumper-lint/cmd/mydumper-lint@latest`.
- The Docker image.
- pre-commit hooks `mydumper-lint` and `mydumper-lint-fix` (`language: golang`, plus a
  Docker variant).
- A composite GitHub Action: downloads the release binary, verifies its checksum and
  attestation, runs `check --format github`, optionally uploads SARIF.
- A Homebrew tap (optional; needs a `homebrew-tap` repository).

### 12.5 Documentation

- README: the problem in five lines, installation, quick start, CI examples (GitHub
  Actions; CircleCI with JUnit; GitLab with Code Quality), the generated rule table, and
  a FAQ ("why not editorconfig?": because of MDL108, MDL4xx, MDL5xx).
- `docs/rules/MDLxxx.md`, generated.
- Configuration and output references generated from the schemas; a supported-versions
  table generated from the knowledge base.
- CONTRIBUTING, with "add a rule" and "add a mydumper version" guides.

### 12.6 Governance and hygiene

- LICENSE (Apache-2.0), `tools/oracle/LICENSE` (GPL-3.0-or-later), NOTICE (third-party
  notices), CODE_OF_CONDUCT (Contributor Covenant 2.1), SECURITY.md (GitHub private
  vulnerability reporting).
- Issue templates: bug report (asks for the mydumper version and the `inspect` output),
  false positive, rule proposal. Pull request template. CODEOWNERS. `.editorconfig`.
  `.gitattributes`.
- The French draft spec stays local (`.git/info/exclude`) because it contains internal
  context. Before the repository goes public: employer clearance (D2), and a history check
  confirming that no internal material was ever committed.

---

## 13. Versioning and compatibility

- SemVer covers the CLI, the JSON output schema, the config schema and rule IDs.
- Before 1.0, minor releases may change defaults; the changelog always says so.
- New rules land in preview and become stable in a later minor release.
- Adding mydumper versions to the knowledge base is a minor release. It can change results
  for users who do not pin `mydumper-version`, because the default target moves to the new
  latest stable release. The README recommends pinning.
- Rule IDs are never reused; a removed rule keeps a documentation page saying when and
  why it was removed.
- JSON output `version: 1` evolves additively only. A breaking change introduces
  `version: 2` with an overlap period.

---

## 14. Milestones

Each milestone gets its own implementation plan, written once the previous milestone has
been reviewed, and ends with a review checkpoint.

**M0 — Skeleton.** Go module, Makefile, CI (build, test, lint, govulncheck), governance
files, `.gitattributes`, oracle Docker images for the three GLib versions with `--json`
and `--serve`, `mydumper-lint version`, this document committed.
*Done when:* CI is green on the three operating systems, and the oracle images reproduce
`expected.txt` on the three GLib versions.

**M1 — Emulation core and loadability (usable in CI).** `source`; `preprocess` (real and
ideal, leak origins); `keyfile` (both readings, causes); oracle cases 50–69; MDL101–MDL112,
MDL306, MDL309, MDL310, MDL312; the safe fixer with self-check, atomic write and `--diff`;
formats text, concise, json and github; `inspect`; `check` with a minimal config (select,
ignore, severity, include, exclude); differential fuzzing.
*Done when:* 69/69 oracle cases conform; a 10-minute differential fuzzing run per GLib
version finds no divergence; the incident reproduction works end to end (a
whitespace-only last line without a final newline reports MDL102 and MDL104, and `--fix`
makes the file safe to append to; after a block is appended, the file reports MDL102 and
`--fix` makes it loadable); idempotence and invariants hold; coverage gates are met.

**M2 — Knowledge base, GOption, options.** The generator and the 28 embeddable versions; the
cross-check against the images; `goption` and the GOption oracle cases; `model`; MDL4xx;
the e2e harness with its first scenarios (config ignored: F1 and F12; boolean flags: G1;
octal: G7; unknown options per version: F6); the `versions`, `rules` and `explain`
commands; documentation generation.
*Done when:* the generator is reproducible from scratch; the cross-check passes on every
version; e2e passes on the 4 sentinel versions; every MDL4xx consequence is proven by a
scenario.

**M3 — Groups, values, tables, masking.** MDL2xx; the remaining MDL3xx (301–305, 307, 308,
311, 313); `sqlscan`; the `masquerade` grammar per version; MDL5xx (except opt-in rules);
suppressions (MDL001, MDL002); unsafe fixes; formats sarif, junit and gitlab; the full
config (overrides, `extend-safe`, preview); the JSON Schemas.
*Done when:* e2e proves every MDL5xx consequence (plaintext column, abort on syntax
error); the upstream example files of every tag lint with 0 errors; every rule has
generated documentation.

**M4 — Connection, conventions, cross-file.** MDL6xx, each after its e2e proof; load sets;
MDL9xx; MDL508 and MDL509.
*Done when:* e2e scenarios settle C1–C3; conventions are documented with examples.

**M5 — Release v0.1.0.** goreleaser, signatures, attestations, SBOM, Docker image,
pre-commit hooks, GitHub Action, README, CONTRIBUTING, upstream-sync workflow,
release-please, mutation-testing threshold.
*Done when:* the v0.1.0 release dry run passes, and the Action and pre-commit hooks work
against the dry-run artifacts.

**M6 — After v0.1 (optional).** LSP server (`mydumper-lint server`: diagnostics and code
actions for VS Code, Neovim, and JetBrains IDEs through LSP4IJ); a WASM playground on
GitHub Pages; an upstream issue or pull request for mydumper describing the facts of §3
(F1, P1, G1, G7–G9, F10) and proposing a `--strict-config` option.

---

## 15. Open questions

1. Employer clearance before the repository goes public (D2).
2. Homebrew tap: create `tomsihap/homebrew-tap`? (M5)
3. A documentation website (GitHub Pages) beyond the generated Markdown? (after v0.1)

---

## Appendix A — Design probes (oracle cases 50–69)

Bytes are Go string literals. Output of the oracle with GLib 2.88.3, identical with 2.68.4
and 2.80.0, with `LANG` unset unless stated otherwise. `U+FFFD` is how GLib prints the
NUL byte in its error message.

| # | Name | Bytes | Oracle |
|---|---|---|---|
| 50 | `key_close_bracket` | `"[mydumper]\nfoo]=1\n"` | `ERROR: Invalid key name: foo]` |
| 51 | `key_bad_locale` | `"[mydumper]\nfoo[bar baz]=1\n"` | `ERROR: Invalid key name: foo[bar baz]` |
| 52 | `group_open_bracket` | `"[a[b]\nk=1\n"` | `ERROR: Invalid group name: a[b` |
| 53 | `group_tab` | `"[my\tdumper]\nk=1\n"` | `ERROR: Invalid group name: my<TAB>dumper` |
| 54 | `locale_c_visible` | `"[mydumper]\nthreads[C]=4\n"` | `OK`: key `threads[C]` = `4` |
| 55 | `locale_fr_visible` (`LANG=fr_FR.UTF-8`) | `"[mydumper]\nthreads[fr]=4\n"` | `OK`: key `threads[fr]` = `4` |
| 56 | `nul_in_key` | `"[mydumper]\nrout\x00ines=1\n"` | `ERROR: Key file contains line “rout<U+FFFD>ines=1” which is not a key-value pair, group, or comment` |
| 57 | `nul_in_value` | `"[mydumper]\nwhere=a\x00b\n"` | `OK`: `where` = `a` |
| 58 | `leak_through_group` | `"[mydumper]\n# see [docs]\n[myloader]\n\nk=1\n"` | `ERROR: Key file contains line “= 1” …` |
| 59 | `include_directive` | `"[mydumper]\n!include /etc/x.cnf\n"` | `OK`: key `!include /etc/x.cnf` = `1` |
| 60 | `vtab_indent` | `"[mydumper]\n\vroutines=1\n"` | `OK`: key `\vroutines` = `1` |
| 61 | `masked_key_bracket` | ``"[`db`.`t`]\n`c[0]`=constant x\n"`` | ``ERROR: Invalid key name: `c[0]` `` |
| 62 | `regex_class_then_blank` | ``"[mydumper]\nregex=^(a\|b)\\.[a-z]+$\n\n[`db`.`t`]\n`c`=constant x\n"`` | `OK`: 2 groups |
| 63 | `dashdash_key` | `"[mydumper]\n--threads=4\n"` | `OK`: key `--threads` = `4` |
| 64 | `lone_bracket` | `"[mydumper]\nk=v\n[\n"` | `ERROR: Key file contains line “[” …` |
| 65 | `quoted_values` | `"[mydumper]\noutputdir=\"/backup\"\nregex='^db\\.'\n"` | `OK`: values `"/backup"` and `'^db\.'`, quotes kept |
| 66 | `nul_line_start` | `"[mydumper]\n\x00routines=1\nevents=1\n"` | `OK`: only `events` = `1` |
| 67 | `nul_in_group` | `"[my\x00dumper]\nk=1\n"` | `ERROR: Key file contains line “[my<U+FFFD>dumper]” …` |
| 68 | `del_in_group` | `"[my\x7fdumper]\nk=1\n"` | `ERROR: Invalid group name: my<DEL>dumper` |
| 69 | `locale_charset` | `"[mydumper]\nthreads[fr_FR.UTF-8@euro]=4\nk[é]=1\n"` | `OK`: 0 visible keys (C locale) |

## Appendix B — GOption probes

argv is built like mydumper does (`[group, --key, value, …]`). Options: `routines` (NONE),
`threads` (INT, short `t`), `outputdir` (FILENAME), `compress` (CALLBACK, OPTIONAL_ARG),
`nodata` (NONE, REVERSE). *strict* = without `set_ignore_unknown_options` (v0.19.3-3);
*lenient* = with it (v1.0.8-1, master). GLib 2.88.3.

| # | argv after the group | Result |
|---|---|---|
| 01–10 | `--threads V` with V = `10`, `010`, `08`, `0x10`, `+5`, `-1`, `1e3`, `10␠`, empty, `99999999999` | 10, 8, error, 16, 5, -1, error, error, error, out of range. An INT64 option gave the same results for 01–09. |
| 11 | `--routines -t --threads 4` | fatal in both modes: `Cannot parse integer value “--threads” for -t` |
| 12 | `--routines -- --threads 4` | ok in both modes; `routines` set, `threads` **not** set |
| 13 | `--compress -x --threads 4` | `compress` gets no value; lenient: `-x` left over, `threads`=4; strict: fatal `Unknown option -x` |
| 14 | `--compress "" --threads 4` | `compress` = `""`; `threads`=4 |
| 15 | `--outputdir -x --threads 4` | `outputdir` = `-x`; `threads`=4 |
| 16 | `--outputdir a --outputdir b` | `outputdir` = `b` |
| 17 | `--nodata 1` (REVERSE) | data disabled; `1` left over |
| 18 | `--threads 4 --bogus 1` | lenient: ok, `--bogus 1` left over; strict: fatal `Unknown option --bogus` |

## Appendix C — Changes from the draft spec

1. **Reference facts.** Amended K2, K4, K14, K17, F10. Added K19, K20, G7–G17, F12–F16,
   C1–C5, V1–V6. F5 confirmed in the source.
2. **Rules.** New: MDL002, MDL110, MDL111, MDL112, MDL313, MDL407, MDL408, MDL601–MDL603.
   Amended: MDL106, MDL108, MDL305, MDL312, MDL404.
3. **Versions.** Every release ≥ v0.19.1-1 instead of three versions (two of which were
   pre-releases, with the 0.20 branch missing). One consolidated history file instead of
   one file per version.
4. **Cross-check.** `--help` instead of `--help-all`, which does not exist (V4); a version
   guard on images (V3); fingerprints of the loader code, so upstream changes to the
   pre-processor cannot go unnoticed.
5. **Dependencies.** `go.yaml.in/yaml/v3` instead of the archived `gopkg.in/yaml.v3`.
6. **Fixer.** One invariant on the recovered model replaces the two-case definition of
   "safe"; edits that touch conflict, not only edits that overlap.
7. **Additions.** Formats `concise`, `junit`, `gitlab`; repeatable `--format NAME=PATH`;
   flags accepted after paths; `versions`, `config` and `completion` commands; preview
   rules; load sets; the Docker end-to-end suite; mutation testing; supply-chain
   hardening; the LSP roadmap.
8. **Milestones.** Re-cut into M0–M6; differential fuzzing moves to M1, where the
   emulators are built.
