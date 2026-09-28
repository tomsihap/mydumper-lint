// mydumper-lint playground: the linter compiled to WebAssembly, and a page
// around it. The analysis always runs on the exact bytes of the file: a
// textarea turns CRLF into LF and cannot hold a BOM or a NUL, so the page
// keeps the bytes itself and rebuilds them from the text when you type.

import { examples } from "./examples.js";

const $ = (id) => document.getElementById(id);
const el = {
  version: $("version"), example: $("example"), open: $("open"), file: $("file"),
  crlf: $("crlf"), bom: $("bom"), share: $("share"), name: $("name"), note: $("bytes-note"),
  source: $("source"), gutter: $("gutter"), verdict: $("verdict"), count: $("count"),
  problems: $("panel-problems"), sees: $("panel-sees"), unsafe: $("unsafe"), apply: $("apply"),
  download: $("download"), notes: $("fix-notes"), diff: $("diff"), command: $("command"),
  build: $("build"), toast: $("toast"),
};

const DOCS = "https://github.com/tomsihap/mydumper-lint/blob/main/docs/rules/";
const BOM = [0xef, 0xbb, 0xbf];
const encoder = new TextEncoder();
const lenient = new TextDecoder("utf-8", { ignoreBOM: true });
const strict = new TextDecoder("utf-8", { ignoreBOM: true, fatal: true });

const state = {
  bytes: new Uint8Array(),
  exact: false, // the bytes came from a file the editor cannot show exactly
  result: null,
  rules: new Map(),
};

// ---- bytes <-> text ---------------------------------------------------------

function bytesFromText(text) {
  const body = encoder.encode(el.crlf.checked ? text.replace(/\n/g, "\r\n") : text);
  if (!el.bom.checked) return body;
  const out = new Uint8Array(body.length + 3);
  out.set(BOM);
  out.set(body, 3);
  return out;
}

// setBytes shows bytes in the editor and says what the editor cannot show.
function setBytes(bytes) {
  const hasBom = bytes.length >= 3 && bytes[0] === 0xef && bytes[1] === 0xbb && bytes[2] === 0xbf;
  const body = hasBom ? bytes.subarray(3) : bytes;
  const text = lenient.decode(body);
  const crlf = text.includes("\r\n");
  const hidden = [];
  try { strict.decode(body); } catch { hidden.push("invalid UTF-8"); }
  if (body.includes(0)) hidden.push("NUL bytes");
  if (/\r(?!\n)/.test(text)) hidden.push("carriage returns that do not end a line");
  if (crlf && /(^|[^\r])\n/.test(text)) hidden.push("mixed line endings");
  el.bom.checked = hasBom;
  el.crlf.checked = crlf;
  el.source.value = text.replace(/\r\n/g, "\n");
  state.bytes = bytes;
  state.exact = hidden.length > 0;
  el.note.hidden = !state.exact;
  el.note.textContent = state.exact
    ? `Checked byte for byte. The file has ${hidden.join(", ")}, which the editor cannot show: editing replaces them.`
    : "";
  updateGutter();
  analyze();
}

function onEdit() {
  state.bytes = bytesFromText(el.source.value);
  if (state.exact) {
    state.exact = false;
    el.note.hidden = true;
  }
  updateGutter();
  scheduleAnalyze();
}

function updateGutter() {
  const n = el.source.value.split("\n").length;
  let s = "";
  for (let i = 1; i <= n; i++) s += i + "\n";
  el.gutter.textContent = s;
  el.gutter.scrollTop = el.source.scrollTop;
}

// ---- analysis ---------------------------------------------------------------

let timer = 0;
function scheduleAnalyze() {
  clearTimeout(timer);
  timer = setTimeout(analyze, 200);
}

function analyze() {
  if (!window.mydumperLint) return;
  const r = JSON.parse(window.mydumperLint.analyze(state.bytes, el.name.value || "input.cnf", el.version.value));
  state.result = r;
  render(r);
}

