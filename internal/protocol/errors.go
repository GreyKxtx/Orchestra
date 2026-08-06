package protocol

import "fmt"

// ErrorCode is a stable, JSON-serializable code for core errors.
type ErrorCode string

// Error code disambiguation contract (M7 in architecture audit).
//
// Several codes intentionally cover more than one scenario (e.g.
// InvalidLLMOutput fires for malformed JSON, schema failure, bad tool
// arguments, or a malformed final.patches op). Rather than split each
// into N siblings — which forces every client to grow its switch — we
// fix the shape of the Data payload so consumers can disambiguate
// from the JSON alone.
//
// Convention: Data is a map (often map[string]any) with a "kind"
// key when more than one scenario shares the same code. Existing
// fields (path, expected_hash, …) are preserved.
const (
	// InvalidLLMOutput — model produced something we can't act on.
	// Data["kind"] disambiguates: "json" (malformed), "schema"
	// (validator failed, Data["error"]), "tool_args" (tool input
	// didn't parse), "op" (final.patches op wrong shape). Data also
	// carries the offending text / path when available.
	InvalidLLMOutput ErrorCode = "InvalidLLMOutput"

	// StaleContent — file_hash mismatch between LLM read and apply.
	// Data: {path, expected_hash, actual_hash}.
	StaleContent ErrorCode = "StaleContent"

	// AmbiguousMatch — search target matched more than once.
	// Data: {path, matches, match_lines}.
	AmbiguousMatch ErrorCode = "AmbiguousMatch"

	// PathTraversal — path escapes workspace_root. Data: {path}.
	PathTraversal ErrorCode = "PathTraversal"

	// NotInitialized — RPC method called before initialize.
	NotInitialized ErrorCode = "NotInitialized"

	// AlreadyInitialized — initialize called twice with different params.
	AlreadyInitialized ErrorCode = "AlreadyInitialized"

	// ProtocolMismatch — protocol_version / ops_version / tools_version
	// the client requested doesn't match what the core supports.
	ProtocolMismatch ErrorCode = "ProtocolMismatch"

	// AlreadyExists — write_atomic with must_not_exist saw an existing
	// file. Data: {path}.
	AlreadyExists ErrorCode = "AlreadyExists"

	// ExecDenied — bash / exec.run was denied.
	// Data["kind"] disambiguates: "static" (--allow-exec off and
	// exec.confirm=true), "rule" (a permission_rule denied,
	// Data["rule_index"]), "interactive" (user denied at prompt).
	// Data also carries {tool, description}.
	ExecDenied ErrorCode = "ExecDenied"

	// ExecTimeout — command exceeded its timeout. Data: {tool, timeout_ms}.
	ExecTimeout ErrorCode = "ExecTimeout"

	// ExecFailed — command returned a non-zero exit code.
	// Data: {tool, exit_code, stderr_tail}.
	ExecFailed ErrorCode = "ExecFailed"

	// InvalidParams maps to the standard JSON-RPC "Invalid params"
	// (-32602). Use for malformed / missing required parameters before
	// any work begins. Data: free-form description of what was wrong.
	InvalidParams ErrorCode = "InvalidParams"

	// NotFound — named thing does not exist.
	// Data["kind"] disambiguates: "skill" / "session" / "workflow" /
	// "file" / "tool" with the corresponding Data["name"] or
	// Data["id"] / Data["path"]. Maps to -32012 (impl-defined).
	NotFound ErrorCode = "NotFound"

	// SyntaxError — tree-sitter detected ERROR/MISSING nodes before staging.
	// Data: {path, line, node}.
	SyntaxError ErrorCode = "SyntaxError"
)

// RPCCode maps an internal ErrorCode to a JSON-RPC server error code.
//
// JSON-RPC reserves -32000..-32099 for implementation-defined server errors.
func (c ErrorCode) RPCCode() int {
	switch c {
	case InvalidLLMOutput:
		return -32001
	case StaleContent:
		return -32002
	case AmbiguousMatch:
		return -32003
	case PathTraversal:
		return -32004
	case NotInitialized:
		return -32007
	case AlreadyInitialized:
		return -32009
	case ProtocolMismatch:
		return -32010
	case AlreadyExists:
		return -32011
	case ExecDenied:
		return -32008
	case ExecTimeout:
		return -32005
	case ExecFailed:
		return -32006
	case InvalidParams:
		return -32602 // JSON-RPC standard "Invalid params"
	case NotFound:
		return -32012
	case SyntaxError:
		return -32013
	default:
		return -32099
	}
}

// Error is a structured error returned by the core.
//
// It is designed to be embedded into JSON-RPC errors (via ErrorCode.RPCCode + Data).
type Error struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
	Data    any       `json:"data,omitempty"`
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Code == "" {
		return e.Message
	}
	if e.Message == "" {
		return string(e.Code)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func NewError(code ErrorCode, message string, data any) *Error {
	return &Error{Code: code, Message: message, Data: data}
}

func AsError(err error) (*Error, bool) {
	e, ok := err.(*Error)
	return e, ok
}
