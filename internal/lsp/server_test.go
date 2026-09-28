package lsp

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tomsihap/mydumper-lint/internal/diag"
)

// fakeLinter reports MDL102 on every line holding only spaces (safe fix:
// remove them) and MDL402 on "routines=0" (unsafe fix), like the real rules.
type fakeLinter struct {
	settings []Settings
	fail     error
}

func (f *fakeLinter) Configure(s Settings) { f.settings = append(f.settings, s) }

func (f *fakeLinter) Check(_ string, text []byte) ([]diag.Diagnostic, error) {
	if f.fail != nil {
		return nil, f.fail
	}
	var out []diag.Diagnostic
	start := 0
	for _, line := range bytes.SplitAfter(text, []byte("\n")) {
		content := bytes.TrimRight(line, "\r\n")
		switch {
		case len(content) > 0 && len(bytes.TrimLeft(content, " ")) == 0:
			sp := diag.Span{Start: start, End: start + len(content)}
			out = append(out, diag.Diagnostic{
				RuleID: "MDL102", RuleName: "whitespace-only-line", Severity: diag.Error, Span: sp,
				Message: "line contains only whitespace", Consequence: "mydumper ignores this entire file",
				Related: []diag.Related{{Span: diag.Span{Start: 0, End: 1}, Message: "the file starts here"}},
				Fix:     &diag.Fix{Applicability: diag.Safe, Description: "Remove the whitespace", Edits: []diag.Edit{{Start: sp.Start, End: sp.End}}},
			})
		case string(content) == "routines=0":
			sp := diag.Span{Start: start + 9, End: start + 10}
			out = append(out, diag.Diagnostic{
				RuleID: "MDL402", RuleName: "boolean-flag-value", Severity: diag.Warning, Span: sp,
				Message: "`routines=0` turns routines on",
				Fix:     &diag.Fix{Applicability: diag.Unsafe, Description: "Comment the line out", Edits: []diag.Edit{{Start: start, End: start, New: "# "}}},
			})
		}
		start += len(line)
	}
	return out, nil
}

func (f *fakeLinter) Fix(path string, text []byte) ([]byte, error) {
	ds, _ := f.Check(path, text)
	out := []byte(nil)
	prev := 0
	for _, d := range ds {
		if d.Fix.Applicability != diag.Safe {
			continue
		}
		for _, e := range d.Fix.Edits {
			out = append(out, text[prev:e.Start]...)
			out = append(out, e.New...)
			prev = e.End
		}
	}
	return append(out, text[prev:]...), nil
}

// client drives a server over pipes.
type client struct {
	t      *testing.T
	in     *io.PipeWriter
	out    *conn
	done   chan error
	nextID int
}

func startServer(t *testing.T, l Linter) *client {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	c := &client{t: t, in: inW, out: newConn(outR, io.Discard), done: make(chan error, 1)}
	go func() {
		s := &Server{Linter: l, Version: "test"}
		err := s.Serve(inR, outW)
		_ = outW.Close()
		c.done <- err
	}()
	return c
}