function render(r) {
  if (r.error) {
    verdict("bad", "mydumper-lint cannot check this file", r.error);
    el.problems.replaceChildren();
    el.count.textContent = "";
    return;
  }
  const file = r.check.files[0];
  const inspect = r.inspect || {};
  renderVerdict(file, inspect, r.notices || []);
  renderProblems(file);
  renderFixes();
  renderSees(inspect);
  el.command.textContent = r.command;
}

function renderVerdict(file, inspect, notices) {
  const ds = file.diagnostics;
  const errors = ds.filter((d) => d.severity === "error").length;
  const others = ds.length - errors;
  let kind, title, detail;
  if (!file.loadable) {
    kind = "bad";
    title = "GLib rejects this file: mydumper ignores all of it";
    const err = inspect.error;
    detail = err
      ? `mydumper only logs "Failed to load config file ${el.name.value}: ${err.message}" (line ${err.line}), runs with its defaults and exits 0.`
      : "mydumper only logs a warning, runs with its defaults and exits 0.";
  } else if (inspect.health === "fatal-at-startup") {
    kind = "bad";
    title = "mydumper aborts at startup with this file";
    detail = "See the problems below.";
  } else if (errors > 0) {
    kind = "warn";
    title = `mydumper loads this file, but ${plural(errors, "problem")} change${errors === 1 ? "s" : ""} what it does`;
    detail = "Each one says what mydumper will do instead.";
  } else if (others > 0) {
    kind = "ok";
    title = "mydumper applies this file as written";
    detail = `${plural(others, "note")} to review below.`;
  } else {
    kind = "ok";
    title = "No problems: mydumper applies this file as written";
    detail = `Checked against mydumper ${file.mydumper_version}.`;
  }
  verdict(kind, title, detail, notices);
  el.count.textContent = ds.length ? String(ds.length) : "";
}

function verdict(kind, title, detail, notices = []) {
  el.verdict.className = "verdict " + kind;
  const parts = [h("strong", {}, title), h("span", {}, inlineCode(detail))];
  for (const n of notices) parts.push(h("span", { class: "notice" }, n));
  el.verdict.replaceChildren(...parts);
}

function renderProblems(file) {
  if (file.diagnostics.length === 0) {
    el.problems.replaceChildren(h("p", { class: "empty" }, "Nothing to report."));
    return;
  }
  const lines = splitLines(state.bytes);
  const items = file.diagnostics.map((d) => {
    const s = d.range.start, e = d.range.end;
    const rule = state.rules.get(d.id);
    const head = h("div", { class: "d-head" },
      h("span", { class: "sev " + d.severity }, d.severity),
      h("a", { href: (rule && rule.url) || DOCS + d.id + ".md", target: "_blank", rel: "noopener", title: rule ? rule.summary : "" }, d.id),
      h("span", { class: "rule-name" }, d.name),
      h("button", { type: "button", class: "loc", title: "Show in the editor", onclick: () => select(s, e) }, `line ${s.line}, col ${s.col}`),
    );
    const body = [head, h("p", { class: "msg" }, inlineCode(d.message))];
    if (d.consequence) body.push(h("p", { class: "consequence" }, inlineCode(d.consequence)));
    body.push(excerpt(lines, s, e));
    if (d.fix) body.push(h("p", { class: "fix " + d.fix.applicability }, (d.fix.applicability === "safe" ? "Safe fix: " : "Unsafe fix: ") + d.fix.description));
    return h("li", { class: "diag" }, ...body);
  });
  el.problems.replaceChildren(h("ol", { class: "diags" }, ...items));
}

