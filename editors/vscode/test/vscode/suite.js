// Runs inside VS Code (test/vscode/run.js): drives the installed extension the
// way a user does and checks what VS Code shows. A failed assertion or a
// timeout fails the run.
"use strict";

const assert = require("node:assert");
const cp = require("node:child_process");
const fs = require("node:fs");
const path = require("node:path");
const vscode = require("vscode");

const exe = process.platform === "win32" ? "mydumper-lint.exe" : "mydumper-lint";

async function waitFor(what, probe, timeoutMs = 60000) {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    const value = await probe();
    if (value) {
      return value;
    }
    if (Date.now() > deadline) {
      throw new Error(`timed out waiting for ${what}`);
    }
    await new Promise((resolve) => setTimeout(resolve, 200));
  }
}

// A diagnostic's code is { value, target } when the server links its documentation.
const codeOf = (d) => (typeof d.code === "object" ? d.code.value : d.code);
const codes = (uri) => vscode.languages.getDiagnostics(uri).map(codeOf).sort().join(" ");
const until = (uri, want) => waitFor(`diagnostics "${want}"`, () => codes(uri) === want);

async function setVersion(version) {
  await vscode.workspace.getConfiguration("mydumperLint").update("mydumperVersion", version, vscode.ConfigurationTarget.Workspace);
}

async function run() {
  const ext = vscode.extensions.getExtension("tomsihap.mydumper-lint");
  assert.ok(ext, "the extension is installed");
  const bundled = path.join(ext.extensionPath, "bin", exe);
  assert.ok(fs.existsSync(bundled), `${bundled} is bundled`);

  const uri = vscode.Uri.joinPath(vscode.workspace.workspaceFolders[0].uri, "backup.cnf");
  const doc = await vscode.workspace.openTextDocument(uri);
  await vscode.window.showTextDocument(doc);
  assert.strictEqual(doc.languageId, "ini", ".cnf files open as INI");
  await waitFor("the extension to activate", () => ext.isActive);

  // The diagnostic, from the bundled executable (run.js removed mydumper-lint from PATH).
  await until(uri, "MDL102");
  const [d] = vscode.languages.getDiagnostics(uri);
  assert.strictEqual(d.source, "mydumper-lint");
  assert.strictEqual(d.severity, vscode.DiagnosticSeverity.Error);
  assert.strictEqual(d.range.start.line, 2);
  assert.match(d.message, /whitespace/);
  assert.match(String(d.code.target), /docs\/rules\/MDL102\.md$/);

  // The rule's explanation on hover.
  const hovers = await vscode.commands.executeCommand("vscode.executeHoverProvider", uri, d.range.start);
  const hoverText = hovers.flatMap((h) => h.contents.map((c) => (typeof c === "string" ? c : c.value))).join("\n");
  assert.match(hoverText, /MDL102/);

  // The setting reaches the server: v0.19.1-3 has no pre-processor, so a line
  // of spaces is harmless there; clearing it goes back to .mydumper-lint.yaml.
  await setVersion("v0.19.1-3");
  await until(uri, "");
  await setVersion("");
  await until(uri, "MDL102");

  // Fix all gives what `mydumper-lint check --fix` gives.
  const [fixAll] = await vscode.commands.executeCommand(
    "vscode.executeCodeActionProvider",
    uri,
    new vscode.Range(0, 0, doc.lineCount, 0),
    "source.fixAll.mydumper-lint",
  );
  assert.ok(fixAll, "a source.fixAll.mydumper-lint action");
  const fixed = cp.execFileSync(
    bundled,
    ["check", "--fix", "--no-config", "--mydumper-version", "v1.0.5-1", "--stdin-filename", "backup.cnf", "-"],
    { input: doc.getText(), encoding: "utf8" },
  );
  assert.strictEqual(fixAll.edit.get(uri)[0].newText, fixed);

  // The quick fix, applied: the problem goes away.
  const actions = await vscode.commands.executeCommand("vscode.executeCodeActionProvider", uri, d.range, "quickfix");
  const titles = actions.map((a) => a.title);
  const fix = actions.find((a) => a.title === "Fix MDL102: Remove the whitespace");
  assert.ok(fix, `the fix among ${titles}`);
  assert.ok(fix.isPreferred, "a safe fix is the preferred action");
  // MDL102 is unsuppressible: GLib rejects the file before any directive is read.
  assert.ok(!titles.some((t) => /^Disable MDL102/.test(t)), `no suppression among ${titles}`);
  assert.ok(await vscode.workspace.applyEdit(fix.edit));
  assert.doesNotMatch(doc.getText(), /^[ \t]+$/m);
  await until(uri, "");
}

module.exports = { run };