func (c *client) send(v any) {
	c.t.Helper()
	body, _ := json.Marshal(v)
	if _, err := io.WriteString(c.in, "Content-Length: "+itoa(len(body))+"\r\n\r\n"+string(body)); err != nil {
		c.t.Fatal(err)
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func (c *client) notify(method string, params any) {
	c.send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

// request sends a request and returns its response, skipping notifications
// (which it returns too, in order).
func (c *client) request(method string, params any) (json.RawMessage, *rpcError, []map[string]json.RawMessage) {
	c.t.Helper()
	c.nextID++
	id := c.nextID
	c.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	var notes []map[string]json.RawMessage
	for {
		m := c.recv()
		if _, ok := m["id"]; ok && string(m["id"]) == itoa(id) {
			var e *rpcError
			if raw, ok := m["error"]; ok {
				e = &rpcError{}
				_ = json.Unmarshal(raw, e)
			}
			return m["result"], e, notes
		}
		notes = append(notes, m)
	}
}

func (c *client) recv() map[string]json.RawMessage {
	c.t.Helper()
	type res struct {
		m   map[string]json.RawMessage
		err error
	}
	ch := make(chan res, 1)
	go func() {
		line, err := readRaw(c.out)
		ch <- res{line, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			c.t.Fatalf("read: %v", r.err)
		}
		return r.m
	case <-time.After(5 * time.Second):
		c.t.Fatal("timeout waiting for the server")
		return nil
	}
}

// readRaw reads one message as a generic object.
func readRaw(cn *conn) (map[string]json.RawMessage, error) {
	length := -1
	for {
		line, err := cn.r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if v, ok := strings.CutPrefix(line, "Content-Length: "); ok {
			_ = json.Unmarshal([]byte(v), &length)
		}
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(cn.r, body); err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	err := json.Unmarshal(body, &m)
	return m, err
}

func (c *client) expectNotification(method string) json.RawMessage {
	c.t.Helper()
	m := c.recv()
	if string(m["method"]) != `"`+method+`"` {
		c.t.Fatalf("got %s, want notification %s", m["method"], method)
	}
	return m["params"]
}

func (c *client) close() error {
	_ = c.in.Close()
	select {
	case err := <-c.done:
		return err
	case <-time.After(5 * time.Second):
		c.t.Fatal("the server did not stop")
		return nil
	}
}

const uri = "file:///work/backup.cnf"

func TestSession(t *testing.T) {
	l := &fakeLinter{}
	c := startServer(t, l)

	// Before initialize, requests fail.
	if _, e, _ := c.request("textDocument/hover", map[string]any{}); e == nil || e.Code != codeServerNotInitialized {
		t.Errorf("request before initialize: %+v", e)
	}
	res, e, _ := c.request("initialize", map[string]any{
		"capabilities":          map[string]any{"general": map[string]any{"positionEncodings": []string{"utf-32", "utf-16"}}},
		"initializationOptions": map[string]any{"mydumperLint": map[string]any{"mydumperVersion": "v0.19.3-3"}},
	})
	if e != nil {
		t.Fatal(e)
	}
	var init struct {
		Capabilities struct {
			PositionEncoding string `json:"positionEncoding"`
			HoverProvider    bool   `json:"hoverProvider"`
		} `json:"capabilities"`
		ServerInfo struct{ Name, Version string } `json:"serverInfo"`
	}
	_ = json.Unmarshal(res, &init)
	if init.Capabilities.PositionEncoding != "utf-32" || !init.Capabilities.HoverProvider || init.ServerInfo.Name != "mydumper-lint" {
		t.Errorf("initialize result: %s", res)
	}
	if len(l.settings) != 1 || l.settings[0].MydumperVersion != "v0.19.3-3" {
		t.Errorf("settings: %+v", l.settings)
	}
	if _, e, _ := c.request("initialize", map[string]any{}); e == nil || e.Code != codeInvalidRequest {
		t.Errorf("second initialize: %+v", e)
	}
	c.notify("initialized", map[string]any{})

	text := "[mydumper]\n  \nroutines=0\n"
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "ini", "version": 1, "text": text}})
	var pub struct {
		URI         string       `json:"uri"`
		Version     int          `json:"version"`
		Diagnostics []Diagnostic `json:"diagnostics"`
	}
	_ = json.Unmarshal(c.expectNotification("textDocument/publishDiagnostics"), &pub)
	if pub.URI != uri || pub.Version != 1 || len(pub.Diagnostics) != 2 {
		t.Fatalf("diagnostics: %+v", pub)
	}
	d0 := pub.Diagnostics[0]
	if d0.Code != "MDL102" || d0.Severity != severityError || d0.Range != (Range{Position{1, 0}, Position{1, 2}}) ||
		d0.Message != "line contains only whitespace\nmydumper ignores this entire file" || d0.Source != "mydumper-lint" ||
		d0.CodeDescription == nil || !strings.HasSuffix(d0.CodeDescription.Href, "/MDL102.md") || len(d0.RelatedInformation) != 1 {
		t.Errorf("first diagnostic: %+v", d0)
	}
	if pub.Diagnostics[1].Severity != severityWarning {
		t.Errorf("second diagnostic: %+v", pub.Diagnostics[1])
	}

	// Code actions on line 1: the fix, the suppression is not offered for
	// MDL1xx (unsuppressible), and fix-all.
	res, _, _ = c.request("textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range":        Range{Position{1, 0}, Position{1, 0}},
		"context":      map[string]any{"diagnostics": []any{}},
	})
	var actions []CodeAction
	_ = json.Unmarshal(res, &actions)
	var titles []string
	for _, a := range actions {
		titles = append(titles, a.Kind+": "+a.Title)
	}
	want := []string{"quickfix: Fix MDL102: Remove the whitespace", FixAllKind + ": Fix all safe mydumper-lint problems"}
	if strings.Join(titles, "|") != strings.Join(want, "|") {
		t.Fatalf("actions:\n%s\nwant:\n%s", strings.Join(titles, "\n"), strings.Join(want, "\n"))
	}
	if !actions[0].IsPreferred || actions[0].Edit.Changes[uri][0].Range != (Range{Position{1, 0}, Position{1, 2}}) {
		t.Errorf("fix: %+v", actions[0])
	}
	if got := actions[1].Edit.Changes[uri][0]; got.NewText != "[mydumper]\n\nroutines=0\n" || got.Range != (Range{Position{0, 0}, Position{3, 0}}) {
		t.Errorf("fix all: %+v", got)
	}

	// Line 2: the unsafe fix, not preferred, and the suppression.
	res, _, _ = c.request("textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range":        Range{Position{2, 0}, Position{2, 10}},
		"context":      map[string]any{"diagnostics": []any{}, "only": []string{"quickfix"}},
	})
	actions = nil
	_ = json.Unmarshal(res, &actions)
	if len(actions) != 2 || actions[0].IsPreferred || !strings.Contains(actions[0].Title, "unsafe") ||
		actions[1].Title != "Disable MDL402 for this line" ||
		actions[1].Edit.Changes[uri][0].NewText != "# mydumper-lint: disable-next-line=MDL402\n" {
		t.Errorf("line 2 actions: %+v", actions)
	}

	// Hover on the whitespace line explains the rule.
	res, _, _ = c.request("textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": Position{1, 1}})
	var hover Hover
	_ = json.Unmarshal(res, &hover)
	if hover.Contents.Kind != "markdown" || !strings.Contains(hover.Contents.Value, "**MDL102** `whitespace-only-line`") ||
		!strings.Contains(hover.Contents.Value, "What mydumper does:") || !strings.Contains(hover.Contents.Value, "Safe fix: Remove the whitespace") {
		t.Errorf("hover: %+v", hover)
	}
	if res, _, _ := c.request("textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": Position{0, 3}}); string(res) != "null" {
		t.Errorf("hover without a diagnostic: %s", res)
	}

	// A change republishes; a settings change re-lints every document.
	c.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": 2},
		"contentChanges": []any{map[string]any{"text": "[mydumper]\nthreads=4\n"}},
	})
	_ = json.Unmarshal(c.expectNotification("textDocument/publishDiagnostics"), &pub)
	if pub.Version != 2 || len(pub.Diagnostics) != 0 {
		t.Errorf("after the change: %+v", pub)
	}
	c.notify("workspace/didChangeConfiguration", map[string]any{"settings": map[string]any{"mydumperVersion": "v1.0.5-1"}})
	_ = c.expectNotification("textDocument/publishDiagnostics")
	if last := l.settings[len(l.settings)-1]; last.MydumperVersion != "v1.0.5-1" {
		t.Errorf("settings after the change: %+v", l.settings)
	}

	// A configuration error is shown once, and the diagnostics cleared.
	l.fail = errors.New("bad config")
	c.notify("textDocument/didSave", map[string]any{"textDocument": map[string]any{"uri": uri}})
	var msg ShowMessageParams
	_ = json.Unmarshal(c.expectNotification("window/showMessage"), &msg)
	if msg.Type != messageError || msg.Message != "mydumper-lint: bad config" {
		t.Errorf("message: %+v", msg)
	}
	_ = json.Unmarshal(c.expectNotification("textDocument/publishDiagnostics"), &pub)
	c.notify("workspace/didChangeWatchedFiles", map[string]any{})
	_ = json.Unmarshal(c.expectNotification("textDocument/publishDiagnostics"), &pub) // no second message
	l.fail = nil

	c.notify("textDocument/didClose", map[string]any{"textDocument": map[string]any{"uri": uri}})
	_ = json.Unmarshal(c.expectNotification("textDocument/publishDiagnostics"), &pub)
	if len(pub.Diagnostics) != 0 {
		t.Errorf("close must clear the diagnostics: %+v", pub)
	}
	if res, _, _ := c.request("textDocument/codeAction", map[string]any{"textDocument": map[string]any{"uri": uri}}); string(res) != "[]" {
		t.Errorf("actions on a closed document: %s", res)
	}
	if res, _, _ := c.request("textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": uri}}); string(res) != "null" {
		t.Errorf("hover on a closed document: %s", res)
	}

	if _, e, _ := c.request("textDocument/definition", map[string]any{}); e == nil || e.Code != codeMethodNotFound {
		t.Errorf("unknown method: %+v", e)
	}
	if _, e, _ := c.request("textDocument/hover", "not an object"); e == nil || e.Code != codeInvalidParams {
		t.Errorf("bad params: %+v", e)
	}
	c.notify("$/cancelRequest", map[string]any{"id": 1})
	if _, e, _ := c.request("shutdown", nil); e != nil {
		t.Fatal(e)
	}
	if _, e, _ := c.request("textDocument/hover", map[string]any{}); e == nil || e.Code != codeInvalidRequest {
		t.Errorf("request after shutdown: %+v", e)
	}
	c.notify("exit", nil)
	if err := c.close(); err != nil {
		t.Errorf("exit after shutdown: %v", err)
	}
}

