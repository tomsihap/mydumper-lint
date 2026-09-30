# mydumper-lint for VS Code

Checks [mydumper](https://github.com/mydumper/mydumper) and myloader configuration files
(`.cnf`) as you type, with [mydumper-lint](https://github.com/tomsihap/mydumper-lint):

- every problem mydumper-lint finds, with what mydumper will actually do: a line of
  spaces that makes mydumper ignore the whole file (and its masking rules), `routines=0`
  that turns routines on, a misspelled masking function that dumps a column in clear
  text…
- quick fixes, safe and unsafe, **Disable MDLxxx for this line**, and **Fix all safe
  mydumper-lint problems**;
- the rule's explanation when you hover over a problem, with a link to its
  [documentation](https://github.com/tomsihap/mydumper-lint/tree/main/docs/rules);
- the settings of the nearest `.mydumper-lint.yaml`, like `mydumper-lint check`.

Everything runs on your machine: the extension starts `mydumper-lint server` and sends
nothing anywhere. To try the rules without installing anything, use the
[playground](https://tomsihap.github.io/mydumper-lint/).

## Installation

Install **mydumper-lint** from the Extensions view (the Visual Studio Marketplace, or
Open VSX in VSCodium, Cursor and other editors).

The extension includes mydumper-lint for Linux (x64, arm64, Alpine), macOS (Intel,
Apple silicon) and Windows (x64), from the same release: nothing else to install. On other
platforms it runs `mydumper-lint` from your `PATH`; install it with

```sh
go install github.com/tomsihap/mydumper-lint/cmd/mydumper-lint@latest
```

or from a [release](https://github.com/tomsihap/mydumper-lint/releases). With Remote-SSH,
WSL or dev containers, the extension runs on the remote machine, where the files are.

## Settings

| Setting | Default | |
|---|---|---|
| `mydumperLint.path` | (empty) | the executable to run; empty: the one bundled with the extension, else `mydumper-lint` from `PATH` |
| `mydumperLint.mydumperVersion` | (empty) | the mydumper version to check against; empty: the `mydumper-version` of `.mydumper-lint.yaml`, else the latest stable release |
| `mydumperLint.trace.server` | `off` | log the protocol messages in the **mydumper-lint** output channel |

`.cnf` files open as INI. To apply the safe fixes on save, in `settings.json`:

```json
"[ini]": {
  "editor.codeActionsOnSave": { "source.fixAll.mydumper-lint": "explicit" }
}
```

The **mydumper-lint** output channel says which executable runs; **mydumper-lint:
Restart the server** restarts it.

## Also

The same checks run on the command line, in CI (a GitHub Action, pre-commit hooks, SARIF
for code scanning) and in other editors (Neovim, Helix, Emacs, JetBrains IDEs): see
[mydumper-lint](https://github.com/tomsihap/mydumper-lint#readme). Issues and ideas are
welcome on [GitHub](https://github.com/tomsihap/mydumper-lint/issues).
