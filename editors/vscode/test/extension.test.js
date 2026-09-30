// Tests the extension's wiring without VS Code: `vscode` and the language
// client are replaced by stubs. The server itself is tested in Go
// (internal/lsp, internal/cli); test/vscode/ runs the extension in VS Code.
"use strict";

const test = require("node:test");
const assert = require("node:assert");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const Module = require("node:module");
const manifest = require("../package.json");

const exeName = process.platform === "win32" ? "mydumper-lint.exe" : "mydumper-lint";

function load(config = {}, { failStart = false } = {}) {
  const calls = { clients: [], commands: {}, errors: [], configListeners: [], output: [] };
  class FakeClient {
    constructor(id, name, server, options) {
      Object.assign(this, { id, name, server, options, started: 0, stopped: 0, running: false });
      calls.clients.push(this);
    }
    async start() {
      this.started++;
      if (failStart) throw new Error("spawn mydumper-lint ENOENT");
      this.running = true;
    }
    isRunning() {
      return this.running;
    }
    async stop() {
      if (!this.running) throw new Error("Client is not running and can't be stopped");
      this.stopped++;
      this.running = false;
    }
  }
  const vscode = {
    workspace: {
      getConfiguration: (section) => {
        assert.strictEqual(section, "mydumperLint");
        return { get: (key) => config[key] };
      },
      createFileSystemWatcher: (glob) => ({ glob }),
      onDidChangeConfiguration: (fn) => {
        calls.configListeners.push(fn);
        return { dispose() {} };
      },
    },
    commands: {
      registerCommand: (name, fn) => {
        calls.commands[name] = fn;
        return { dispose() {} };
      },
      executeCommand: (name) => calls.commands[name](),
    },
    window: {
      showErrorMessage: (m) => calls.errors.push(m),
      // A LogOutputChannel: the language client calls .error(), .info()…
      createOutputChannel: (name, options) => {
        assert.deepStrictEqual(options, { log: true }, "the language client needs a log channel");
        const log = (level) => (line) => calls.output.push(`${level} ${line}`);
        return { name, info: log("info"), error: log("error"), warn: log("warn"), trace: log("trace"), dispose() {} };
      },
    },
  };
  const stubs = { vscode, "vscode-languageclient/node": { LanguageClient: FakeClient, TransportKind: { stdio: 0 } } };
  const original = Module._load;
  Module._load = function (request, ...rest) {
    return request in stubs ? stubs[request] : original.call(this, request, ...rest);
  };
  try {
    delete require.cache[require.resolve("../extension.js")];
    return { ext: require("../extension.js"), calls };
  } finally {
    Module._load = original;
  }
}

// An installed extension's directory, with or without a bundled executable.
function extensionDir(t, { binary = false, mode = 0o755 } = {}) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "mydumper-lint-ext-"));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  if (binary) {
    fs.mkdirSync(path.join(dir, "bin"));
    fs.writeFileSync(path.join(dir, "bin", exeName), "#!/bin/sh\n", { mode });
  }
  return dir;
}

const context = (extensionPath) => ({ subscriptions: [], extensionPath });

test("starts `mydumper-lint server` from PATH for .cnf files", async (t) => {
  const { ext, calls } = load();
  const ctx = context(extensionDir(t));
  await ext.activate(ctx);
  assert.strictEqual(calls.clients.length, 1);
  const c = calls.clients[0];
  assert.deepStrictEqual(c.server.run, { command: "mydumper-lint", args: ["server"], transport: 0 });
  assert.deepStrictEqual(c.options.documentSelector, [{ scheme: "file", pattern: "**/*.cnf" }]);
  assert.deepStrictEqual(c.options.initializationOptions, { mydumperLint: { mydumperVersion: "" } });
  assert.strictEqual(c.options.synchronize.configurationSection, "mydumperLint");
  assert.strictEqual(c.options.synchronize.fileEvents.glob, "**/.mydumper-lint.{yaml,yml}");
  assert.strictEqual(c.options.outputChannel.name, "mydumper-lint");
  assert.deepStrictEqual(calls.output, ["info Starting mydumper-lint server (found in PATH)."]);
  assert.strictEqual(c.started, 1);
  assert.strictEqual(ctx.subscriptions.length, 3);
  await ext.deactivate();
  assert.strictEqual(c.stopped, 1);
});

