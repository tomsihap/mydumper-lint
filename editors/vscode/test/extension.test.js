// Tests the extension's wiring without VS Code: `vscode` and the language
// client are replaced by stubs. The server itself is tested in Go
// (internal/lsp, internal/cli).
"use strict";

const test = require("node:test");
const assert = require("node:assert");
const Module = require("node:module");
const manifest = require("../package.json");

function load(config = {}, { failStart = false } = {}) {
  const calls = { clients: [], commands: {}, errors: [], configListeners: [] };
  class FakeClient {
    constructor(id, name, server, options) {
      Object.assign(this, { id, name, server, options, started: 0, stopped: 0 });
      calls.clients.push(this);
    }
    async start() {
      this.started++;
      if (failStart) throw new Error("spawn mydumper-lint ENOENT");
    }
    async stop() {
      this.stopped++;
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
    window: { showErrorMessage: (m) => calls.errors.push(m) },
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

const context = () => ({ subscriptions: [] });

test("starts `mydumper-lint server` for .cnf files", async () => {
  const { ext, calls } = load();
  const ctx = context();
  await ext.activate(ctx);
  assert.strictEqual(calls.clients.length, 1);
  const c = calls.clients[0];
  assert.deepStrictEqual(c.server.run, { command: "mydumper-lint", args: ["server"], transport: 0 });
  assert.deepStrictEqual(c.options.documentSelector, [{ scheme: "file", pattern: "**/*.cnf" }]);
  assert.deepStrictEqual(c.options.initializationOptions, { mydumperLint: { mydumperVersion: "" } });
  assert.strictEqual(c.options.synchronize.configurationSection, "mydumperLint");
  assert.strictEqual(c.options.synchronize.fileEvents.glob, "**/.mydumper-lint.{yaml,yml}");
  assert.strictEqual(c.started, 1);
  assert.strictEqual(ctx.subscriptions.length, 2);
  await ext.deactivate();
  assert.strictEqual(c.stopped, 1);
});

test("uses the configured executable and mydumper version", async () => {
  const { ext, calls } = load({ path: "/opt/bin/mydumper-lint", mydumperVersion: "v0.19.3-3" });
  await ext.activate(context());
  const c = calls.clients[0];
  assert.strictEqual(c.server.run.command, "/opt/bin/mydumper-lint");
  assert.deepStrictEqual(c.options.initializationOptions, { mydumperLint: { mydumperVersion: "v0.19.3-3" } });
});

test("restarts on command and when the executable changes", async () => {
  const { ext, calls } = load();
  await ext.activate(context());
  await calls.commands["mydumperLint.restart"]();
  assert.strictEqual(calls.clients.length, 2);
  assert.strictEqual(calls.clients[0].stopped, 1);
  assert.strictEqual(calls.clients[1].started, 1);
  await calls.configListeners[0]({ affectsConfiguration: (k) => k === "mydumperLint.path" });
  assert.strictEqual(calls.clients.length, 3);
  await calls.configListeners[0]({ affectsConfiguration: () => false });
  assert.strictEqual(calls.clients.length, 3);
});

test("says how to fix a missing executable", async () => {
  const { ext, calls } = load({}, { failStart: true });
  await ext.activate(context());
  assert.strictEqual(calls.errors.length, 1);
  assert.match(calls.errors[0], /mydumper-lint server.*ENOENT.*mydumperLint\.path/);
});

test("the manifest declares every setting and command the code uses", () => {
  const props = manifest.contributes.configuration.properties;
  for (const key of ["mydumperLint.path", "mydumperLint.mydumperVersion", "mydumperLint.trace.server"]) {
    assert.ok(props[key], key);
  }
  assert.deepStrictEqual(manifest.contributes.commands.map((c) => c.command), ["mydumperLint.restart"]);
  assert.ok(manifest.activationEvents.includes("workspaceContains:**/*.cnf"));
  assert.strictEqual(manifest.main, "./extension.js");
});
