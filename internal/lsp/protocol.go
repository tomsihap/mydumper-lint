package lsp

import "encoding/json"

// The subset of the Language Server Protocol 3.17 the server uses.

// Position is a place in a document: a 0-based line, and a character counted in the negotiated encoding.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range is a half-open range between two positions.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Location is a range in a document.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// TextEdit replaces a range with new text.
type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

// WorkspaceEdit is a set of text edits, by document URI.
type WorkspaceEdit struct {
	Changes map[string][]TextEdit `json:"changes"`
}

// Diagnostic severities.
const (
	severityError       = 1
	severityWarning     = 2
	severityInformation = 3
)

// CodeDescription links a diagnostic's code to its documentation.
type CodeDescription struct {
	Href string `json:"href"`
}

// DiagnosticRelatedInformation points at another place that explains a diagnostic.
type DiagnosticRelatedInformation struct {
	Location Location `json:"location"`
	Message  string   `json:"message"`
}

// Diagnostic is a problem as editors show it.
type Diagnostic struct {
	Range              Range                          `json:"range"`
	Severity           int                            `json:"severity"`
	Code               string                         `json:"code"`
	CodeDescription    *CodeDescription               `json:"codeDescription,omitempty"`
	Source             string                         `json:"source"`
	Message            string                         `json:"message"`
	RelatedInformation []DiagnosticRelatedInformation `json:"relatedInformation,omitempty"`
}

// PublishDiagnosticsParams are the parameters of textDocument/publishDiagnostics.
type PublishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Version     *int32       `json:"version,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// TextDocumentIdentifier names a document.
type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

// TextDocumentItem is an opened document and its text.
type TextDocumentItem struct {
	URI     string `json:"uri"`
	Version int32  `json:"version"`
	Text    string `json:"text"`
}

// VersionedTextDocumentIdentifier names a version of a document.
type VersionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int32  `json:"version"`
}

// DidOpenTextDocumentParams are the parameters of textDocument/didOpen.
type DidOpenTextDocumentParams struct {
	TextDocument TextDocumentItem `json:"textDocument"`
}

// TextDocumentContentChangeEvent is one change: a range and its new text, or the whole text.
type TextDocumentContentChangeEvent struct {
	Range *Range `json:"range,omitempty"` // absent: the whole document
	Text  string `json:"text"`
}

// DidChangeTextDocumentParams are the parameters of textDocument/didChange.
type DidChangeTextDocumentParams struct {
	TextDocument   VersionedTextDocumentIdentifier  `json:"textDocument"`
	ContentChanges []TextDocumentContentChangeEvent `json:"contentChanges"`
}

// DidCloseTextDocumentParams are the parameters of textDocument/didClose.
type DidCloseTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// CodeActionContext says which diagnostics and kinds of action the editor asks for.
type CodeActionContext struct {
	Diagnostics []Diagnostic `json:"diagnostics"`
	Only        []string     `json:"only,omitempty"`
}

// CodeActionParams are the parameters of textDocument/codeAction.
type CodeActionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Range        Range                  `json:"range"`
	Context      CodeActionContext      `json:"context"`
}

// CodeAction is a fix or a suppression the editor can apply.
type CodeAction struct {
	Title       string         `json:"title"`
	Kind        string         `json:"kind"`
	Diagnostics []Diagnostic   `json:"diagnostics,omitempty"`
	IsPreferred bool           `json:"isPreferred,omitempty"`
	Edit        *WorkspaceEdit `json:"edit,omitempty"`
}

// TextDocumentPositionParams name a position in a document, as for textDocument/hover.
type TextDocumentPositionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// MarkupContent is text in Markdown or plain text.
type MarkupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// Hover is what the editor shows over a position.
type Hover struct {
	Contents MarkupContent `json:"contents"`
	Range    *Range        `json:"range,omitempty"`
}

// InitializeParams are the parts of the initialize parameters the server reads.
type InitializeParams struct {
	RootURI      string `json:"rootUri"`
	Capabilities struct {
		General struct {
			PositionEncodings []string `json:"positionEncodings"`
		} `json:"general"`
	} `json:"capabilities"`
	InitializationOptions json.RawMessage `json:"initializationOptions"`
}

// ShowMessageParams are the parameters of window/showMessage.
type ShowMessageParams struct {
	Type    int    `json:"type"`
	Message string `json:"message"`
}

// Message types of window/showMessage.
const (
	messageError   = 1
	messageWarning = 2
)
