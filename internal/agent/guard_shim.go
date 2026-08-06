package agent

import (
	"encoding/json"

	"github.com/orchestra/orchestra/internal/agent/guard"
	"github.com/orchestra/orchestra/internal/config"
)

// Re-export guard types so external API and root call sites stay stable.
type (
	ErrorKind      = guard.ErrorKind
	CircuitBreaker = guard.CircuitBreaker
	DiagTracker    = guard.DiagTracker
)

const (
	ErrorKindNone        = guard.ErrorKindNone
	ErrorKindDenied      = guard.ErrorKindDenied
	ErrorKindToolError   = guard.ErrorKindToolError
	ErrorKindFinalFailed = guard.ErrorKindFinalFailed
	ErrorKindInvalid     = guard.ErrorKindInvalid
)

func NewCircuitBreaker(maxDenied, maxToolErr, maxFinal, maxInvalid int) *CircuitBreaker {
	return guard.NewCircuitBreaker(maxDenied, maxToolErr, maxFinal, maxInvalid)
}

func newDiagTracker() *DiagTracker {
	return guard.NewDiagTracker()
}

func checkPermissions(rules []config.PermissionRule, name, subject string) (action string, matched bool) {
	return guard.CheckPermissions(rules, name, subject)
}

func subjectForTool(name string, input json.RawMessage) string {
	return guard.SubjectForTool(name, input)
}

func fingerprintLSPErrors(out json.RawMessage) string {
	return guard.FingerprintLSPErrors(out)
}

func extractWriteOrEditPath(input json.RawMessage) string {
	return guard.ExtractWriteOrEditPath(input)
}

func dedupExemptTool(toolName string) bool {
	return guard.DedupExemptTool(toolName)
}
