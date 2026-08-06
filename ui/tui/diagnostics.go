package tui

import (
	"github.com/orchestra/orchestra/ui/tui/rpcclient"
	"github.com/orchestra/orchestra/ui/tui/state"
)

func diagnosticsToState(in []rpcclient.ToolDiagnosticPayload) []state.ToolDiagnostic {
	if len(in) == 0 {
		return nil
	}
	out := make([]state.ToolDiagnostic, len(in))
	for i, d := range in {
		out[i] = state.ToolDiagnostic{
			StartLine: d.StartLine,
			StartCol:  d.StartCol,
			EndLine:   d.EndLine,
			EndCol:    d.EndCol,
			Severity:  d.Severity,
			Source:    d.Source,
			Message:   d.Message,
		}
	}
	return out
}
