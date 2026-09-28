# End-to-end suite

The strongest guarantee of mydumper-lint (design §11.6): every runtime
consequence a rule claims is proven here, against the real mydumper and myloader
binaries of the official images and a real MySQL 8.4 server. A rule may only
claim a consequence that a scenario proves (`Meta.E2E` names the scenarios).

The suite also settles the open hypotheses of the design (§3.7 C1–C4, the
findings W1) before any rule relies on them.

## Running it

You need Go and Docker with the compose plugin.

```sh
make e2e          # default matrix: latest stable image-verified release of each branch
make e2e-all      # every image-verified version (pulls about thirty images)
make e2e-down     # stop MySQL and remove leftover containers
E2E_VERSIONS=v0.19.3-3,v1.0.8-1 make e2e       # a list of tags
make e2e E2E_FLAGS='-run TestScenarios/mdl102 -v'   # one scenario, verbose
E2E_LINT_ONLY=1 go test -tags e2e -run TestScenarios ./e2e/   # lint: lines only, no Docker
```

`make e2e` starts MySQL (`make e2e-up`, also done by the harness), runs
`go test -tags e2e ./e2e/...` and prints `e2e/.out/summary.md`: the images and
their guard, a scenario × version table with durations, the failures, and the
pending lint checks. Every run writes `e2e/.out/logs/<scenario>/<version>.log`:
the command, exit code, stderr, each observation with what was seen, and the
linter's diagnostics. Failed runs keep their work directory
(`e2e/.out/work/<scenario>/<version>/`, mounted as `/work`).

| Variable | Effect |
|---|---|
| `E2E_VERSIONS` | empty: default matrix; `all`: every image-verified version; or a comma-separated list of tags (each must be in the knowledge base) |
| `E2E_PARALLEL` | concurrent runs (`make` only, default 4) |
| `E2E_TIMEOUT` | limit per container run (Go duration, default `5m`) |
| `E2E_KEEP=1` | keep every work directory, not only the failed ones |
| `E2E_LINT_ONLY=1` | only compare the linter with the `lint:` lines, without Docker |
| `E2E_SUMMARY` | where to write the summary (default `e2e/.out/summary.md`) |

## How it works

- **MySQL.** `compose.yaml` runs `mysql:8.4` pinned by digest, on the network
  `mydumper-lint-e2e`, seeded by `fixtures/seed.sql`: database `app` with
  `users(id, email, name)` (three rows with recognizable emails
  `alice@e2e.example`, `bob@e2e.example`, `zoe@e2e.example`; `Zoé` is the
  non-ASCII name), `orders`, the view `e2e_user_totals`, the trigger
  `e2e_orders_amount`, the procedure `e2e_count_users` and the disabled event
  `e2e_noop_event`. Test-only credentials: `root`/`e2e-root-password` and
  `e2e`/`e2e-password` (all privileges). The data directory is a tmpfs: every
  `up` starts from the seed.
- **Versions.** They come from the embedded knowledge base
  (`internal/optionsdb`), restricted to versions whose image passed the
  generator's cross-check (`ImageVerified`). The default matrix takes the latest
  stable one of each branch (MAJOR.MINOR). With the default matrix, a scenario
  that applies to none of those versions runs on the newest image-verified
  version it applies to (marked † in the summary), so every scenario runs in
  every `make e2e`.
- **Images and the image guard.** Each version runs
  `mydumper/mydumper:<tag>@<digest>`, with the digest that
  `tools/gen-optionsdb -verify-images` recorded in
  `tools/gen-optionsdb/images.json`: the suite runs exactly the images the
  knowledge base was verified against. Before use, `mydumper --version` and
  `myloader --version` must report the tag. Otherwise the version is skipped
  with a loud banner and listed in the summary; it is never run silently
  (`mydumper/mydumper:v1.0.5-1` ships a v1.0.3-1 binary).
- **Runs.** One container per scenario and version:
  `docker run --rm --network mydumper-lint-e2e -v <work>:/work -w /work
  --user <uid>:<gid> --entrypoint <tool> <image> <args>`. For myloader, the
  harness first dumps `app` into `/work/source-dump` with the same image
  (`mydumper --host mysql --user e2e --password e2e-password --database app
  --outputdir /work/source-dump`).
- **Linter.** The harness builds `./cmd/mydumper-lint` once and runs
  `check --format json --mydumper-version <tag> --preview --no-config` on the
  scenario's files (`--config` with the scenario's `.mydumper-lint.yaml` when it
  has one). The set of reported rule IDs must equal the `lint:` line, for the
  rules that are registered (`internal/rules`). IDs of rules not registered yet
  are recorded as *pending* in the log and the summary, and not checked. When a
  scenario observes `config-loaded`, the linter's `loadable` verdict must agree
  too, unless a pending MDL1xx rule is expected (MDL1xx rules decide
  loadability).

## Scenario format

One file per scenario in `scenarios/*.txtar` (txtar, as in the golden tests):
the comment says what the scenario proves; the `scenario` section describes the
run; every other section is a file written into `/work`. A section whose name
ends in `.esc` holds one line of Go string escapes, for exact bytes (`\r`, a
BOM as `\xef\xbb\xbf`, `\x20` for a trailing space, no final newline). A
section named `.mydumper-lint.yaml` is the linter configuration (load sets).

