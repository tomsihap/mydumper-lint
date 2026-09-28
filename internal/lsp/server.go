// Package lsp is mydumper-lint's language server (design §14, M6): the
// diagnostics of `mydumper-lint check` while you type, its fixes as code
// actions, and each rule's explanation on hover, for any editor that speaks
// the Language Server Protocol over stdin and stdout.
package lsp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/rules"
)

// Linter is what the server needs from mydumper-lint.
type Linter interface {
	// Check lints a document. path is its file path, "" when the document is
	// not a file; the configuration that applies to path is used.
	Check(path string, text []byte) ([]diag.Diagnostic, error)
	// Fix applies the safe fixes to a document, as `check --fix` would.
	Fix(path string, text []byte) ([]byte, error)
	// Configure applies the client's settings, and forgets the configuration
	// files read so far (one of them may have changed).
	Configure(Settings)
}

// Settings are what the client can set, through initializationOptions or
// workspace/didChangeConfiguration.
type Settings struct {
	// MydumperVersion overrides the version of the configuration files
	// (`--mydumper-version`); empty keeps them.
	MydumperVersion string `json:"mydumperVersion"`
}

// FixAllKind is the kind of the "fix everything safe" source action, which
// editors can run on save.
const FixAllKind = "source.fixAll.mydumper-lint"

// Server is one language server session.
type Server struct {
	Linter  Linter
	Version string    // reported to the client
	Log     io.Writer // for messages about the connection itself; may be nil

	conn        *conn
	enc         Encoding
	settings    Settings
	docs        map[string]*document
	results     map[string][]diag.Diagnostic
	errors      map[string]string // the configuration error last shown, per document
	initialized bool
	shutdown    bool
}

// ErrNoShutdown means the client exited or went away without asking the
// server to shut down first; LSP asks for exit code 1 then.
var ErrNoShutdown = errors.New("lsp: the client exited without a shutdown request")

// Serve answers the client until it sends exit or closes the connection.
func (s *Server) Serve(in io.Reader, out io.Writer) error {
	s.conn = newConn(in, out)
	s.enc = UTF16
	s.docs = map[string]*document{}
	s.results = map[string][]diag.Diagnostic{}
	s.errors = map[string]string{}
	for {
		m, err := s.conn.read()
		switch {
		case errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF):
			if s.shutdown {
				return nil
			}
			return ErrNoShutdown
		case err != nil:
			return err
		}
		if m.Method == "exit" {
			if s.shutdown {
				return nil
			}
			return ErrNoShutdown
		}
		if err := s.handle(m); err != nil {
			return err
		}
	}
}

func (s *Server) logf(format string, args ...any) {
	if s.Log != nil {
		fmt.Fprintf(s.Log, "mydumper-lint server: "+format+"\n", args...)
	}
}

