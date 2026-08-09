package guard

import (
	"errors"
	"strings"

	"github.com/orchestra/orchestra/protocol"
)

// String returns the ROADMAP / llm_log kind name for this ErrorKind.
func (k ErrorKind) String() string {
	switch k {
	case ErrorKindDenied:
		return "tool_denied"
	case ErrorKindToolError:
		return "tool_failed"
	case ErrorKindResolveFailed:
		return "resolve_failed"
	case ErrorKindApplyRecoverable:
		return "apply_recoverable"
	case ErrorKindFinalFailed:
		return "resolve_failed" // legacy alias → resolve_failed
	case ErrorKindInvalid:
		return "validation_error"
	default:
		return "none"
	}
}

// Classify maps an error (and optional hint) onto a circuit-breaker kind.
//
// ROADMAP kinds: validation_error, tool_denied, tool_failed, resolve_failed, apply_recoverable.
//
// When the caller already knows the failure surface (resolve / apply / denied / …),
// that hint wins — except StaleContent/AmbiguousMatch always classify as
// apply_recoverable even if the call site used RecordFinalFailure/RecordResolveFailure.
func Classify(err error, hint ErrorKind) ErrorKind {
	if pe, ok := protocol.AsError(err); ok {
		switch pe.Code {
		case protocol.StaleContent, protocol.AmbiguousMatch:
			return ErrorKindApplyRecoverable
		}
	} else if err != nil {
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "stalecontent") || strings.Contains(msg, "ambiguousmatch") {
			return ErrorKindApplyRecoverable
		}
	}

	switch hint {
	case ErrorKindResolveFailed, ErrorKindApplyRecoverable, ErrorKindDenied,
		ErrorKindToolError, ErrorKindInvalid:
		return hint
	case ErrorKindFinalFailed:
		return ErrorKindResolveFailed
	}

	if pe, ok := protocol.AsError(err); ok {
		switch pe.Code {
		case protocol.ExecDenied:
			return ErrorKindDenied
		case protocol.InvalidLLMOutput:
			return ErrorKindInvalid
		}
	}
	if err != nil {
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "denied") || strings.Contains(msg, "permission") {
			return ErrorKindDenied
		}
		return ErrorKindToolError
	}
	return ErrorKindNone
}

// ClassifyHint is a zero-allocation helper when the caller already knows the kind
// and only wants ROADMAP naming / logging consistency.
func ClassifyHint(hint ErrorKind) ErrorKind {
	return Classify(nil, hint)
}

// RecordMeta carries optional context for CircuitBreaker.Record / OnClassified.
type RecordMeta struct {
	ToolName string
	Err      error
	Detail   string
}

// OnClassifiedFunc is invoked after each classified failure is recorded
// (before the circuit may trip). Used for llm_log step.classified events.
type OnClassifiedFunc func(kind ErrorKind, meta RecordMeta)

// SetOnClassified installs a classify observer (nil clears).
func (cb *CircuitBreaker) SetOnClassified(fn OnClassifiedFunc) {
	if cb == nil {
		return
	}
	cb.onClassified = fn
}

func (cb *CircuitBreaker) emitClassified(kind ErrorKind, meta RecordMeta) {
	if cb == nil || cb.onClassified == nil {
		return
	}
	if meta.Detail == "" && meta.Err != nil {
		meta.Detail = meta.Err.Error()
	}
	cb.onClassified(kind, meta)
}

// Record classifies and increments the matching counter. Returns a protocol
// error when the circuit trips for that kind.
func (cb *CircuitBreaker) Record(kind ErrorKind, meta RecordMeta) *protocol.Error {
	kind = Classify(meta.Err, kind)
	cb.emitClassified(kind, meta)
	switch kind {
	case ErrorKindDenied:
		return cb.recordDenied(meta.ToolName)
	case ErrorKindToolError:
		return cb.recordToolError(meta.ToolName)
	case ErrorKindResolveFailed, ErrorKindFinalFailed:
		return cb.recordResolveFailure(meta.Err)
	case ErrorKindApplyRecoverable:
		return cb.recordApplyRecoverable(meta.Err)
	case ErrorKindInvalid:
		return cb.recordInvalid()
	default:
		return nil
	}
}

// ErrStringCompact returns a short error string for logging (nil-safe).
func ErrStringCompact(err error) string {
	if err == nil {
		return ""
	}
	var pe *protocol.Error
	if errors.As(err, &pe) && pe != nil {
		return string(pe.Code) + ": " + pe.Message
	}
	return err.Error()
}