function renderFixes() {
  const r = state.result;
  if (!r || r.error) return;
  const f = el.unsafe.checked ? r.unsafe : r.safe;
  const notes = [];
  if (f.failure) notes.push(h("p", { class: "note bad" }, "Fixing failed: " + f.failure));
  for (const n of f.notes || []) notes.push(h("p", { class: "note" }, n));
  el.notes.replaceChildren(...notes);
  el.apply.disabled = el.download.disabled = !f.diff;
  if (!f.diff) {
    el.diff.replaceChildren(h("span", { class: "empty" }, el.unsafe.checked ? "No fix to apply." : "No safe fix to apply. Unsafe fixes, if any, need the box above."));
    return;
  }
  const out = f.diff.split("\n").filter((l) => !l.startsWith("diff ")).map((l) => {
    const cls = l.startsWith("+++") || l.startsWith("---") ? "meta" : l.startsWith("@@") ? "hunk" : l.startsWith("+") ? "add" : l.startsWith("-") ? "del" : "";
    const text = cls === "meta" || cls === "hunk" ? l : l.slice(0, 1) + visibleEdges(l.slice(1));
    return h("span", { class: cls }, text + "\n");
  });
  el.diff.replaceChildren(...out);
}

const reasons = {
  effective: "applied",
  "file-rejected": "ignored: GLib rejects the file",
  "unknown-group": "ignored: nobody reads this group",
  localized: "hidden by GLib: localized key",
  "shadowed-by-duplicate": "replaced by a later duplicate",
  "connection-key": "read by the MySQL client library, not by mydumper",
  "unknown-option": "unknown option",
  "fatal-at-startup": "makes mydumper abort at startup",
  "unknown-table-key": "ignored: not a table key",
  "masquerade-fallback-identity": "unknown masking function: the column is not masked",
  "consumed-as-option-value": "swallowed as the value of an option",
  "after-end-of-options": "ignored: after a “--” value",
};

// reasonText explains an entry. [client] of a rejected file still counts:
// the MySQL client library reads the file with its own parser.
function reasonText(g, e) {
  if (g.kind === "client" && e.reason === "file-rejected") {
    return "ignored by mydumper; the MySQL client library still reads [client] with its own parser";
  }
  return reasons[e.reason] || e.reason;
}

function renderSees(doc) {
  const parts = [];
  parts.push(h("p", {}, doc.loadable
    ? `GLib loads the file (mydumper ${doc.mydumper_version}${doc.preprocessor ? "" : ", no pre-processor in this version"}).`
    : `GLib rejects the file at line ${doc.error.line}: ${doc.error.message}. mydumper applies none of it.`));
  if ((doc.rewritten_lines || []).length) {
    parts.push(h("h3", {}, "Lines mydumper rewrites before GLib reads them"));
    parts.push(table(["Line", "In the file", "What GLib reads"], doc.rewritten_lines.map((l) => [
      String(l.line), code(visibleEdges(l.original)),
      h("span", {}, code(visibleEdges(l.rewritten)), l.state_leak ? h("span", { class: "hint" }, ` state carried over from line ${l.leak_origin}`) : ""),
    ])));
  }
  parts.push(h("h3", {}, "Effective configuration"));
  if (!(doc.model || []).length) parts.push(h("p", { class: "empty" }, "No groups."));
  for (const g of doc.model || []) {
    const who = g.kind === "unknown" ? "read by nobody" : g.kind + (g.tool ? ", " + g.tool : "");
    parts.push(h("h4", {}, code("[" + g.name + "]"), h("span", { class: "hint" }, " " + who)));
    if (!g.entries.length) continue;
    parts.push(table(["Line", "Key", "Value", ""], g.entries.map((e) => [
      String(e.line), code(visibleEdges(e.key)), code(visibleEdges(e.value)),
      h("span", { class: e.effective ? "ok" : "bad" }, (e.effective ? "✓ " : "✗ ") + reasonText(g, e)),
    ])));
  }
  el.sees.replaceChildren(...parts);
}

// ---- positions and excerpts ------------------------------------------------

// splitLines returns the lines of the file as byte arrays, terminators
// excluded (a CR before the LF is kept, to be shown).
function splitLines(bytes) {
  const out = [];
  let start = 0;
  for (let i = 0; i < bytes.length; i++) {
    if (bytes[i] === 0x0a) {
      out.push(bytes.subarray(start, i));
      start = i + 1;
    }
  }
  out.push(bytes.subarray(start));
  return out;
}