// handle dispatches one message. Only a failure to write ends the session.
func (s *Server) handle(m *message) error {
	switch {
	case m.JSONRPC == "" && m.Method == "":
		return s.conn.replyError(nil, codeParseError, "invalid JSON")
	case m.Method == "":
		return nil // a response: the server sends no request
	case !s.initialized && m.Method != "initialize":
		if m.isRequest() {
			return s.conn.replyError(m.ID, codeServerNotInitialized, "the server is not initialized")
		}
		return nil
	case s.shutdown && m.isRequest():
		return s.conn.replyError(m.ID, codeInvalidRequest, "the server is shutting down")
	}
	var err error
	switch m.Method {
	case "initialize":
		return s.initialize(m)
	case "shutdown":
		s.shutdown = true
		return s.conn.reply(m.ID, nil)
	case "textDocument/didOpen":
		var p DidOpenTextDocumentParams
		if err = json.Unmarshal(m.Params, &p); err == nil {
			d := newDocument(p.TextDocument.URI, pathOf(p.TextDocument.URI), p.TextDocument.Version, []byte(p.TextDocument.Text))
			s.docs[d.uri] = d
			err = s.publish(d)
		}
	case "textDocument/didChange":
		var p DidChangeTextDocumentParams
		if err = json.Unmarshal(m.Params, &p); err == nil {
			if d := s.docs[p.TextDocument.URI]; d != nil {
				for _, c := range p.ContentChanges {
					if c.Range == nil {
						d.setText([]byte(c.Text))
					} else {
						d.replace(*c.Range, c.Text, s.enc)
					}
				}
				d.version = p.TextDocument.Version
				err = s.publish(d)
			}
		}
	case "textDocument/didClose":
		var p DidCloseTextDocumentParams
		if err = json.Unmarshal(m.Params, &p); err == nil {
			delete(s.docs, p.TextDocument.URI)
			delete(s.results, p.TextDocument.URI)
			delete(s.errors, p.TextDocument.URI)
			err = s.conn.notify("textDocument/publishDiagnostics", PublishDiagnosticsParams{URI: p.TextDocument.URI, Diagnostics: []Diagnostic{}})
		}
	case "textDocument/didSave", "workspace/didChangeWatchedFiles":
		// A saved file may be a .mydumper-lint.yaml: read the configuration again.
		err = s.reconfigure(nil)
	case "workspace/didChangeConfiguration":
		var p struct {
			Settings json.RawMessage `json:"settings"`
		}
		if err = json.Unmarshal(m.Params, &p); err == nil {
			err = s.reconfigure(p.Settings)
		}
	case "textDocument/codeAction":
		var p CodeActionParams
		if err = json.Unmarshal(m.Params, &p); err == nil {
			return s.conn.reply(m.ID, s.codeActions(p))
		}
	case "textDocument/hover":
		var p TextDocumentPositionParams
		if err = json.Unmarshal(m.Params, &p); err == nil {
			return s.conn.reply(m.ID, s.hover(p))
		}
	case "initialized", "$/cancelRequest", "$/setTrace", "$/progress":
		return nil
	default:
		if m.isRequest() {
			return s.conn.replyError(m.ID, codeMethodNotFound, "unsupported method "+m.Method)
		}
		return nil
	}
	var syntax *json.SyntaxError
	var typ *json.UnmarshalTypeError
	if errors.As(err, &syntax) || errors.As(err, &typ) {
		s.logf("%s: invalid parameters: %v", m.Method, err)
		if m.isRequest() {
			return s.conn.replyError(m.ID, codeInvalidParams, err.Error())
		}
		return nil
	}
	return err
}

func (s *Server) initialize(m *message) error {
	if s.initialized {
		return s.conn.replyError(m.ID, codeInvalidRequest, "the server is already initialized")
	}
	var p InitializeParams
	if len(m.Params) > 0 {
		if err := json.Unmarshal(m.Params, &p); err != nil {
			return s.conn.replyError(m.ID, codeInvalidParams, err.Error())
		}
	}
	for _, e := range p.Capabilities.General.PositionEncodings {
		if e := Encoding(e); e == UTF8 || e == UTF16 || e == UTF32 {
			s.enc = e
			break
		}
	}
	s.settings = parseSettings(p.InitializationOptions, s.settings)
	s.Linter.Configure(s.settings)
	s.initialized = true
	return s.conn.reply(m.ID, map[string]any{
		"capabilities": map[string]any{
			"positionEncoding": s.enc,
			"textDocumentSync": map[string]any{
				"openClose": true,
				"change":    1, // the whole document on each change
				"save":      map[string]any{"includeText": false},
			},
			"codeActionProvider": map[string]any{"codeActionKinds": []string{"quickfix", FixAllKind}},
			"hoverProvider":      true,
		},
		"serverInfo": map[string]any{"name": "mydumper-lint", "version": s.Version},
	})
}

// parseSettings reads client settings, flat or under "mydumperLint".
func parseSettings(raw json.RawMessage, current Settings) Settings {
	if len(raw) == 0 || string(raw) == "null" {
		return current
	}
	var nested struct {
		Section *Settings `json:"mydumperLint"`
	}
	if json.Unmarshal(raw, &nested) == nil && nested.Section != nil {
		return *nested.Section
	}
	var flat Settings
	if json.Unmarshal(raw, &flat) == nil {
		return flat
	}
	return current
}

func (s *Server) reconfigure(raw json.RawMessage) error {
	s.settings = parseSettings(raw, s.settings)
	s.Linter.Configure(s.settings)
	for _, d := range s.docs {
		if err := s.publish(d); err != nil {
			return err
		}
	}
	return nil
}

