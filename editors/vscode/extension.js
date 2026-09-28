// mydumper-lint for VS Code: starts `mydumper-lint server` (the language
// server shipped with the command line) for .cnf files. All the checking is
// in the server; this file only connects VS Code to it.
"use strict";

const vscode = require("vscode");
const { LanguageClient, TransportKind } = require("vscode-languageclient/node");

let client;

function settings() {
  const cfg = vscode.workspace.getConfiguration("mydumperLint");
  return { path: cfg.get("path") || "mydumper-lint", mydumperVersion: cfg.get("mydumperVersion") || "" };
}

function createClient() {
  const s = settings();
  // TransportKind.stdio adds --stdio, which the server accepts.
  const run = { command: s.path, args: ["server"], transport: TransportKind.stdio };
  return new LanguageClient(
    "mydumperLint",
    "mydumper-lint",
    { run, debug: run },
    {
      // .cnf files only: other .ini files are not mydumper configurations.
      documentSelector: [{ scheme: "file", pattern: "**/*.cnf" }],
      initializationOptions: { mydumperLint: { mydumperVersion: s.mydumperVersion } },
      synchronize: {
        // Sent as workspace/didChangeConfiguration: the server re-checks.
        configurationSection: "mydumperLint",
        // A changed .mydumper-lint.yaml is read again.
        fileEvents: vscode.workspace.createFileSystemWatcher("**/.mydumper-lint.{yaml,yml}"),
      },
    },
  );
}

async function activate(context) {
  client = createClient();
  context.subscriptions.push(
    vscode.commands.registerCommand("mydumperLint.restart", async () => {
      await client.stop();
      client = createClient();
      await client.start();
    }),
    vscode.workspace.onDidChangeConfiguration(async (e) => {
      if (e.affectsConfiguration("mydumperLint.path")) {
        await vscode.commands.executeCommand("mydumperLint.restart");
      }
    }),
  );
  try {
    await client.start();
  } catch (err) {
    vscode.window.showErrorMessage(
      `mydumper-lint could not start "${settings().path} server": ${err.message}. Install mydumper-lint, or set mydumperLint.path.`,
    );
  }
}

function deactivate() {
  return client ? client.stop() : undefined;
}

module.exports = { activate, deactivate };