// chars cuts a line into what the linter counts as columns: code points,
// an invalid byte counting as one (design §4.6).
function chars(line) {
  const out = [];
  for (let i = 0; i < line.length;) {
    const b = line[i];
    const n = b < 0x80 ? 1 : b >= 0xf0 ? 4 : b >= 0xe0 ? 3 : b >= 0xc0 ? 2 : 1;
    let s = null;
    if (n > 1 && i + n <= line.length) {
      try { s = strict.decode(line.subarray(i, i + n)); } catch { s = null; }
    }
    if (n === 1 || s === null) {
      out.push({ bytes: [b], text: b < 0x80 ? String.fromCharCode(b) : null });
      i += 1;
    } else {
      out.push({ bytes: Array.from(line.subarray(i, i + n)), text: s });
      i += n;
    }
  }
  return out;
}

function show(c, first) {
  if (first && c.text === "\uFEFF") return ["⟨BOM⟩", "inv"];
  if (c.text === null) return ["\\x" + c.bytes[0].toString(16).padStart(2, "0"), "inv"];
  switch (c.text) {
    case " ": return ["·", "ws"];
    case "\t": return ["→", "ws"];
    case "\r": return ["␍", "inv"];
    case "\0": return ["␀", "inv"];
  }
  const code = c.text.codePointAt(0);
  if (code < 0x20 || code === 0x7f) return ["\\x" + code.toString(16).padStart(2, "0"), "inv"];
  return [c.text, ""];
}

// visible renders a string with its invisible characters shown.
function visible(s) {
  return marks(s.replace(/ /g, "·").replace(/\t/g, "→"));
}

// visibleEdges shows the spaces and tabs that start or end a string (all of
// them in a blank string), and the other invisible characters.
function visibleEdges(s) {
  const m = /^([ \t]*)([\s\S]*?)([ \t]*)$/.exec(s);
  const ws = (w) => w.replace(/ /g, "·").replace(/\t/g, "→");
  return marks(ws(m[1]) + m[2] + ws(m[3]));
}

function marks(s) {
  return s.replace(/\r/g, "␍").replace(/\0/g, "␀").replace(/\uFEFF/g, "⟨BOM⟩");
}

function excerpt(lines, s, e) {
  const line = lines[s.line - 1];
  if (!line) return h("span");
  const cs = chars(line);
  const from = s.col - 1;
  const to = e.line === s.line ? e.col - 1 : cs.length;
  const out = [h("span", { class: "lno" }, String(s.line).padStart(4) + " │ ")];
  cs.forEach((c, i) => {
    const [text, cls] = show(c, s.line === 1 && i === 0);
    if (i === from && to === from) out.push(h("span", { class: "caret" }, "▏"));
    const span = h("span", { class: cls }, text);
    out.push(i >= from && i < to ? h("mark", {}, span) : span);
  });
  if (from >= cs.length) out.push(h("span", { class: "caret" }, "▏"));
  return h("pre", { class: "excerpt" }, ...out);
}

// select puts the cursor on a diagnostic in the editor.
function select(s, e) {
  const text = el.source.value;
  const bomShift = el.bom.checked ? 1 : 0;
  const index = (p) => {
    let i = 0;
    for (let l = 1; l < p.line; l++) {
      const next = text.indexOf("\n", i);
      if (next < 0) return text.length;
      i = next + 1;
    }
    let col = p.col - 1 - (p.line === 1 ? bomShift : 0);
    for (const ch of text.slice(i)) {
      if (col <= 0 || ch === "\n") break;
      i += ch.length;
      col--;
    }
    return i;
  };
  el.source.focus();
  el.source.setSelectionRange(index(s), Math.max(index(s), index(e)));
  const lineHeight = parseFloat(getComputedStyle(el.source).lineHeight) || 20;
  el.source.scrollTop = Math.max(0, (s.line - 3) * lineHeight);
}

// ---- share links -------------------------------------------------------------