func TestExitWithoutShutdown(t *testing.T) {
	c := startServer(t, &fakeLinter{})
	c.notify("exit", nil)
	if err := c.close(); !errors.Is(err, ErrNoShutdown) {
		t.Errorf("exit without shutdown: %v", err)
	}
	c = startServer(t, &fakeLinter{})
	if err := c.close(); !errors.Is(err, ErrNoShutdown) {
		t.Errorf("EOF without shutdown: %v", err)
	}
}

func TestParseErrorsAndDisableEdits(t *testing.T) {
	c := startServer(t, &fakeLinter{})
	_, _ = io.WriteString(c.in, "Content-Length: 5\r\n\r\n{nope")
	m := c.recv()
	var e rpcError
	_ = json.Unmarshal(m["error"], &e)
	if e.Code != codeParseError {
		t.Errorf("parse error: %s", m["error"])
	}
	_, _ = io.WriteString(c.in, "garbage\r\n\r\n")
	if err := c.close(); err == nil || errors.Is(err, ErrNoShutdown) {
		t.Errorf("a malformed header ends the session: %v", err)
	}

	s := &Server{enc: UTF16}
	d := newDocument(uri, "", 1, []byte("[mydumper]\r\n  # mydumper-lint: disable-next-line=MDL303\r\n  where=a > 1\r\n  routines=0\r\n"))
	at := func(sub string) int { return bytes.Index(d.text, []byte(sub)) }
	// routines=0 has no directive above: a new one, indented, with the
	// file's line ending.
	x := diag.Diagnostic{RuleID: "MDL402", Span: diag.Span{Start: at("0\r"), End: at("0\r") + 1}}
	if got := s.disableEdit(d, x); got.NewText != "  # mydumper-lint: disable-next-line=MDL402\r\n" || got.Range.Start != (Position{3, 0}) {
		t.Errorf("new directive: %+v", got)
	}
	// where= has one: the rule is appended to it.
	x = diag.Diagnostic{RuleID: "MDL309", Span: diag.Span{Start: at("where"), End: at("where") + 5}}
	if got := s.disableEdit(d, x); got.NewText != ",MDL309" || got.Range.Start != (Position{1, 43}) {
		t.Errorf("appended to the directive above: %+v", got)
	}
}

func TestPathOf(t *testing.T) {
	for in, want := range map[string]string{
		"file:///work/a%20b.cnf": "/work/a b.cnf",
		"untitled:Untitled-1":    "",
		"%%":                     "",
	} {
		if got := filepath.ToSlash(pathOf(in)); got != want && !strings.HasSuffix(got, strings.TrimPrefix(want, "/")) {
			t.Errorf("pathOf(%q) = %q, want %q", in, got, want)
		}
	}
	if s := parseSettings(json.RawMessage(`[1]`), Settings{MydumperVersion: "keep"}); s.MydumperVersion != "keep" {
		t.Errorf("invalid settings must keep the current ones: %+v", s)
	}
}
