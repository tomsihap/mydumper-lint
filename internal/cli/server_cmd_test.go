package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// lspSession drives `mydumper-lint server` over pipes, the way an editor does.
type lspSession struct {
	t    *testing.T
	in   *io.PipeWriter
	msgs chan map[string]json.RawMessage // read continuously, so the server never blocks on a write
	code chan int
	id   int
}

func startLSP(t *testing.T, args ...string) *lspSession {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	s := &lspSession{t: t, in: inW, msgs: make(chan map[string]json.RawMessage, 256), code: make(chan int, 1)}
	go func() {
		code := Run(append([]string{"server"}, args...), inR, outW, io.Discard)
		_ = outW.Close()
		s.code <- code
	}()
	go func() {
		defer close(s.msgs)
		r := bufio.NewReader(outR)
		for {
			n := -1
			for {
				line, err := r.ReadString('\n')
				if err != nil {
					return
				}
				line = strings.TrimSpace(line)
				if line == "" {
					break
				}
				if v, ok := strings.CutPrefix(line, "Content-Length: "); ok {
					n, _ = strconv.Atoi(v)
				}
			}
			body := make([]byte, n)
			if _, err := io.ReadFull(r, body); err != nil {
				return
			}
			var m map[string]json.RawMessage
			_ = json.Unmarshal(body, &m)
			s.msgs <- m
		}
	}()
	return s
}

func (s *lspSession) send(method string, params any, request bool) int {
	s.t.Helper()
	m := map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
	if request {
		s.id++
		m["id"] = s.id
	}
	body, _ := json.Marshal(m)
	if _, err := fmt.Fprintf(s.in, "Content-Length: %d\r\n\r\n%s", len(body), body); err != nil {
		s.t.Fatal(err)
	}
	return s.id
}

func (s *lspSession) recv() map[string]json.RawMessage {
	s.t.Helper()
	select {
	case m, ok := <-s.msgs:
		if !ok {
			s.t.Fatal("the server closed the connection")
		}
		return m
	case <-time.After(10 * time.Second):
		s.t.Fatal("timeout waiting for the server")
		return nil
	}
}

// call sends a request and returns its result, dropping notifications.
func (s *lspSession) call(method string, params any) json.RawMessage {
	s.t.Helper()
	id := s.send(method, params, true)
	for {
		m := s.recv()
		if string(m["id"]) == strconv.Itoa(id) {
			if e, ok := m["error"]; ok {
				s.t.Fatalf("%s: %s", method, e)
			}
			return m["result"]
		}
	}
}

// diagnostics waits for the next publishDiagnostics and returns its codes.
func (s *lspSession) diagnostics() []string {
	s.t.Helper()
	for {
		m := s.recv()
		if string(m["method"]) != `"textDocument/publishDiagnostics"` {
			continue
		}
		var p struct {
			Diagnostics []struct{ Code string } `json:"diagnostics"`
		}
		_ = json.Unmarshal(m["params"], &p)
		codes := []string{}
		for _, d := range p.Diagnostics {
			codes = append(codes, d.Code)
		}
		return codes
	}
}

func (s *lspSession) stop() int {
	s.t.Helper()
	s.call("shutdown", nil)
	s.send("exit", nil, false)
	_ = s.in.Close()
	select {
	case code := <-s.code:
		return code
	case <-time.After(10 * time.Second):
		s.t.Fatal("the server did not exit")
		return -1
	}
}

func fileURI(path string) string {
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
}

