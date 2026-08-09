package agent

import (
	"encoding/json"

	"github.com/orchestra/orchestra/internal/llm"
	"github.com/orchestra/orchestra/protocol"

	agentformat "github.com/orchestra/orchestra/internal/agent/format"
)

// Thin shims so the agent root package keeps unexported call sites stable
// after helpers moved to internal/agent/format.

func formatValidatorError(msg string, raw string) string {
	return agentformat.ValidatorError(msg, raw)
}

func formatValidatorErrorCompact(msg string) string {
	return agentformat.ValidatorErrorCompact(msg)
}

func formatResolveErrorCompact(err error) string {
	return agentformat.ResolveErrorCompact(err)
}

func formatApplyErrorCompact(err error, code protocol.ErrorCode) string {
	return agentformat.ApplyErrorCompact(err, code)
}

func extractLSPErrors(out json.RawMessage) string {
	return agentformat.ExtractLSPErrors(out)
}

func formatErr(err error) string {
	return agentformat.ErrString(err)
}

func compactJSON(raw json.RawMessage) string {
	return agentformat.CompactJSON(raw)
}

func truncate(s string, max int) string {
	return agentformat.Truncate(s, max)
}

func truncateID(id string, maxLen int) string {
	return agentformat.TruncateID(id, maxLen)
}

func safeRun(label string, fn func()) any {
	return agentformat.SafeRun(label, fn)
}

func safeRunErr(label string, fn func() error) error {
	return agentformat.SafeRunErr(label, fn)
}

func extractScreenshotImagePart(out json.RawMessage) (llm.ContentPart, bool) {
	return agentformat.ExtractScreenshotImagePart(out)
}
