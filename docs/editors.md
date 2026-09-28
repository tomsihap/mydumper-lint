# Editors

`mydumper-lint server` is a language server (LSP) on stdin and stdout. Any editor that
speaks LSP gets, for `.cnf` files:

- the diagnostics of `mydumper-lint check`, updated as you type, each with what mydumper
  will do and a link to the rule's page;
- quick fixes: the rule's fix (safe, or unsafe when it changes what mydumper does), and
  **Disable MDLxxx for this line** (a `# mydumper-lint: disable-next-line=` comment);
- **Fix all safe mydumper-lint problems** (`source.fixAll.mydumper-lint`), which editors
  can run on save;
- the rule's explanation when you hover over a problem.

Each file gets the settings `mydumper-lint check` would give it: the nearest
`.mydumper-lint.yaml` (read again when you save it), or `--config`. The editor can set the
mydumper version with the `mydumperVersion` setting (below), which takes precedence over
the configuration file. Load sets (`load-sets:`, MDL509, MDL510, MDL603) are only analyzed
by `mydumper-lint check`.

Install mydumper-lint first (see the [README](../README.md#install)) and make sure
`mydumper-lint` is in the `PATH` the editor sees.

## VS Code

The extension in [`editors/vscode`](../editors/vscode) starts the server for `.cnf` files.
Build and install it:

```sh
make vscode
code --install-extension editors/vscode/mydumper-lint.vsix
```

Settings: `mydumperLint.path` (the executable), `mydumperLint.mydumperVersion`, and
`mydumperLint.trace.server`. To apply the safe fixes on save, in `settings.json`:

```json
"[ini]": {
  "editor.codeActionsOnSave": { "source.fixAll.mydumper-lint": "explicit" }
}
```

The extension maps `.cnf` to the `ini` language, so the files keep INI highlighting.

## Neovim (0.11 or later)

```lua
vim.filetype.add({ extension = { cnf = "dosini" } })

vim.lsp.config("mydumper_lint", {
  cmd = { "mydumper-lint", "server" },
  filetypes = { "dosini" },
  -- dosini also covers other .ini files: only attach to .cnf files.
  root_dir = function(bufnr, on_dir)
    if vim.api.nvim_buf_get_name(bufnr):match("%.cnf$") then
      on_dir(vim.fs.root(bufnr, { ".mydumper-lint.yaml", ".mydumper-lint.yml", ".git" }) or vim.fn.getcwd())
    end
  end,
  -- init_options = { mydumperLint = { mydumperVersion = "v0.19.3-3" } },
})
vim.lsp.enable("mydumper_lint")
```

Fixes are in `vim.lsp.buf.code_action()`; the rule's explanation in `vim.lsp.buf.hover()`.

## Helix

In `languages.toml`:

```toml
[language-server.mydumper-lint]
command = "mydumper-lint"
args = ["server"]

[[language]]
name = "mydumper-cnf"
scope = "source.mydumper-cnf"
file-types = ["cnf"]
grammar = "ini"
comment-token = "#"
language-servers = ["mydumper-lint"]
```

## Emacs (Eglot)

```elisp
(define-derived-mode mydumper-cnf-mode conf-unix-mode "mydumper-cnf")
(add-to-list 'auto-mode-alist '("\\.cnf\\'" . mydumper-cnf-mode))
(with-eval-after-load 'eglot
  (add-to-list 'eglot-server-programs '(mydumper-cnf-mode "mydumper-lint" "server")))
```

Then `M-x eglot` in a `.cnf` file, or `(add-hook 'mydumper-cnf-mode-hook #'eglot-ensure)`.

## JetBrains IDEs (IntelliJ IDEA, PhpStorm, DataGrip…)

With the [LSP4IJ](https://plugins.jetbrains.com/plugin/23257-lsp4ij) plugin:

1. **Settings › Languages & Frameworks › Language Servers**, then **+**.
2. **Server**: name `mydumper-lint`, command `mydumper-lint server` (an absolute path if the
   IDE does not see your `PATH`).
3. **Mappings › File name patterns**: add `*.cnf`, language id `ini`.
4. Optional, **Configuration › Initialization options**:
   `{"mydumperLint": {"mydumperVersion": "v0.19.3-3"}}`.

## Any other editor

Start `mydumper-lint server` over stdio for `.cnf` files. The server accepts `--stdio`,
`--config FILE`, `--no-config` and `--mydumper-version VERSION`; the client may send
`{"mydumperLint": {"mydumperVersion": "…"}}` (or the flat `{"mydumperVersion": "…"}`) as
`initializationOptions` or in `workspace/didChangeConfiguration`. It supports the UTF-8,
UTF-16 and UTF-32 position encodings, full document synchronization, `textDocument/
codeAction` and `textDocument/hover`.

## What was tested

The server is tested in Go with a scripted client (`internal/lsp`, and
`internal/cli/server_cmd_test.go` against the real linter and configuration discovery),
and with `vscode-jsonrpc`, the protocol library of VS Code's client. The VS Code
extension's wiring has unit tests (`editors/vscode/test`) and its package builds in CI.
The Neovim, Helix, Emacs and JetBrains configurations follow each editor's documented
LSP setup; reports of what works or not in them are welcome.
