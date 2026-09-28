package lsp

import (
	"encoding/json"
	"io"
	"testing"

	"github.com/tomsihap/mydumper-lint/internal/diag"
)

// FuzzHandle sends arbitrary parameters to every method of an initialized
// server with an open document: nothing may panic, and positions far outside
// the document must be clamped.
func FuzzHandle(f *testing.F) {
	methods := []string{
		"textDocument/didOpen", "textDocument/didChange", "textDocument/didClose", "textDocument/didSave",
		"textDocument/codeAction", "textDocument/hover", "workspace/didChangeConfiguration",
		"workspace/didChangeWatchedFiles", "initialize", "shutdown", "unknown/method",
	}
	f.Add(uint8(4), `{"textDocument":{"uri":"file:///t.cnf"},"range":{"start":{"line":0,"character":0},"end":{"line":9,"character":99}},"context":{"diagnostics":[]}}`, "[mydumper]\n  \nroutines=0\n")
	f.Add(uint8(1), `{"textDocument":{"uri":"file:///t.cnf","version":2},"contentChanges":[{"range":{"start":{"line":1,"character":-5},"end":{"line":0,"character":99}},"text":"x\r\n"}]}`, "a\r\nb")
	f.Add(uint8(5), `{"textDocument":{"uri":"file:///t.cnf"},"position":{"line":-1,"character":1000000}}`, "😀\r[x]")
	f.Fuzz(func(t *testing.T, which uint8, params, text string) {
		s := &Server{Linter: &fakeLinter{}, enc: UTF16, docs: map[string]*document{}, results: map[string][]diag.Diagnostic{}, errors: map[string]string{}}
		s.conn = newConn(nil, io.Discard)
		s.initialized = true
		d := newDocument("file:///t.cnf", "/t.cnf", 1, []byte(text))
		s.docs[d.uri] = d
		if err := s.publish(d); err != nil {
			t.Fatal(err)
		}
		m := &message{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: methods[int(which)%len(methods)], Params: json.RawMessage(params)}
		if !json.Valid(m.Params) {
			m.Params = json.RawMessage("null")
		}
		_ = s.handle(m)
	})
}