```text
MDL102, F1, F12: a line of two spaces makes mydumper ignore the whole file…
-- scenario --
tool: mydumper
versions: >= v0.19.3-1
args: --defaults-file /work/defaults.cnf --database app
lint: MDL102, MDL405
expect config-loaded: false
expect exit: 0
expect connected: true
expect column app.users.email: plaintext
-- defaults.cnf.esc --
[client]\nhost=mysql\nuser=e2e\npassword=e2e-password\n\n[mydumper]\nthreads=2\n\x20\x20\n…
```

The `scenario` section is line-based and strict: an unknown directive,
observation or value, a duplicate, or a missing required line is an error.
Blank lines and lines starting with `#` are ignored.

| Line | Meaning |
|---|---|
| `tool: mydumper` | required, first: `mydumper` or `myloader` |
| `versions: COND, …` | optional: the versions the scenario applies to (every condition must hold) |
| `args: …` | required, repeatable: container arguments, split on spaces (put values with spaces in a file) |
| `env: NAME=value` | repeatable: container environment (the images set no `LANG`) |
| `lint: MDL102, MDL405` | required: the rule IDs the linter must report; empty for none |
| `lint-files: a.cnf b.cnf` | optional: the files to lint (default: every `.cnf` section) |
| `expect OBSERVATION [ARG]: VALUE` | repeatable: a runtime expectation |
| `when COND, …` … `end` | a block of `lint:` and `expect` lines for the versions meeting the conditions |

A condition is `OP TAG` with `OP` one of `<`, `<=`, `>`, `>=`, `=`, `!=`
(`>= v0.19.3-1`), or a fact of the knowledge base about the version:
`kb:ignore-unknown-options`, `kb:masquerade=null`, `kb:table-sections-ignored` (mydumper
loses every table section, F17), negated with `!`
(`kb:!ignore-unknown-options`). Blocks override the top-level `lint:` and
single-valued observations; repeatable observations accumulate. Two blocks that
apply to the same version must agree: a knowledge-base block and a version
block that disagree make the plan fail, which is how a scenario checks both the
knowledge base and the boundary the design states.

`${RESTORE_DB}` in `args` or an observation argument is replaced by a database
name unique to the run, dropped afterwards.

## Observations

| Observation | Tools | Meaning |
|---|---|---|
| `config-loaded [FILE]` | both | `true` unless stderr contains GLib's `Failed to load config file` warning (F1); with a file argument, only the warning for `/work/FILE` counts |
| `exit` | both | the exit code: `0`, another number, or `nonzero` |
| `stderr-contains` | both | stderr contains the text (repeatable); both tools log everything to stderr |
| `stderr-lacks` | both | stderr does not contain the text (repeatable) |
| `connected` | mydumper | `true` when mydumper wrote its `metadata` (or `metadata.partial`) file, which it creates only after connecting; `false` otherwise (the connection error is quoted) |
| `column DB.TABLE.COLUMN` | mydumper | what the rows of the table's data files hold in the column: `plaintext` (every seeded value), `masked` (other non-empty values), `empty` (empty strings) or `null` (NULL); anything else is reported as a mix and matches none. Seeded: `app.users.email` |
| `routines-dumped` | mydumper | `true` when a dump file defines the procedure ``e2e_count_users`` |
| `data-dumped` | mydumper | `true` when the dump holds a data file of `app` |
| `threads` | mydumper | the number of dumper threads mydumper logs (`Using N dumper threads`, needs `--verbose 3`) |
| `dump-dir` | mydumper | where the dump went, relative to `/work` (`dump`, `export-*` for the default timestamped directory), or `none` |
| `dump-contains` | mydumper | a dump file contains the text (repeatable) |
| `dump-lacks` | mydumper | no dump file contains the text (repeatable; fails without a dump) |
| `rows DB.TABLE` | both | the table's row count on the server after the run, or `missing`; `DB` may be `${RESTORE_DB}` |

The dump directory is the only directory under `/work` holding a `metadata` or
`metadata.partial` file (the myloader source dump excepted). Data files are the
dump's `DB.TABLE.*` files that are not schema files, read as mydumper's SQL
format (`INSERT … VALUES(…),(…);`); gzip is decompressed, zstd is not supported
(do not compress or switch to CSV in scenarios that read data).

## Adding a scenario

1. Name it after the rule or fact it proves: `mdl102-…` for a rule
   consequence, `c1-…`/`w1-…` for a hypothesis, `…-control-…` for the
   counterpart that shows the observation can tell both outcomes apart.
2. Write the smallest file that shows the consequence, and restrict `versions:`
   to where the behavior holds. Connect through `[client]` when the scenario is
   about the file being ignored (F12), and with `--host mysql --user e2e
   --password e2e-password` on the command line otherwise. Always pass
   `--database app` (or `--tables-list`) unless the file itself selects what to
   dump, so that concurrent runs do not see each other's restore databases.
3. List in `lint:` the rule IDs design §6.3 says the linter reports, whether or
   not the rules exist yet, and check them with `E2E_LINT_ONLY=1` (seconds, no
   Docker).
4. Run it on the default matrix and on every version
   (`E2E_VERSIONS=all make e2e E2E_FLAGS='-run TestScenarios/<name>'`).
   Never bend an expectation to make a run pass: a surprising observation is a
   finding about mydumper or about the design.
5. Reference it from the rule's `Meta.E2E`; `TestRuleE2EReferences` checks the
   references, `TestScenarioFiles` parses and plans every scenario on every
   embedded version without Docker.
