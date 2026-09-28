# mydumper-lint

A linter and fixer for [mydumper](https://github.com/mydumper/mydumper) and myloader
configuration files (`--defaults-file`, `--defaults-extra-file`).

**The problem.** mydumper reads its `.cnf` files with GLib, after rewriting them with
a small pre-processor of its own. A single line of spaces, one empty line with a
Windows line ending, or a `[` in a comment followed by an empty line makes GLib reject
the **whole file**. mydumper then logs one warning, runs with its defaults and exits 0.
Your `[client]` section still works, so the backup looks healthy, while your masking
rules, filters and output directory were never applied.

mydumper-lint finds these problems before they reach production, explains what
mydumper will actually do, and fixes what can be fixed safely. It does not guess: it
reproduces GLib's parser, mydumper's pre-processor and GLib's option parser, checked
against real GLib and the official mydumper images, for every mydumper release since
v0.19.1-1.

```console
$ mydumper-lint check mydumper.cnf
mydumper.cnf:8:1: error MDL102[whitespace-only-line] line contains only whitespace: mydumper's pre-processor turns it into `  = 1`, a key/value pair with an empty key (GLib: Key file contains line “  = 1” which is not a key-value pair, group, or comment)
  = impact: mydumper ignores this entire file: it only logs a WARNING ("Failed to load config file") and runs with its defaults, exit code 0. The [client] section is still read by the MySQL client library, so the connection works and the run looks normal. The masking rules are NOT applied: masked columns are dumped in plaintext.
  |
8 |
  | ^^
  |
  = help: fixable with --fix: Remove the whitespace

mydumper.cnf:9:10: error MDL402[boolean-flag-value] `routines=0` turns routines on: the option takes no value, so GLib ignores `0`
  = impact: routines takes effect, the opposite of what `0` says.
  |
9 | routines=0
  |          ^
  |
  = help: fixable with --fix --unsafe-fixes: Comment the line out to leave the option off

Found 2 errors, 0 warnings, 0 infos in 1 file (1 fixable with --fix, 1 more with --unsafe-fixes).
```

**Try it without installing anything:** the
[playground](https://tomsihap.github.io/mydumper-lint/) runs mydumper-lint in your
browser (WebAssembly). Paste or open a file, pick your mydumper version, and see the
problems, the fixes and what mydumper really reads. The file never leaves your
browser.

**In your editor:** `mydumper-lint server` is a language server. Problems appear as you
type, with quick fixes and each rule's explanation on hover, in VS Code (extension in
[`editors/vscode`](editors/vscode)), Neovim, Helix, Emacs and JetBrains IDEs: see
[docs/editors.md](docs/editors.md).

## Contents

- [Install](#install)
- [Quick start](#quick-start)
- [Target your mydumper version](#target-your-mydumper-version)
- [Fixing](#fixing)
- [Configuration](#configuration)
- [Continuous integration](#continuous-integration)
- [Rules](#rules)
- [How it works, and why you can trust it](#how-it-works-and-why-you-can-trust-it)
- [FAQ](#faq)
- [Contributing](#contributing)
- [License](#license)

## Install

With Go 1.26 or later:

```sh
go install github.com/tomsihap/mydumper-lint/cmd/mydumper-lint@latest
```

Prebuilt binaries for Linux, macOS and Windows (amd64, arm64) are attached to each
[GitHub release](https://github.com/tomsihap/mydumper-lint/releases), with checksums,
signatures and SBOMs. A container image is published too:

```sh
docker run --rm -v "$PWD:/work" -w /work ghcr.io/tomsihap/mydumper-lint check .
```

mydumper-lint is a single static binary with no runtime dependency. It never connects
to a database and never runs mydumper.

## Quick start

```sh
mydumper-lint check .                  # lint every *.cnf below the current directory
mydumper-lint check conf/prod.cnf      # lint one file
mydumper-lint check --fix .            # apply the safe fixes
mydumper-lint check --diff .           # show the fixes as a diff, write nothing
mydumper-lint inspect conf/prod.cnf    # what mydumper actually sees in a file
mydumper-lint inspect --load-set conf/defaults.cnf conf/sales-extra.cnf  # and in a pair
mydumper-lint explain MDL102           # the full documentation of a rule
mydumper-lint rules                    # every rule
mydumper-lint versions                 # the mydumper versions mydumper-lint knows
```

`check` exits with 0 when nothing at or above `--fail-on` (default: `error`) is found,
1 when something is, and 2 on a usage or internal error.

`inspect` is the fastest way to understand a file: whether GLib loads it (and the
exact message mydumper logs if not), the lines the pre-processor rewrites, the groups
and keys GLib returns, and for every key whether it has an effect, and why not.
With `--load-set`, give it the file of `--defaults-file` and the file of
`--defaults-extra-file`: it shows what one run applies from both (mydumper merges the
extra file into the defaults file, so a rejected defaults file also loses the extra
file's masking) and which file the MySQL client library reads for the connection.

## Target your mydumper version

mydumper's behavior changes between releases: v0.19.1-x has no pre-processor at all,
versions up to v1.0.0-1 abort on an unknown option while later ones ignore it, and the
masking functions and their parser changed several times. Tell mydumper-lint which
version you run:

```sh
mydumper-lint check --mydumper-version 0.19.3-3 .
```

or, better, in `.mydumper-lint.yaml`:

```yaml
mydumper-version: "0.19.3-3"
```

Accepted forms: `v0.19.3-3`, `0.19.3-3`, `0.19.3` (latest build of 0.19.3), `0.19`
(latest stable 0.19.x), `latest` and `latest-prerelease`. Without a version,
mydumper-lint checks against the latest stable release and says so.

<!-- versions:begin -->
mydumper-lint knows 28 mydumper versions, from v0.19.1-1 to v1.0.8-1; the default target is v1.0.5-1, the latest stable release. See [docs/versions.md](docs/versions.md).
<!-- versions:end -->

## Fixing

- `--fix` applies **safe** fixes: they never change what mydumper applies from the
  file. For a file mydumper currently ignores, "safe" means that mydumper will now
  apply exactly what you wrote.
- `--unsafe-fixes` also applies fixes that change behavior on purpose, such as
  renaming `split_string_pk` to `split-string-pk` or commenting out `routines=0`.
  Review them.
- `--diff` prints the fixes as a unified diff and writes nothing: use it in CI.

Every fix is checked before anything is written: the fixer re-reads the result the
way mydumper does and refuses to write a file that would load worse than before, or,
with safe fixes only, a file whose effective configuration changed. Files are written
atomically, keeping their permissions.

## Configuration

mydumper-lint looks for `.mydumper-lint.yaml` (or `.yml`) from each file's directory up
to the repository root. Every key is optional:

```yaml
mydumper-version: "0.19.3-3"
mydumper-build: {client: mysql, ssl: true}   # the official images' build
include: ["**/*.cnf"]
exclude: ["legacy/**"]
fail-on: error                 # error, warning, info or none

rules:
  select: ["ALL"]              # IDs, names or prefixes such as MDL1
  extend-select: [MDL509]      # enable opt-in rules
  ignore: [MDL310]
  severity:
    MDL308: off
    MDL303: error

fix:
  extend-safe: [MDL302]        # apply this unsafe fix with --fix

load-sets:                     # files one mydumper run loads together (MDL509, MDL510, MDL603)
  - defaults-file: defaults.cnf
    extra-files: ["*-extra.cnf"]

conventions:                   # team rules, MDL901-MDL905
  filename-pattern: '^(?P<schema>[a-z_]+)-extra\.cnf$'
  values:
    mydumper.outputdir: '/{schema}$'
  table-schema: '{schema}'
  required:
    mydumper: [outputdir, logfile]
  forbidden:
    "*": [password]

overrides:
  - files: ["legacy/*.cnf"]
    mydumper-version: "0.19.1-3"
```

`mydumper-lint config schema` prints the JSON Schema of this file
([schemas/config.v1.json](schemas/config.v1.json)); `mydumper-lint config show FILE`
prints the settings that apply to a file.

To accept one occurrence of a problem, put a comment on the line above it:

```ini
# mydumper-lint: disable-next-line=MDL303
where=a > 1 # this comment is part of the value
```

`# mydumper-lint: disable-file=MDL401` anywhere in a file disables a rule for the whole
file. Problems that make mydumper ignore the whole file (MDL1xx) cannot be suppressed
by comments.

## Continuous integration

**GitHub Actions**, with annotations on the pull request:

```yaml
- uses: actions/checkout@v5
- uses: tomsihap/mydumper-lint@v0.1.0   # a release tag (or its commit SHA)
  with:
    args: check --format github .
```

The action installs the release matching its tag, and checks the archive against the
release's checksums and its build provenance attestation (`gh attestation verify`,
available on GitHub-hosted runners; `verify-attestation: false` skips it elsewhere).

To also see the problems in GitHub code scanning, write SARIF and upload it (the job
needs `security-events: write`):

```yaml
- uses: tomsihap/mydumper-lint@v0.1.0
  with:
    args: check --format github --format sarif=mydumper-lint.sarif .
- uses: github/codeql-action/upload-sarif@v3
  if: always()   # upload even when the check fails
  with:
    sarif_file: mydumper-lint.sarif
```

**GitLab CI**, with the Code Quality widget:

```yaml
mydumper-lint:
  image: golang:1.26-alpine    # GitLab runs scripts with a shell; the release image has none
  script:
    - go install github.com/tomsihap/mydumper-lint/cmd/mydumper-lint@v0.1.0
    - mydumper-lint check --format gitlab=gl-code-quality.json --format text .
  artifacts:
    reports: {codequality: gl-code-quality.json}
```

**CircleCI** (or anything that reads JUnit):

```yaml
- run: mydumper-lint check --format junit=results/mydumper-lint.xml --format text .
- store_test_results: {path: results}
```

**pre-commit**:

```yaml
repos:
  - repo: https://github.com/tomsihap/mydumper-lint
    rev: v0.1.0
    hooks:
      - id: mydumper-lint        # or mydumper-lint-fix
```

Output formats: `text` (default), `concise`, `json`, `sarif`, `github`, `junit`,
`gitlab`. `--format` is repeatable, and `NAME=PATH` writes a format to a file.

## Rules

Every diagnostic says what mydumper will do: ignore the file, abort at startup, dump a
column in plaintext… Each rule has a page with an example taken from its tests: see
[docs/rules](docs/rules/README.md), or run `mydumper-lint explain ID`.

<!-- rules:begin -->

**Suppressions**

| Rule | Name | Severity | Fix | Summary |
|---|---|---|---|---|
| [MDL001](docs/rules/MDL001.md) | `unused-suppression` | info | safe | A suppression comment suppresses nothing. |
| [MDL002](docs/rules/MDL002.md) | `invalid-suppression` | warning | — | A suppression comment is malformed, names an unknown rule, or targets MDL1xx. |

**Loading: mydumper ignores the whole file**

| Rule | Name | Severity | Fix | Summary |
|---|---|---|---|---|
| [MDL101](docs/rules/MDL101.md) | `utf8-bom` | error | safe | The file starts with a UTF-8 byte order mark. |
| [MDL102](docs/rules/MDL102.md) | `whitespace-only-line` | error | safe | A line contains only spaces or tabs. |
| [MDL103](docs/rules/MDL103.md) | `carriage-return` | error | safe | The file contains carriage returns (Windows CRLF line endings). |
| [MDL104](docs/rules/MDL104.md) | `missing-final-newline` | error | safe | The file does not end with a newline. |
| [MDL105](docs/rules/MDL105.md) | `key-before-group` | error | — | A key/value pair appears before the first [group] header. |
| [MDL106](docs/rules/MDL106.md) | `invalid-group-line` | error | safe | A line starting with `[` is not a valid group header. |
| [MDL107](docs/rules/MDL107.md) | `empty-key` | error | — | A line starts with `=`: the key is empty. |
| [MDL108](docs/rules/MDL108.md) | `bracket-state-leak` | error | safe | mydumper's pre-processor mis-handles a line because an earlier line contains `[`. |
| [MDL109](docs/rules/MDL109.md) | `unparsable-line` | error | — | GLib rejects a line for a reason no more specific rule describes. |
| [MDL110](docs/rules/MDL110.md) | `invalid-key-name` | error | — | A key contains `]`, or a `[` that is not a valid locale suffix. |
| [MDL111](docs/rules/MDL111.md) | `nul-byte` | error | — | A NUL byte makes a line unparsable. |
| [MDL112](docs/rules/MDL112.md) | `bracket-leak-risk` | warning | — | An empty line placed after this line would make mydumper ignore the file. |
| [MDL113](docs/rules/MDL113.md) | `unsupported-flag-without-value` | error | safe | A line without `=` in a mydumper version that has no pre-processor. |

**Groups**

| Rule | Name | Severity | Fix | Summary |
|---|---|---|---|---|
| [MDL201](docs/rules/MDL201.md) | `unknown-group` | warning | — | A group that neither mydumper, myloader nor the MySQL client library reads. |
| [MDL202](docs/rules/MDL202.md) | `group-name-case` | error | unsafe | A group name matches a known group only when case is ignored. |
| [MDL203](docs/rules/MDL203.md) | `group-name-whitespace` | error | unsafe | A group name starts or ends with spaces. |
| [MDL204](docs/rules/MDL204.md) | `malformed-table-group` | error | unsafe | A group looks like a table section but is not written ``[`db`.`table`]``. |
| [MDL205](docs/rules/MDL205.md) | `duplicate-group` | warning | unsafe | The same group is declared twice. |

**Lines, keys and values**

| Rule | Name | Severity | Fix | Summary |
|---|---|---|---|---|
| [MDL301](docs/rules/MDL301.md) | `duplicate-key` | warning | unsafe | The same key appears twice in a group. |
| [MDL302](docs/rules/MDL302.md) | `trailing-whitespace-in-value` | error | unsafe | A value ends with spaces or tabs, which GLib keeps. |
| [MDL303](docs/rules/MDL303.md) | `inline-comment` | warning | unsafe | A `#` comment after a value is part of the value. |
| [MDL304](docs/rules/MDL304.md) | `semicolon-comment` | error | safe | A line starting with `;` is a key, not a comment. |
| [MDL305](docs/rules/MDL305.md) | `localized-key` | warning | — | A key with a `[locale]` suffix depends on the runtime language. |
| [MDL306](docs/rules/MDL306.md) | `leading-whitespace` | info | safe | A key, comment or group header is indented. |
| [MDL307](docs/rules/MDL307.md) | `spaces-around-equals` | info | safe | Spaces around `=`. |
| [MDL308](docs/rules/MDL308.md) | `flag-without-value` | info | safe | An option written without `=`, such as `routines`. |
| [MDL309](docs/rules/MDL309.md) | `trailing-whitespace` | warning | safe | A comment or group header ends with spaces or tabs. |
| [MDL310](docs/rules/MDL310.md) | `blank-lines` | info | safe | Several consecutive empty lines, or empty lines at the end of the file. |
| [MDL311](docs/rules/MDL311.md) | `invalid-utf8` | warning | — | A line contains bytes that are not valid UTF-8. |
| [MDL312](docs/rules/MDL312.md) | `control-character` | warning | — | A line contains an invisible control character. |
| [MDL313](docs/rules/MDL313.md) | `quoted-value` | warning | unsafe | An option's value is wrapped in quotes, which mydumper keeps. |

**Options**

| Rule | Name | Severity | Fix | Summary |
|---|---|---|---|---|
| [MDL401](docs/rules/MDL401.md) | `unknown-option` | error | unsafe | A key of [mydumper] or [myloader] is not an option of the target version. |
| [MDL402](docs/rules/MDL402.md) | `boolean-flag-value` | error | unsafe | An option that takes no value is given one that looks like `false`. |
| [MDL403](docs/rules/MDL403.md) | `invalid-number` | error | — | A numeric option's value is not a number GLib accepts. |
| [MDL404](docs/rules/MDL404.md) | `empty-option-value` | warning | — | An option that expects a value is given an empty one. |
| [MDL405](docs/rules/MDL405.md) | `plaintext-password` | warning | — | The file holds a password in plain text. |
| [MDL406](docs/rules/MDL406.md) | `invalid-regex` | warning | — | A regular-expression option's value does not compile. |
| [MDL407](docs/rules/MDL407.md) | `octal-integer` | warning | — | An integer written with a leading 0 is read in octal. |
| [MDL408](docs/rules/MDL408.md) | `value-parsed-as-option` | error | — | A value starting with `-` is parsed as an option. |
| [MDL409](docs/rules/MDL409.md) | `non-ascii-option-value` | warning | — | A string option's value contains non-ASCII characters. |

**Table sections and masking**

| Rule | Name | Severity | Fix | Summary |
|---|---|---|---|---|
| [MDL501](docs/rules/MDL501.md) | `unknown-table-key` | error | unsafe | A key of a table section that mydumper does not know. |
| [MDL502](docs/rules/MDL502.md) | `unknown-masquerade-function` | error | unsafe | A masked column's value names no masking function of the version. |
| [MDL503](docs/rules/MDL503.md) | `masquerade-function-prefix` | warning | — | A masking function is selected by a prefix of a longer word. |
| [MDL504](docs/rules/MDL504.md) | `masquerade-syntax` | error | — | A masking function's arguments make mydumper stop, or misbehave. |
| [MDL505](docs/rules/MDL505.md) | `malformed-masquerade-key` | error | unsafe | A masked column is missing its closing backtick. |
| [MDL506](docs/rules/MDL506.md) | `sql-unbalanced` | error | — | An SQL value has unbalanced parentheses, quotes or comments. |
| [MDL507](docs/rules/MDL507.md) | `columns-count-mismatch` | warning | — | `columns_on_select` and `columns_on_insert` list different numbers of columns. |
| [MDL508](docs/rules/MDL508.md) | `masquerade-file-missing` | warning (opt-in) | — | A `<file X>` of a masking format does not exist. |
| [MDL509](docs/rules/MDL509.md) | `table-group-not-dumped` | info (opt-in) | — | A table section names a table the `regex` of [mydumper] excludes. |
| [MDL510](docs/rules/MDL510.md) | `extra-file-section-dropped` | error | — | The defaults file is rejected, so the extra file's table sections and variable groups are ignored. |
| [MDL511](docs/rules/MDL511.md) | `table-sections-lost` | error | — | This mydumper version ignores every table section, masking included. |

**Connection (MySQL client library)**

| Rule | Name | Severity | Fix | Summary |
|---|---|---|---|---|
| [MDL601](docs/rules/MDL601.md) | `connection-keys-ignored` | error | unsafe | `host`, `user` or `password` in a group where nobody reads them. |
| [MDL602](docs/rules/MDL602.md) | `mysql-include-directive` | warning | — | `!include` or `!includedir`: a MySQL directive that mydumper does not follow. |
| [MDL603](docs/rules/MDL603.md) | `client-group-overridden` | warning | — | An extra file makes the MySQL client library ignore the defaults file's [client]. |

**Team conventions**

| Rule | Name | Severity | Fix | Summary |
|---|---|---|---|---|
| [MDL901](docs/rules/MDL901.md) | `filename-pattern` | error | — | The file name does not follow the naming convention. |
| [MDL902](docs/rules/MDL902.md) | `value-template` | error | — | A value does not match its template. |
| [MDL903](docs/rules/MDL903.md) | `table-schema-template` | error | — | A table section names another database than the template. |
| [MDL904](docs/rules/MDL904.md) | `required-key` | error | — | A key the conventions require is missing. |
| [MDL905](docs/rules/MDL905.md) | `forbidden-key` | error | — | A key or group the conventions forbid is present. |
<!-- rules:end -->

## How it works, and why you can trust it

mydumper-lint rebuilds, line by line, everything between your file and mydumper's
behavior:

1. **mydumper's pre-processor**, which appends `= 1` to lines without `=` and forgets
   to reset its state after a `[`, the source of the most surprising failures;
2. **GLib's key-file parser** (`GKeyFile`), byte for byte, including its handling of
   whitespace, carriage returns, NUL bytes, locales and invalid UTF-8;
3. **GLib's option parser** (`GOption`), which mydumper feeds with `--key value` pairs
   built from your file: how `routines=0` enables routines, why `threads=08` aborts,
   how a value starting with `-` swallows the next key;
4. **mydumper's own logic**: which groups each tool reads, which keys the MySQL client
   library reads instead, table sections and the masking functions of each version;
5. **a knowledge base of every mydumper release** since v0.19.1-1, generated from the
   upstream sources and cross-checked against the `--help` of the official images.

None of this is trusted blindly:

- a **GLib oracle** (a small C program linked against real GLib, run on GLib 2.68,
  2.80 and the newest release) validates the emulators on a corpus of byte-exact cases
  and by differential fuzzing, with no divergence;
- an **end-to-end suite** runs the official mydumper images against a real MySQL server
  to prove the consequence each rule claims (a masked column really dumped in
  plaintext, a run that really aborts);
- every fix is fuzzed for idempotence and for the fixer's invariants.

The [design document](docs/design/2026-09-24-mydumper-lint-design.md) lists every fact
the rules rely on, with its evidence.

## FAQ

**Why not editorconfig, or a generic INI linter?** They can catch trailing whitespace,
but not a `[` in a comment that breaks the next empty line (MDL108), `routines=0`
enabling routines (MDL402), a value that swallows the next key (MDL408), or a
misspelled masking function that ships your customers' e-mails in plaintext (MDL502).
Those depend on mydumper's pre-processor, GLib's option parser and the mydumper
version.

**Does it change my files?** Only with `--fix`, and only in ways it has checked.
`--diff` shows the same changes without writing.

**It says my file is fine, but mydumper disagrees.** Check the target version
(`mydumper-lint versions`, `--mydumper-version`), then please open an issue with the
file and mydumper's output: the emulators are meant to be exact.

**The file works on my laptop but not in cron.** mydumper-lint assumes mydumper runs
without `LANG`, as in cron jobs and the official image: non-ASCII values then abort
mydumper (MDL409), and localized keys disappear (MDL305).

**Is myloader supported?** Yes: [myloader] and its groups are checked against
myloader's options.

## Contributing

Contributions are welcome: bug reports with a reproducing file, new rules, new
mydumper versions. See [CONTRIBUTING.md](CONTRIBUTING.md) for the development setup,
how to add a rule, how the knowledge base is regenerated, and how to run the oracle
and end-to-end suites. Quick version:

```sh
make test        # unit and golden tests
make lint        # golangci-lint, pinned, in Docker
make docs        # regenerate docs/rules, docs/versions.md, the README tables
```

Please report security problems privately, as described in [SECURITY.md](SECURITY.md).

## License

mydumper-lint is licensed under the [Apache License 2.0](LICENSE). The GLib oracle in
`tools/oracle` is a test tool licensed under the GPL-3.0-or-later; it is never part of
the mydumper-lint binary. See [NOTICE](NOTICE) for third-party notices. mydumper is a
separate project, not affiliated with mydumper-lint.