test("prefers the executable bundled with the extension to PATH", async (t) => {
  const { ext, calls } = load();
  const dir = extensionDir(t, { binary: true });
  await ext.activate(context(dir));
  assert.strictEqual(calls.clients[0].server.run.command, path.join(dir, "bin", exeName));
  assert.match(calls.output[0], /\(bundled with the extension\)\.$/);
});

test("restores the execute bit of a bundled executable unpacked without it", { skip: process.platform === "win32" }, (t) => {
  const { ext } = load();
  const dir = extensionDir(t, { binary: true, mode: 0o644 });
  const exe = path.join(dir, "bin", exeName);
  assert.strictEqual(ext.executable(dir).command, exe);
  assert.strictEqual(fs.statSync(exe).mode & 0o777, 0o755);
});

test("the mydumperLint.path setting wins over the bundled executable", async (t) => {
  const { ext, calls } = load({ path: "/opt/bin/mydumper-lint", mydumperVersion: "v0.19.3-3" });
  await ext.activate(context(extensionDir(t, { binary: true })));
  const c = calls.clients[0];
  assert.strictEqual(c.server.run.command, "/opt/bin/mydumper-lint");
  assert.match(calls.output[0], /\(the mydumperLint\.path setting\)\.$/);
  assert.deepStrictEqual(c.options.initializationOptions, { mydumperLint: { mydumperVersion: "v0.19.3-3" } });
});

test("restarts on command and when the executable changes", async (t) => {
  const { ext, calls } = load();
  await ext.activate(context(extensionDir(t)));
  await calls.commands["mydumperLint.restart"]();
  assert.strictEqual(calls.clients.length, 2);
  assert.strictEqual(calls.clients[0].stopped, 1);
  assert.strictEqual(calls.clients[1].started, 1);
  await calls.configListeners[0]({ affectsConfiguration: (k) => k === "mydumperLint.path" });
  assert.strictEqual(calls.clients.length, 3);
  await calls.configListeners[0]({ affectsConfiguration: () => false });
  assert.strictEqual(calls.clients.length, 3);
});

test("says how to fix a missing executable, and restarts after the fix", async (t) => {
  const { ext, calls } = load({}, { failStart: true });
  await ext.activate(context(extensionDir(t)));
  assert.strictEqual(calls.errors.length, 1);
  assert.match(calls.errors[0], /"mydumper-lint server" \(found in PATH\).*ENOENT.*Install mydumper-lint, or set mydumperLint\.path/);
  assert.strictEqual(calls.output[1], `error ${calls.errors[0]}`);
  // The server never started: setting mydumperLint.path must still restart it.
  await calls.configListeners[0]({ affectsConfiguration: (k) => k === "mydumperLint.path" });
  assert.strictEqual(calls.clients.length, 2);
  assert.strictEqual(calls.clients[1].started, 1);
  await ext.deactivate();
});

test("a failing configured executable points at the setting", async (t) => {
  const { ext, calls } = load({ path: "/nowhere/mydumper-lint" }, { failStart: true });
  await ext.activate(context(extensionDir(t)));
  assert.match(calls.errors[0], /\(the mydumperLint\.path setting\).*Check mydumperLint\.path, or clear it/);
});

test("the manifest declares every setting and command the code uses", () => {
  const props = manifest.contributes.configuration.properties;
  for (const key of ["mydumperLint.path", "mydumperLint.mydumperVersion", "mydumperLint.trace.server"]) {
    assert.ok(props[key], key);
  }
  // Empty: the bundled executable, else PATH.
  assert.strictEqual(props["mydumperLint.path"].default, "");
  assert.deepStrictEqual(manifest.contributes.commands.map((c) => c.command), ["mydumperLint.restart"]);
  assert.ok(manifest.activationEvents.includes("workspaceContains:**/*.cnf"));
  // esbuild bundles extension.js and the language client (npm run bundle).
  assert.strictEqual(manifest.main, "./dist/extension.js");
  // Runs where the files are (Remote-SSH, WSL, containers), with that
  // machine's platform-specific package.
  assert.deepStrictEqual(manifest.extensionKind, ["workspace"]);
  assert.strictEqual(manifest.icon, "images/icon.png");
});
