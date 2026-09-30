# Contributing to mydumper-lint

Thank you for helping. mydumper-lint is only useful if it is exact: every rule states
what mydumper will do, so every change comes with evidence. This guide explains the
layout, the test layers, and the two most common contributions: adding a rule and
adding a mydumper version.

- [Reporting a problem](#reporting-a-problem)
- [Development setup](#development-setup)
- [Repository tour](#repository-tour)
- [Tests](#tests)
- [Adding a rule](#adding-a-rule)
- [Adding a mydumper version](#adding-a-mydumper-version)
- [Pull requests](#pull-requests)
- [Licensing rules](#licensing-rules)

## Reporting a problem

The most valuable report is a **false positive or false negative with the file that
shows it**, the mydumper version, the command line, and mydumper's output
(`--verbose 3` helps). Configuration files are byte-sensitive: attach the file instead of
pasting it, or describe invisible bytes (a trailing space, a carriage return).

Security problems: see [SECURITY.md](SECURITY.md). Please do not open a public issue.

## Development setup

You need:

- **Go 1.26** or later;
- **Docker** for the pinned tools (golangci-lint, actionlint, govulncheck), the GLib
  oracle images and the end-to-end suite;
- optionally a C compiler, `pkg-config` and GLib to build the oracle locally
  (`make oracle`).

```sh
make build       # bin/mydumper-lint
make test        # unit and golden tests, with the race detector
make test-short  # the same, faster
make lint        # golangci-lint (pinned, in Docker)
make check-all   # go vet, golangci-lint, govulncheck and the tests
make help        # every target
```

## Repository tour

The pipeline follows what mydumper does with a file (design §4):

| Package | Role |
|---|---|
| `internal/source` | Lossless line table: every byte, every line ending, positions. |
| `internal/preprocess` | mydumper's pre-processor (`= 1` insertion and its state leak), or none for v0.19.1-x. |
| `internal/keyfile` | GLib's `GKeyFile` parser, byte for byte, plus one precise cause per rejected line and `Recover`. |
| `internal/goption` | GLib's `GOption` parser, for the `--key value` vectors mydumper builds. |
| `internal/optionsdb` | The knowledge base: every mydumper version's options, table keys, masking functions. |
| `internal/model` | The effective configuration: what each key does, and why a key has no effect. |
| `internal/rules` | One file per rule, the rule registry, suppressions, golden tests. |
| `internal/fix` | Applying fixes, the fixpoint loop, the health guard and the self-check, atomic writes. |
| `internal/lint` | Ties the pipeline together. |
| `internal/cli`, `internal/report`, `internal/config` | Command line, output formats, `.mydumper-lint.yaml`. |
| `internal/playground`, `cmd/mydumper-lint-wasm`, `web/playground` | The browser playground: the command line run in memory, its WebAssembly bridge, and the static page. |
| `internal/lsp`, `editors/vscode` | The language server behind `mydumper-lint server` (JSON-RPC, positions, diagnostics, code actions, hover), and the VS Code extension that starts it. |
| `tools/oracle` | The GLib oracle (C, GPL-3.0-or-later, test only). |
| `tools/gen-optionsdb` | Generates the knowledge base from the upstream sources. |
| `tools/gendocs` | Generates `docs/rules`, `docs/versions.md`, the README tables and the config schema. |
| `e2e` | The end-to-end suite: official mydumper images against MySQL. |

The [design document](docs/design/2026-09-24-mydumper-lint-design.md) is the source of
truth: the facts every rule relies on (K*, P*, G*, F*, C*), with their evidence, the rule
catalog and the invariants of the fixer. Update it with your change when you learn
something new about mydumper or GLib.

## Tests

| Layer | Where | Run |
|---|---|---|
| Unit tests | `*_test.go` | `make test` |
| Golden rule tests | `internal/rules/testdata/<ID>/*.txtar` | `make test`; `go test ./internal/rules -run TestGolden -update` rewrites the expected sections |
| Oracle conformance | `testdata/oracle-cases`, `testdata/goption-cases` | `make oracle-conformance` (Docker) |
| Differential fuzzing | `internal/oracletest`, `internal/goption` | `make oracle-diff` (Docker), or `MYDUMPER_LINT_ORACLE=bin/oracle go test ./internal/oracletest/` |
| Fixer fuzzing | `internal/lint` (`FuzzFix`, `FuzzCheck`) | `go test ./internal/lint -run '^$' -fuzz '^FuzzFix$' -fuzztime 2m` |
| End to end | `e2e/scenarios/*.txtar` | `make e2e` (Docker), `make e2e-all` for every version |
| Upstream examples | `internal/lint/upstream_integration_test.go` | `make integration` (network): every tag's example file lints with no error |
| Mutation testing | the core packages | `make mutation` (slow; nightly): gremlins, with a blocking efficacy threshold |
| Playground | `web/playground/examples.test.mjs`, `internal/playground` | `make playground-test` (needs Node): the page's examples through the WebAssembly build; `make playground-serve` to try the page on http://localhost:8765 |
| Language server | `internal/lsp`, `internal/cli/server_cmd_test.go`, `editors/vscode/test` | `make test` for the server; `make vscode` (needs Node 22) tests the VS Code extension and packages it; `make vscode-test` runs the package in VS Code (downloaded; on Linux, under `xvfb-run`); `make vscode-release-check` packages every platform from a goreleaser snapshot, as a release does |

A golden case is a [txtar](https://pkg.go.dev/golang.org/x/tools/txtar) file:

```text
# What the case shows (the comment).
-- options --
mydumper-version: v0.19.3-3
select: MDL402, MDL001
-- input.cnf --
[mydumper]
routines=0
-- diagnostics --
(written by -update)
-- fixed-unsafe.cnf --
(written by -update)
```

Sections: `input.cnf` (or `input.cnf.esc` with `\n`, `\r`, `\t`, `\xHH` escapes for
byte-exact files; never write those with an editor), `options` (`select`, `ignore`,
`mydumper-version`, `path`, `base-dir`, `conventions` as JSON, `load-set` with a
`defaults.cnf` section, `doc-example`), and the expected `diagnostics`, `fixed.cnf`,
`fixed-unsafe.cnf`. The harness also checks that fixing twice changes nothing, and a
meta-test requires at least one case where each rule reports and one where it does
not.

After `-update`, **read the diff**: the expected output is the specification.

## Adding a rule

1. **Establish the fact.** What does mydumper (or GLib, or the MySQL client library)
   do, in which versions? Read the upstream source for the versions concerned
   (`make gen-optionsdb` caches every tag's source in `.cache/gen-optionsdb/src/`), and
   when the consequence is a runtime behavior, prove it with an end-to-end scenario
   (step 5). Add the fact to the design document.
2. **Pick an ID** in the right family (MDL1xx loading, 2xx groups, 3xx values, 4xx
   options, 5xx tables and masking, 6xx connection, 9xx conventions) and a stable
   kebab-case name. IDs are never reused.
3. **Write `internal/rules/mdlNNN_name.go`**: a `register(&Rule{Meta: …, Check: …})` in
   `init`. `Meta.Why` is user documentation: say what mydumper does, in plain words.
   Every diagnostic has a message (what is wrong) and a consequence (what mydumper will
   do). Use the helpers: `optionKeys` for option groups, `rejectionConsequence`,
   `fatalConsequence`, `removeLine` (safe around bracket leaks).
4. **Fixes.** A *safe* fix must not change what mydumper applies from the file (the
   fixer's self-check verifies it on every run); anything else is *unsafe*. Think about
   interactions: mydumper's pre-processor carries state across lines, and GLib's option
   parser lets a value swallow the next key.
5. **Tests.** Golden cases, positive and negative, with fixes; mark the best one with
   `doc-example: yes`. If the rule claims a runtime consequence, add an e2e scenario in
   `e2e/scenarios/` (see `e2e/README.md`) and cite it in `Meta.E2E`.
6. **Docs.** `make docs` regenerates `docs/rules/MDLNNN.md` and the README table. Add
   the rule to the catalog of the design document.
7. **Run** `make test`, `make lint`, a few minutes of `FuzzFix`, and `make docs-check`.

## Adding a mydumper version

New upstream tags are picked up by the `upstream-sync` workflow, which opens a pull
request. To do it by hand:

```sh
make gen-optionsdb        # fetches the new tags' sources, regenerates internal/optionsdb/data/optionsdb.json
make verify-images        # cross-checks every version with the --help of its official image
make docs                 # docs/versions.md and the README summary
```

Then review:

- the generator's report: options added, removed or retyped, `ignore_unknown_options`,
  masking functions, table keys;
- **the loader fingerprints**: when `load_config_file` or `parse_key_file_group` changed,
  the emulators may need a change. Read the new code and extend the tests before merging;
- the masking parser (`src/mydumper/mydumper_masquerade.c`): `internal/rules/masquerade.go`
  models it per version;
- `make e2e` with the new version (`E2E_VERSIONS=vX.Y.Z-N make e2e`).

## Pull requests

- One topic per pull request, with tests. The CI runs the unit, golden and fuzz seed
  tests, golangci-lint, govulncheck, actionlint, the docs check, and, when relevant, the
  oracle conformance and the end-to-end suite.
- Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/)
  (`feat(rules): …`, `fix(keyfile): …`): they drive the changelog and the version.
- Behavior changes that could fail users' CI (a new error, a changed default) ship as
  preview rules first; see the design document §13.

## Releasing

Merging the release pull request that release-please keeps open tags the version and runs
`.github/workflows/release.yml`: goreleaser (archives, checksums, SBOMs, signatures,
attestations, the ghcr.io image), then the VS Code packages (one per platform, attested and
attached to the release). The extension's version is the release's: release-please bumps
`editors/vscode/package.json` too.

Publishing the extension waits for the approval of the `marketplace` environment's
reviewer (**Review deployments** on the workflow run), then publishes to the Visual Studio
Marketplace and Open VSX with no stored token. A job that failed can be re-run alone:
packages already published are skipped.

One-time setup, in this order (until the last step, releases skip publishing):

1. Visual Studio Marketplace (https://marketplace.visualstudio.com/manage): the publisher
   `tomsihap` (`publisher` in `package.json`), then a trusted publisher for the repository
   `tomsihap/mydumper-lint`, workflow `release.yml`, environment `marketplace`.
2. Open VSX (https://open-vsx.org, signed in with GitHub): the Eclipse Foundation
   publisher agreement, the namespace `tomsihap`, then the same trusted publisher
   (https://open-vsx.org/user-settings/trusted-publishers).
3. GitHub, **Settings › Environments**: an environment `marketplace` with yourself as
   required reviewer, restricted to the `main` branch (release.yml runs on pushes to
   `main`). Create it before the next step: a job creates a missing environment without
   any protection.
4. GitHub, **Settings › Secrets and variables › Actions › Variables**: `VSCODE_PUBLISH` =
   `true`.

## Licensing rules

- mydumper-lint is Apache-2.0. mydumper is GPL-3.0: **never copy mydumper code** into the
  module. Re-implement behavior from its description, as the design document does.
- The oracle (`tools/oracle`) is GPL-3.0-or-later because it embeds mydumper's
  pre-processor for comparison; it is a test tool, never linked into the binary.
- GLib is LGPL; the emulators re-implement its behavior and do not copy its code.
- Upstream help texts are GPL: the knowledge base stores option names and types, never
  their descriptions.

By contributing, you agree that your contribution is licensed under the Apache License
2.0.
