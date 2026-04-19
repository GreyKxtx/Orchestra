package protocol

import "fmt"

// ErrorCode is a stable, JSON-serializable code for core errors.
type ErrorCode string

const (
	InvalidLLMOutput   ErrorCode = "InvalidLLMOutput"
	StaleContent       ErrorCode = "StaleContent"
	AmbiguousMatch     ErrorCode = "AmbiguousMatch"
	PathTraversal      ErrorCode = "PathTraversal"
	NotInitialized     ErrorCode = "NotInitialized"
	AlreadyInitialized ErrorCode = "AlreadyInitialized"
	ProtocolMismatch   ErrorCode = "ProtocolMismatch"
	AlreadyExists      ErrorCode = "AlreadyExists"
	ExecDenied         ErrorCode = "ExecDenied"
	ExecTimeout        ErrorCode = "ExecTimeout"
	ExecFailed         ErrorCode = "ExecFailed"
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
