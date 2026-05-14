package agent

import "github.com/orchestra/orchestra/internal/protocol"

// ErrorKind classifies the type of failure that occurred in the agent loop.
type ErrorKind int

const (
	ErrorKindNone        ErrorKind = iota
	ErrorKindDenied                // tool call was blocked by policy
	ErrorKindToolError             // tool call returned an error
	ErrorKindFinalFailed           // resolve/apply of final patches failed
	ErrorKindInvalid               // LLM produced invalid JSON/schema output
)

// CircuitBreaker tracks per-kind failure counters and opens (trips) when any
// counter exceeds its limit.
type CircuitBreaker struct {
	maxDenied    int
	maxToolErr   int
	maxFinal     int
	maxInvalid   int

	// deniedPerTool tracks repeated denied calls per tool name.
	deniedPerTool       map[string]int
	consecutiveToolErrs int
	finalFailures       int
	invalidOutputs      int

	// successfulCallKeys tracks (tool+argsHash) → count for dedup detection.
	successfulCallKeys map[string]int
}

// NewCircuitBreaker creates a CircuitBreaker with the given limits.
// Zero or negative limits fall back to conservative defaults.
func NewCircuitBreaker(maxDenied, maxToolErr, maxFinal, maxInvalid int) *CircuitBreaker {
	if maxDenied <= 0 {
		maxDenied = 2
	}
	if maxToolErr <= 0 {
		maxToolErr = 6
	}
	if maxFinal <= 0 {
		maxFinal = 6
	}
	if maxInvalid <= 0 {
		maxInvalid = 3
	}
	return &CircuitBreaker{
		maxDenied:          maxDenied,
		maxToolErr:         maxToolErr,
		maxFinal:           maxFinal,
		maxInvalid:         maxInvalid,
		deniedPerTool:      make(map[string]int, 4),
		successfulCallKeys: make(map[string]int, 8),
	}
}

// RecordDenied records a denied tool call and returns an error if the circuit trips.
func (cb *CircuitBreaker) RecordDenied(toolName string) *protocol.Error {
	cb.deniedPerTool[toolName]++
	if cb.deniedPerTool[toolName] > cb.maxDenied {
		return protocol.NewError(protocol.InvalidLLMOutput, "model repeatedly requested denied tool", map[string]any{
			"tool":        toolName,
			"count":       cb.deniedPerTool[toolName],
			"max_repeats": cb.maxDenied,
		})
	}
	return nil
}

// RecordToolError records a consecutive tool call error and returns an error if the circuit trips.
// Successful tool calls should reset consecutive errors via ResetToolErrors.
func (cb *CircuitBreaker) RecordToolError(toolName string) *protocol.Error {
	cb.consecutiveToolErrs++
	if cb.consecutiveToolErrs > cb.maxToolErr {
		return protocol.NewError(protocol.InvalidLLMOutput, "model repeatedly produced failing tool calls", map[string]any{
			"count":       cb.consecutiveToolErrs,
			"max_repeats": cb.maxToolErr,
			"last_tool":   toolName,
		})
	}
	return nil
}

// ResetToolErrors resets the consecutive tool error counter after a successful tool call.
func (cb *CircuitBreaker) ResetToolErrors() {
	cb.consecutiveToolErrs = 0
}

// RecordFinalFailure records a failed resolve/apply attempt and returns an error if the circuit trips.
func (cb *CircuitBreaker) RecordFinalFailure(lastErr error) *protocol.Error {
	cb.finalFailures++
	if cb.finalFailures > cb.maxFinal {
		return protocol.NewError(protocol.InvalidLLMOutput, "failed to resolve/apply patches repeatedly", map[string]any{
			"count":        cb.finalFailures,
			"max_failures": cb.maxFinal,
			"last_error":   formatErr(lastErr),
		})
	}
	return nil
}

// ResetFinalFailures resets final failure counter (e.g., after a successful tool call signals progress).
func (cb *CircuitBreaker) ResetFinalFailures() {
	cb.finalFailures = 0
}

// IsDuplicateCall returns true if this exact tool+args combination was already
// successfully executed before. Does NOT modify the counter — call
// RecordSuccessfulCall after deciding whether to execute.
func (cb *CircuitBreaker) IsDuplicateCall(toolName string, inputBytes []byte) bool {
	key := toolName + ":" + string(inputBytes)
	return cb.successfulCallKeys[key] > 0
}

// RecordSuccessfulCall tracks repeated successful calls of the same tool+args.
// Returns a non-empty hint string when the model has called the same tool with
// the same arguments more than once — caller should inject it as a user message
// so the model learns it already has the data and should answer.
func (cb *CircuitBreaker) RecordSuccessfulCall(toolName string, inputBytes []byte) string {
	key := toolName + ":" + string(inputBytes)
	cb.successfulCallKeys[key]++
	if cb.successfulCallKeys[key] == 2 {
		return "⛔ СТОП — НЕМЕДЛЕННО ПРЕКРАТИ ВЫЗЫВАТЬ ИНСТРУМЕНТЫ. Ты уже вызывал «" + toolName + "» с идентичными аргументами и получил результат. Эти данные уже в твоей истории. СЛЕДУЮЩИЙ ТВОй ВЫВОД ОБЯЗАН БЫТЬ ФИНАЛЬНЫМ ОТВЕТОМ — без tool_calls, только текст + {\"patches\":[]}."
	}
	return ""
}

// RecordInvalid records an invalid LLM output and returns an error if the circuit trips.
func (cb *CircuitBreaker) RecordInvalid() *protocol.Error {
	cb.invalidOutputs++
	if cb.invalidOutputs > cb.maxInvalid {
		return protocol.NewError(protocol.InvalidLLMOutput, "model repeatedly produced invalid output", map[string]any{
			"count":   cb.invalidOutputs,
			"max":     cb.maxInvalid,
		})
	}
	return nil
}