// publish lints a document and sends its diagnostics. A configuration error
// is shown once, until it changes.
func (s *Server) publish(d *document) error {
	ds, err := s.Linter.Check(d.path, d.text)
	if err != nil {
		msg := "mydumper-lint: " + err.Error()
		if s.errors[d.uri] != msg {
			s.errors[d.uri] = msg
			if err := s.conn.notify("window/showMessage", ShowMessageParams{Type: messageError, Message: msg}); err != nil {
				return err
			}
		}
		ds = nil
	} else {
		delete(s.errors, d.uri)
	}
	s.results[d.uri] = ds
	out := make([]Diagnostic, 0, len(ds))
	for _, x := range ds {
		out = append(out, s.diagnostic(d, x))
	}
	version := d.version
	return s.conn.notify("textDocument/publishDiagnostics", PublishDiagnosticsParams{URI: d.uri, Version: &version, Diagnostics: out})
}

func (s *Server) diagnostic(d *document, x diag.Diagnostic) Diagnostic {
	out := Diagnostic{
		Range:    d.rangeOf(x.Span.Start, x.Span.End, s.enc),
		Severity: severityInformation,
		Code:     x.RuleID,
		Source:   "mydumper-lint",
		Message:  x.Message,
	}
	switch x.Severity {
	case diag.Error:
		out.Severity = severityError
	case diag.Warning:
		out.Severity = severityWarning
	case diag.Off, diag.Info:
	}
	if x.Consequence != "" {
		out.Message += "\n" + x.Consequence
	}
	if r, ok := rules.Lookup(x.RuleID); ok {
		out.CodeDescription = &CodeDescription{Href: r.DocsURL()}
	}
	for _, rel := range x.Related {
		out.RelatedInformation = append(out.RelatedInformation, DiagnosticRelatedInformation{
			Location: Location{URI: d.uri, Range: d.rangeOf(rel.Span.Start, rel.Span.End, s.enc)},
			Message:  rel.Message,
		})
	}
	return out
}

// codeActions returns, for the diagnostics in the range, their fixes and a
// way to suppress them, then a fix-all action.
func (s *Server) codeActions(p CodeActionParams) []CodeAction {
	d := s.docs[p.TextDocument.URI]
	actions := []CodeAction{}
	if d == nil {
		return actions
	}
	want := func(kind string) bool {
		if len(p.Context.Only) == 0 {
			return true
		}
		for _, o := range p.Context.Only {
			if kind == o || strings.HasPrefix(kind, o+".") {
				return true
			}
		}
		return false
	}
	start, end := d.offset(p.Range.Start, s.enc), d.offset(p.Range.End, s.enc)
	var suppress []CodeAction
	safe := false
	for _, x := range s.results[d.uri] {
		if x.Fix != nil && x.Fix.Applicability == diag.Safe {
			safe = true
		}
		if x.Span.Start > end || x.Span.End < start || !want("quickfix") {
			continue
		}
		ld := s.diagnostic(d, x)
		if x.Fix != nil && len(x.Fix.Edits) > 0 {
			title := "Fix " + x.RuleID + ": " + x.Fix.Description
			if x.Fix.Applicability == diag.Unsafe {
				title = "Fix " + x.RuleID + " (unsafe, changes what mydumper does): " + x.Fix.Description
			}
			edits := make([]TextEdit, 0, len(x.Fix.Edits))
			for _, e := range x.Fix.Edits {
				edits = append(edits, TextEdit{Range: d.rangeOf(e.Start, e.End, s.enc), NewText: e.New})
			}
			actions = append(actions, CodeAction{
				Title: title, Kind: "quickfix", Diagnostics: []Diagnostic{ld},
				IsPreferred: x.Fix.Applicability == diag.Safe,
				Edit:        &WorkspaceEdit{Changes: map[string][]TextEdit{d.uri: edits}},
			})
		}
		if r, ok := rules.Lookup(x.RuleID); ok && !r.Unsuppressible {
			suppress = append(suppress, CodeAction{
				Title: "Disable " + x.RuleID + " for this line", Kind: "quickfix", Diagnostics: []Diagnostic{ld},
				Edit: &WorkspaceEdit{Changes: map[string][]TextEdit{d.uri: {s.disableEdit(d, x)}}},
			})
		}
	}
	actions = append(actions, suppress...)
	if safe && want(FixAllKind) {
		if out, err := s.Linter.Fix(d.path, d.text); err == nil && !bytes.Equal(out, d.text) {
			actions = append(actions, CodeAction{
				Title: "Fix all safe mydumper-lint problems", Kind: FixAllKind,
				Edit: &WorkspaceEdit{Changes: map[string][]TextEdit{d.uri: {{Range: d.rangeOf(0, len(d.text), s.enc), NewText: string(out)}}}},
			})
		} else if err != nil {
			s.logf("fix all %s: %v", d.uri, err)
		}
	}
	return actions
}