func TestServerCommand(t *testing.T) {
	src := "[mydumper]\nthreads=4\n  \n[`app`.`users`]\n`email`=random_string\n"
	root := workspace(t, map[string]string{".mydumper-lint.yaml": "mydumper-version: v1.0.5-1\n"})
	uri := fileURI(filepath.Join(root, "backup.cnf"))

	s := startLSP(t)
	s.call("initialize", map[string]any{"capabilities": map[string]any{}})
	s.send("initialized", map[string]any{}, false)
	s.send("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "ini", "version": 1, "text": src}}, false)
	if got := s.diagnostics(); strings.Join(got, " ") != "MDL102" {
		t.Errorf("diagnostics %v, want MDL102", got)
	}
	// Fix all gives what check --fix gives.
	raw := s.call("textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range":        map[string]any{"start": map[string]int{"line": 0, "character": 0}, "end": map[string]int{"line": 5, "character": 0}},
		"context":      map[string]any{"diagnostics": []any{}, "only": []string{"source.fixAll"}},
	})
	var actions []struct {
		Kind string
		Edit struct {
			Changes map[string][]struct{ NewText string }
		}
	}
	if err := json.Unmarshal(raw, &actions); err != nil || len(actions) != 1 || actions[0].Kind != "source.fixAll.mydumper-lint" {
		t.Fatalf("fix-all action: %v %s", err, raw)
	}
	fixed := run(t, src, "check", "--fix", "--no-config", "--mydumper-version", "v1.0.5-1", "--stdin-filename", "backup.cnf", "-").stdout
	if got := actions[0].Edit.Changes[uri][0].NewText; got != fixed {
		t.Errorf("fix all:\n%q\nwant (check --fix):\n%q", got, fixed)
	}
	// The editor's version wins over the configuration file: v0.19.1-3 has
	// no pre-processor, so a line of spaces is harmless there.
	s.send("workspace/didChangeConfiguration", map[string]any{"settings": map[string]any{"mydumperLint": map[string]any{"mydumperVersion": "v0.19.1-3"}}}, false)
	if got := s.diagnostics(); len(got) != 0 {
		t.Errorf("with v0.19.1-3: %v", got)
	}
	// An unknown version is reported as a message, once.
	s.send("workspace/didChangeConfiguration", map[string]any{"settings": map[string]any{"mydumperVersion": "bogus"}}, false)
	m := s.recv()
	if string(m["method"]) != `"window/showMessage"` || !bytes.Contains(m["params"], []byte("invalid mydumper version")) {
		t.Errorf("configuration error: %s %s", m["method"], m["params"])
	}
	if code := s.stop(); code != ExitOK {
		t.Errorf("exit code %d", code)
	}
}

func TestServerCommandFlags(t *testing.T) {
	if r := run(t, "", "server", "--help"); r.code != ExitOK || !strings.Contains(r.stdout, "language server") {
		t.Errorf("help: %+v", r)
	}
	if r := run(t, "", "server", "file.cnf"); r.code != ExitError || !strings.Contains(r.stderr, "takes no file") {
		t.Errorf("a file argument: %+v", r)
	}
	if r := run(t, "", "server", "--config", filepath.Join(t.TempDir(), "missing.yaml")); r.code != ExitError {
		t.Errorf("a missing --config: %+v", r)
	}
	// No shutdown before the end of input: exit code 1, as LSP asks.
	if r := run(t, "", "server", "--stdio", "--no-config"); r.code != ExitFindings {
		t.Errorf("end of input without shutdown: %+v", r)
	}
	// A fixed configuration file and a version on the command line.
	root := workspace(t, map[string]string{"cfg.yaml": "rules:\n  ignore: [MDL102]\n"})
	s := startLSP(t, "--config", filepath.Join(root, "cfg.yaml"), "--mydumper-version", "v1.0.5-1")
	s.call("initialize", map[string]any{})
	s.send("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": "untitled:Untitled-1", "version": 1, "text": "[mydumper]\n  \nroutines=0\n"}}, false)
	if got := s.diagnostics(); strings.Join(got, " ") != "MDL402" {
		t.Errorf("diagnostics %v, want MDL402 (MDL102 ignored by the configuration)", got)
	}
	if code := s.stop(); code != ExitOK {
		t.Errorf("exit code %d", code)
	}
}