async function pack(bytes) {
  if (!("CompressionStream" in window)) return "r=" + b64(bytes);
  const stream = new Blob([bytes]).stream().pipeThrough(new CompressionStream("deflate-raw"));
  return "z=" + b64(new Uint8Array(await new Response(stream).arrayBuffer()));
}

async function unpack(params) {
  if (params.has("z")) {
    const stream = new Blob([unb64(params.get("z"))]).stream().pipeThrough(new DecompressionStream("deflate-raw"));
    return new Uint8Array(await new Response(stream).arrayBuffer());
  }
  return params.has("r") ? unb64(params.get("r")) : null;
}

function b64(bytes) {
  let s = "";
  for (let i = 0; i < bytes.length; i += 0x8000) s += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  return btoa(s).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

function unb64(s) {
  const bin = atob(s.replace(/-/g, "+").replace(/_/g, "/"));
  return Uint8Array.from(bin, (c) => c.charCodeAt(0));
}

async function share() {
  const params = new URLSearchParams();
  params.set("v", el.version.value);
  params.set("n", el.name.value);
  const [k, v] = (await pack(state.bytes)).split("=");
  params.set(k, v);
  history.replaceState(null, "", "#" + params.toString());
  const secret = /^\s*password\s*=/m.test(el.source.value);
  try {
    await navigator.clipboard.writeText(location.href);
    toast(secret ? "Link copied. It contains a password: remove it before sharing." : "Link copied: it contains the file.");
  } catch {
    toast("Link ready in the address bar.");
  }
}

// ---- small DOM helpers -------------------------------------------------------

function h(tag, attrs = {}, ...children) {
  const n = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (k.startsWith("on")) n.addEventListener(k.slice(2), v);
    else n.setAttribute(k, v);
  }
  for (const c of children.flat()) if (c !== null && c !== undefined && c !== "") n.append(c instanceof Node ? c : String(c));
  return n;
}

const code = (s) => h("code", {}, s);

