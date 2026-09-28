// Runs the playground's examples through the WebAssembly build, the way the
// page does, and checks the rules each one triggers (`expect`). Run after
// `make playground`: node web/playground/examples.test.mjs
import { readFile } from "node:fs/promises";
import "./wasm_exec.js";
import { examples } from "./examples.js";

const go = new globalThis.Go();
const wasm = await readFile(new URL("./mydumper-lint.wasm", import.meta.url));
const { instance } = await WebAssembly.instantiate(wasm, go.importObject);
go.run(instance);
const api = globalThis.mydumperLint;

let failed = 0;
const fail = (msg) => {
  failed++;
  console.error("FAIL " + msg);
};

const versions = JSON.parse(api.versions());
if (!versions.some((v) => v.default)) fail("versions: no default version");
for (const x of examples) {
  if (!versions.some((v) => v.tag === x.version)) fail(`${x.title}: unknown version ${x.version}`);
  const text = x.crlf ? x.content.replace(/\n/g, "\r\n") : x.content;
  const r = JSON.parse(api.analyze(new TextEncoder().encode(text), "backup.cnf", x.version));
  if (r.error) {
    fail(`${x.title}: ${r.error}`);
    continue;
  }
  const got = [...new Set(r.check.files[0].diagnostics.map((d) => d.id))].sort().join(" ");
  const want = [...x.expect].sort().join(" ");
  if (got !== want) fail(`${x.title}: got [${got}], want [${want}]`);
  else console.log(`ok   ${x.title}: ${got}`);
}
// Bytes a textarea cannot hold go through untouched.
const r = JSON.parse(api.analyze(Uint8Array.from([0xef, 0xbb, 0xbf, ...new TextEncoder().encode("[mydumper]\n")]), "x.cnf", "v1.0.5-1"));
if (!r.check.files[0].diagnostics.some((d) => d.id === "MDL101")) fail("BOM: MDL101 not reported");
if (!api.build.startsWith("mydumper-lint ")) fail("build: " + api.build);
if (failed) process.exit(1);
console.log(`ok   ${examples.length} examples, BOM, build ${api.build}`);
process.exit(0);
