// Runs the packaged extension in a real VS Code (make vscode-test):
//
//   VSIX=mydumper-lint-linux-x64.vsix node test/vscode/run.js
//
// Downloads VS Code (VSCODE_TEST_VERSION, default "stable") into .vscode-test/,
// installs the VSIX into a fresh profile, opens a workspace with a .cnf file,
// and runs suite.js inside VS Code. mydumper-lint is removed from the PATH VS
// Code sees, so the diagnostics can only come from the executable the VSIX
// bundles. On Linux without a display, run it under xvfb-run.
"use strict";

const cp = require("node:child_process");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { downloadAndUnzipVSCode, resolveCliArgsFromVSCodeExecutablePath, runTests } = require("@vscode/test-electron");

const root = path.resolve(__dirname, "..", "..");
const exe = process.platform === "win32" ? "mydumper-lint.exe" : "mydumper-lint";

// PATH without the directories that hold a mydumper-lint executable.
function pathWithoutMydumperLint() {
  return (process.env.PATH || "")
    .split(path.delimiter)
    .filter((dir) => dir && !fs.existsSync(path.join(dir, exe)))
    .join(path.delimiter);
}

async function main() {
  const vsix = path.resolve(root, process.env.VSIX || "mydumper-lint.vsix");
  if (!fs.existsSync(vsix)) {
    throw new Error(`${vsix} does not exist: run make vscode-host first`);
  }
  const vscodeExecutablePath = await downloadAndUnzipVSCode({
    version: process.env.VSCODE_TEST_VERSION || "stable",
    cachePath: path.join(root, ".vscode-test"),
  });

  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "mydumper-lint-vscode-"));
  const profile = [`--extensions-dir=${path.join(tmp, "extensions")}`, `--user-data-dir=${path.join(tmp, "user-data")}`];
  const [cli] = resolveCliArgsFromVSCodeExecutablePath(vscodeExecutablePath);
  cp.execFileSync(cli, [...profile, "--install-extension", vsix], {
    stdio: "inherit",
    shell: process.platform === "win32",
  });

  const workspace = path.join(tmp, "workspace");
  fs.mkdirSync(workspace);
  fs.writeFileSync(path.join(workspace, ".mydumper-lint.yaml"), "mydumper-version: v1.0.5-1\n");
  // The protocol messages go to the extension's output channel, printed on failure.
  fs.mkdirSync(path.join(workspace, ".vscode"));
  fs.writeFileSync(path.join(workspace, ".vscode", "settings.json"), '{ "mydumperLint.trace.server": "verbose" }\n');
  // Line 3 holds only spaces (MDL102).
  fs.writeFileSync(path.join(workspace, "backup.cnf"), "[mydumper]\nthreads=4\n  \n[myloader]\nthreads=4\n");

  try {
    await runTests({
      vscodeExecutablePath,
      extensionDevelopmentPath: path.join(__dirname, "harness"),
      extensionTestsPath: path.join(__dirname, "suite.js"),
      launchArgs: [workspace, ...profile],
      extensionTestsEnv: { PATH: pathWithoutMydumperLint() },
    });
  } catch (err) {
    printExtensionLogs(path.join(tmp, "user-data", "logs"));
    throw err;
  } finally {
    fs.rmSync(tmp, { recursive: true, force: true, maxRetries: 3 });
  }
}

// VS Code writes every output channel to a log file: print the extension's
// (what it started, the server's stderr) and the extension host's.
function printExtensionLogs(dir) {
  if (!fs.existsSync(dir)) {
    return;
  }
  for (const entry of fs.readdirSync(dir, { recursive: true })) {
    const file = path.join(dir, entry);
    if (/mydumper-lint[^/\\]*\.log$|exthost\.log$/.test(entry) && fs.statSync(file).isFile()) {
      console.error(`--- ${entry}\n${fs.readFileSync(file, "utf8")}`);
    }
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
