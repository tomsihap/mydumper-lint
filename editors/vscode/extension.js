// mydumper-lint for VS Code: starts `mydumper-lint server` (the language
// server shipped with the command line) for .cnf files. All the checking is
// in the server; this file only connects VS Code to it.
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const vscode = require("vscode");
const { LanguageClient, TransportKind } = require("vscode-languageclient/node");

let client;
let output;

function settings() {
  const cfg = vscode.workspace.getConfiguration("mydumperLint");
  return { path: cfg.get("path") || "", mydumperVersion: cfg.get("mydumperVersion") || "" };
}

// The executable the platform-specific packages ship in bin/, built by the
// same release as the extension; "" in the universal package.
function bundled(extensionPath) {
  const exe = path.join(extensionPath, "bin", process.platform === "win32" ? "mydumper-lint.exe" : "mydumper-lint");
  if (!fs.existsSync(exe)) {
    return "";
  }
  if (process.platform !== "win32") {
    try {
      fs.accessSync(exe, fs.constants.X_OK);
    } catch {
      // An unpacker that dropped the mode bits: restore them if we may.
      try {
        fs.chmodSync(exe, 0o755);
      } catch {
        return "";
      }
    }
  }
  return exe;
}

// Which mydumper-lint to run: the mydumperLint.path setting, else the bundled
// executable, else the one found in PATH.
function executable(extensionPath) {
  const s = settings();
  if (s.path) {
    return { command: s.path, origin: "the mydumperLint.path setting" };
  }
  const exe = bundled(extensionPath);
  if (exe) {
    return { command: exe, origin: "bundled with the extension" };
  }
  return { command: "mydumper-lint", origin: "found in PATH" };
}

function createClient(extensionPath) {
  const s = settings();
  const exe = executable(extensionPath);
  output.info(`Starting ${exe.command} server (${exe.origin}).`);
  // TransportKind.stdio adds --stdio, which the server accepts.
  const run = { command: exe.command, args: ["server"], transport: TransportKind.stdio };
  const c = new LanguageClient(
    "mydumperLint",
    "mydumper-lint",
    { run, debug: run },
    {
      // .cnf files only: other .ini files are not mydumper configurations.
      documentSelector: [{ scheme: "file", pattern: "**/*.cnf" }],
      initializationOptions: { mydumperLint: { mydumperVersion: s.mydumperVersion } },
      outputChannel: output,
      synchronize: {
        // Sent as workspace/didChangeConfiguration: the server re-checks.
        configurationSection: "mydumperLint",
        // A changed .mydumper-lint.yaml is read again.
        fileEvents: vscode.workspace.createFileSystemWatcher("**/.mydumper-lint.{yaml,yml}"),
      },
    },
  );
  c.executable = exe;
  return c;
}

async function start(extensionPath) {
  client = createClient(extensionPath);
  try {
    await client.start();
  } catch (err) {
    const { command, origin } = client.executable;
    const hint =
      origin === "found in PATH"
        ? "Install mydumper-lint, or set mydumperLint.path."
        : "Check mydumperLint.path, or clear it to use the executable bundled with the extension.";
    const message = `mydumper-lint could not start "${command} server" (${origin}): ${err.message}. ${hint}`;
    output.error(message);
    vscode.window.showErrorMessage(message);
  }
}

async function activate(context) {
  // A log channel: the language client writes its own errors and traces with
  // .error(), .info()… (vscode-languageclient 10).
  output = vscode.window.createOutputChannel("mydumper-lint", { log: true });
  context.subscriptions.push(
    output,
    vscode.commands.registerCommand("mydumperLint.restart", async () => {
      await stop();
      await start(context.extensionPath);
    }),
    vscode.workspace.onDidChangeConfiguration(async (e) => {
      if (e.affectsConfiguration("mydumperLint.path")) {
        await vscode.commands.executeCommand("mydumperLint.restart");
      }
    }),
  );
  await start(context.extensionPath);
}

// A client whose server failed to start cannot be stopped (the language
// client throws): there is nothing to stop then.
async function stop() {
  if (client && client.isRunning()) {
    await client.stop();
  }
}

function deactivate() {
  return stop();
}

module.exports = { activate, deactivate, executable };