// directiveRE matches a disable-next-line comment and captures its rule list.
var directiveRE = regexp.MustCompile(`^[ \t]*#[ \t]*mydumper-lint:[ \t]*disable-next-line=([^\r\n]*?)[ \t]*$`)

// disableEdit suppresses x with a comment on the line above. A directive
// already there gets the rule appended: a new comment line would push it
// away from the line it suppresses.
func (s *Server) disableEdit(d *document, x diag.Diagnostic) TextEdit {
	line := d.position(x.Span.Start, UTF8).Line
	if line > 0 {
		prev := d.text[d.starts[line-1]:d.lineEnd(line-1)]
		if m := directiveRE.FindSubmatchIndex(prev); m != nil {
			at := d.starts[line-1] + m[3]
			return TextEdit{Range: d.rangeOf(at, at, s.enc), NewText: "," + x.RuleID}
		}
	}
	at := d.starts[line]
	eol := "\n"
	if end := d.lineEnd(line); end+1 < len(d.text) && d.text[end] == '\r' && d.text[end+1] == '\n' {
		eol = "\r\n"
	}
	i := at
	for i < d.lineEnd(line) && (d.text[i] == ' ' || d.text[i] == '\t') {
		i++
	}
	return TextEdit{Range: d.rangeOf(at, at, s.enc), NewText: string(d.text[at:i]) + "# mydumper-lint: disable-next-line=" + x.RuleID + eol}
}

// hover explains the diagnostics under the cursor.
func (s *Server) hover(p TextDocumentPositionParams) *Hover {
	d := s.docs[p.TextDocument.URI]
	if d == nil {
		return nil
	}
	off := d.offset(p.Position, s.enc)
	var parts []string
	var span *diag.Span
	for _, x := range s.results[d.uri] {
		if off < x.Span.Start || off > x.Span.End {
			continue
		}
		if span == nil {
			sp := x.Span
			span = &sp
		}
		var b strings.Builder
		fmt.Fprintf(&b, "**%s** `%s` · %s\n\n%s", x.RuleID, x.RuleName, x.Severity, x.Message)
		if x.Consequence != "" {
			fmt.Fprintf(&b, "\n\n**What mydumper does:** %s", x.Consequence)
		}
		if r, ok := rules.Lookup(x.RuleID); ok {
			fmt.Fprintf(&b, "\n\n%s\n\n[Documentation](%s)", r.Why, r.DocsURL())
		}
		switch {
		case x.Fix == nil:
		case x.Fix.Applicability == diag.Safe:
			fmt.Fprintf(&b, "\n\nSafe fix: %s", x.Fix.Description)
		default:
			fmt.Fprintf(&b, "\n\nUnsafe fix (it changes what mydumper does): %s", x.Fix.Description)
		}
		parts = append(parts, b.String())
	}
	if len(parts) == 0 {
		return nil
	}
	r := d.rangeOf(span.Start, span.End, s.enc)
	return &Hover{Contents: MarkupContent{Kind: "markdown", Value: strings.Join(parts, "\n\n---\n\n")}, Range: &r}
}

// pathOf returns the file path of a file: URI, or "".
func pathOf(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return ""
	}
	p := u.Path
	if runtime.GOOS == "windows" {
		p = strings.TrimPrefix(p, "/") // the slash before the drive letter
		if u.Host != "" {
			p = `\\` + u.Host + `\` + p // UNC path
		}
	}
	return filepath.FromSlash(p)
}
