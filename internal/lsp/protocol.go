package lsp

import (
	"encoding/json"
	"fmt"
)

// Position is a 0-based line/character pair as used in LSP.
type Position struct {
	Line      uint32 `json:"line"`
	Character uint32 `json:"character"`
}

// Range is a range in a text document.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Location is a file URI + range.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// LocationLink is an extended definition result.
type LocationLink struct {
	OriginSelectionRange *Range `json:"originSelectionRange,omitempty"`
	TargetURI            string `json:"targetUri"`
	TargetRange          Range  `json:"targetRange"`
	TargetSelectionRange Range  `json:"targetSelectionRange"`
}

// DiagnosticSeverity mirrors LSP DiagnosticSeverity.
type DiagnosticSeverity int

const (
	SeverityError       DiagnosticSeverity = 1
	SeverityWarning     DiagnosticSeverity = 2
	SeverityInformation DiagnosticSeverity = 3
	SeverityHint        DiagnosticSeverity = 4
)

func (s DiagnosticSeverity) String() string {
	switch s {
	case SeverityError:
		return "error"
	case SeverityWarning:
		return "warning"
	case SeverityInformation:
		return "information"
	case SeverityHint:
		return "hint"
	default:
		return "unknown"
	}
}

// Diagnostic represents a compiler/linter diagnostic.
type Diagnostic struct {
	Range    Range              `json:"range"`
	Severity DiagnosticSeverity `json:"severity,omitempty"`
	Code     json.RawMessage    `json:"code,omitempty"` // string | number
	Source   string             `json:"source,omitempty"`
	Message  string             `json:"message"`
}

// TextEdit is a text replacement within a range.
type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

// VersionedTextDocumentIdentifier identifies a versioned document.
type VersionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
}

// TextDocumentEdit is a set of edits on a versioned document.
type TextDocumentEdit struct {
	TextDocument VersionedTextDocumentIdentifier `json:"textDocument"`
	Edits        []TextEdit                      `json:"edits"`
}

// WorkspaceEdit is the result of a rename or other multi-file operation.
type WorkspaceEdit struct {
	Changes         map[string][]TextEdit `json:"changes,omitempty"`
	DocumentChanges []TextDocumentEdit    `json:"documentChanges,omitempty"`
}

// SymbolKind is the LSP symbol kind (1-based enum).
type SymbolKind int

const (
	SymbolFile          SymbolKind = 1
	SymbolModule        SymbolKind = 2
	SymbolNamespace     SymbolKind = 3
	SymbolPackage       SymbolKind = 4
	SymbolClass         SymbolKind = 5
	SymbolMethod        SymbolKind = 6
	SymbolProperty      SymbolKind = 7
	SymbolField         SymbolKind = 8
	SymbolConstructor   SymbolKind = 9
	SymbolEnum          SymbolKind = 10
	SymbolInterface     SymbolKind = 11
	SymbolFunction      SymbolKind = 12
	SymbolVariable      SymbolKind = 13
	SymbolConstant      SymbolKind = 14
	SymbolStruct        SymbolKind = 23
	SymbolTypeParameter SymbolKind = 26
)

func (k SymbolKind) String() string {
	switch k {
	case SymbolFile:
		return "file"
	case SymbolModule:
		return "module"
	case SymbolNamespace:
		return "namespace"
	case SymbolPackage:
		return "package"
	case SymbolClass:
		return "class"
	case SymbolMethod:
		return "method"
	case SymbolProperty:
		return "property"
	case SymbolField:
		return "field"
	case SymbolConstructor:
		return "constructor"
	case SymbolEnum:
		return "enum"
	case SymbolInterface:
		return "interface"
	case SymbolFunction:
		return "function"
	case SymbolVariable:
		return "variable"
	case SymbolConstant:
		return "constant"
	case SymbolStruct:
		return "struct"
	case SymbolTypeParameter:
		return "typeParameter"
	default:
		return "unknown"
	}
}

// DocumentSymbol is the hierarchical symbol returned by textDocument/documentSymbol.
type DocumentSymbol struct {
	Name           string           `json:"name"`
	Detail         string           `json:"detail,omitempty"`
	Kind           SymbolKind       `json:"kind"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children,omitempty"`
}

// SymbolInformation is the flat symbol format (older servers).
type SymbolInformation struct {
	Name     string     `json:"name"`
	Kind     SymbolKind `json:"kind"`
	Location Location   `json:"location"`
}

// MarkupContent is formatted hover content.
type MarkupContent struct {
	Kind  string `json:"kind"` // "plaintext" | "markdown"
	Value string `json:"value"`
}

// rpcMessage is a raw JSON-RPC 2.0 envelope.
type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("LSP error %d: %s", e.Code, e.Message)
}
