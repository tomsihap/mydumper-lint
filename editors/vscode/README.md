# mydumper-lint for VS Code

Checks [mydumper](https://github.com/mydumper/mydumper) and myloader configuration files
(`.cnf`) as you type, with [mydumper-lint](https://github.com/tomsihap/mydumper-lint):

- every problem mydumper-lint finds, with what mydumper will actually do: a line of
  spaces that makes mydumper ignore the whole file (and its masking rules), `routines=0`
  that turns routines on, a misspelled masking function that dumps a column in clear
  text…
- quick fixes, safe and unsafe, and **Fix all safe mydumper-lint problems**;
- the rule's explanation when you hover over a problem;
- the settings of the nearest `.mydumper-lint.yaml`, like `mydumper-lint check`.

## Requirements

The extension runs `mydumper-lint server`, so mydumper-lint must be installed:

```sh
go install github.com/tomsihap/mydumper-lint/cmd/mydumper-lint@latest
```

or a [release binary](https://github.com/tomsihap/mydumper-lint/releases). If it is not in
your `PATH`, set `mydumperLint.path`.

## Settings

| Setting | Default | |
|---|---|---|
| `mydumperLint.path` | `mydumper-lint` | the executable |
| `mydumperLint.mydumperVersion` | (empty) | the mydumper version to check against; empty: the `mydumper-version` of `.mydumper-lint.yaml`, else the latest stable release |
| `mydumperLint.trace.server` | `off` | log the protocol messages |

To apply the safe fixes on save:

```json
"editor.codeActionsOnSave": { "source.fixAll.mydumper-lint": "explicit" }
```