// inlineCode turns `text` into <code>text</code>, and shows the invisible
// characters of what GLib quotes (“  = 1”).
function inlineCode(s) {
  return s.split(/(`[^`]*`|“[^”]*”)/).map((p) => {
    if (p.length > 1 && p.startsWith("`") && p.endsWith("`")) return code(visible(p.slice(1, -1)));
    if (p.startsWith("“") && p.endsWith("”")) return "“" + visible(p.slice(1, -1)) + "”";
    return p;
  });
}

function table(head, rows) {
  return h("table", {},
    h("thead", {}, h("tr", {}, ...head.map((t) => h("th", {}, t)))),
    h("tbody", {}, ...rows.map((r) => h("tr", {}, ...r.map((c) => h("td", {}, c))))));
}

function plural(n, word) {
  return `${n} ${word}${n === 1 ? "" : "s"}`;
}

let toastTimer = 0;
function toast(msg) {
  el.toast.textContent = msg;
  el.toast.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => (el.toast.hidden = true), 3500);
}

function download() {
  const r = state.result;
  const f = el.unsafe.checked ? r.unsafe : r.safe;
  const url = URL.createObjectURL(new Blob([unb64std(f.output)], { type: "application/octet-stream" }));
  const a = h("a", { href: url, download: el.name.value || "input.cnf" });
  a.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

// Go encodes []byte as standard base64.
function unb64std(s) {
  return Uint8Array.from(atob(s || ""), (c) => c.charCodeAt(0));
}

// ---- wiring --------------------------------------------------------------------

function setupTabs() {
  const tabs = [...document.querySelectorAll('[role="tab"]')];
  const activate = (t) => {
    for (const x of tabs) {
      const on = x === t;
      x.setAttribute("aria-selected", String(on));
      x.tabIndex = on ? 0 : -1;
      $(x.getAttribute("aria-controls")).hidden = !on;
    }
    t.focus();
  };
  tabs.forEach((t, i) => {
    t.tabIndex = i === 0 ? 0 : -1;
    t.addEventListener("click", () => activate(t));
    t.addEventListener("keydown", (e) => {
      const d = e.key === "ArrowRight" ? 1 : e.key === "ArrowLeft" ? -1 : 0;
      if (d) activate(tabs[(i + d + tabs.length) % tabs.length]);
    });
  });
}

function setupExamples() {
  examples.forEach((x, i) => el.example.append(h("option", { value: String(i) }, x.title)));
  el.example.addEventListener("change", () => {
    const x = examples[Number(el.example.value)];
    if (!x) return;
    loadExample(x);
  });
}

function loadExample(x) {
  if (x.version && [...el.version.options].some((o) => o.value === x.version)) el.version.value = x.version;
  el.name.value = x.name || "backup.cnf";
  el.crlf.checked = !!x.crlf;
  el.bom.checked = !!x.bom;
  el.source.value = x.content;
  onEdit();
  analyze();
}

function setupFile() {
  el.open.addEventListener("click", () => el.file.click());
  el.file.addEventListener("change", async () => {
    const f = el.file.files[0];
    if (!f) return;
    el.name.value = f.name;
    setBytes(new Uint8Array(await f.arrayBuffer()));
    el.file.value = "";
  });
  el.source.addEventListener("dragover", (e) => e.preventDefault());
  el.source.addEventListener("drop", async (e) => {
    const f = e.dataTransfer.files[0];
    if (!f) return;
    e.preventDefault();
    el.name.value = f.name;
    setBytes(new Uint8Array(await f.arrayBuffer()));
  });
}

async function loadWasm() {
  const go = new Go();
  let instance;
  try {
    ({ instance } = await WebAssembly.instantiateStreaming(fetch("mydumper-lint.wasm"), go.importObject));
  } catch {
    const buf = await (await fetch("mydumper-lint.wasm")).arrayBuffer();
    ({ instance } = await WebAssembly.instantiate(buf, go.importObject));
  }
  const ready = new Promise((resolve) => window.addEventListener("mydumper-lint-ready", resolve, { once: true }));
  go.run(instance);
  if (!window.mydumperLint) await ready;
}

async function main() {
  setupTabs();
  setupExamples();
  setupFile();
  el.source.addEventListener("input", onEdit);
  el.source.addEventListener("scroll", () => (el.gutter.scrollTop = el.source.scrollTop));
  el.crlf.addEventListener("change", onEdit);
  el.bom.addEventListener("change", onEdit);
  el.name.addEventListener("input", scheduleAnalyze);
  el.version.addEventListener("change", analyze);
  el.unsafe.addEventListener("change", renderFixes);
  el.share.addEventListener("click", share);
  el.download.addEventListener("click", download);
  el.apply.addEventListener("click", () => {
    const f = el.unsafe.checked ? state.result.unsafe : state.result.safe;
    setBytes(unb64std(f.output));
    toast("Fixes applied to the editor.");
  });
  document.querySelectorAll("[data-copy]").forEach((b) => b.addEventListener("click", async () => {
    try {
      await navigator.clipboard.writeText($(b.dataset.copy).textContent);
      toast("Copied.");
    } catch {
      toast("Select the text to copy it.");
    }
  }));

  try {
    await loadWasm();
  } catch (err) {
    verdict("bad", "The playground could not start", String(err));
    return;
  }
  el.build.textContent = window.mydumperLint.build;
  for (const r of JSON.parse(window.mydumperLint.rules())) state.rules.set(r.id, r);
  const versions = JSON.parse(window.mydumperLint.versions());
  el.version.replaceChildren(...versions.map((v) => h("option", { value: v.tag },
    v.tag + (v.default ? " (latest stable)" : v.prerelease ? " (pre-release)" : ""))));
  el.version.value = (versions.find((v) => v.default) || versions[0]).tag;
  el.version.disabled = false;

  const params = new URLSearchParams(location.hash.slice(1));
  const shared = await unpack(params).catch(() => null);
  if (shared) {
    if (params.get("v")) el.version.value = params.get("v");
    if (params.get("n")) el.name.value = params.get("n");
    setBytes(shared);
  } else {
    el.example.value = "0";
    loadExample(examples[0]);
  }
}

main();
