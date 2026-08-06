package state

// ToolDiagnostic is one LSP diagnostic attached to a write/edit tool result.
type ToolDiagnostic struct {
	StartLine int    `json:"start_line"`
	StartCol  int    `json:"start_col"`
	EndLine   int    `json:"end_line,omitempty"`
	EndCol    int    `json:"end_col,omitempty"`
	Severity  string `json:"severity"`
	Source    string `json:"source,omitempty"`
	Message   string `json:"message"`
}
