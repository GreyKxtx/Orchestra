package llm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

// LLMLogEntry represents a single log entry in llm_log.jsonl.
// Events: "llm_request", "llm_response", "llm_error", "tool_call", "tool_result",
// "step.classified", "memory.note", "memory.inject".
type LLMLogEntry struct {
	TSUnix          int64    `json:"ts_unix"`
	Event           string   `json:"event"`
	URL             string   `json:"url,omitempty"`
	Model           string   `json:"model,omitempty"`
	TimeoutS        int      `json:"timeout_s,omitempty"`
	RequestBytes    int      `json:"request_bytes,omitempty"`
	ToolsCount      int      `json:"tools_count,omitempty"`
	MessagesCount   int      `json:"messages_count,omitempty"`
	MessageRoles    []string `json:"message_roles,omitempty"`
	ResponseBytes   int      `json:"response_bytes,omitempty"`
	DurationMS      int64    `json:"duration_ms,omitempty"`
	HTTPCode        int      `json:"http_code,omitempty"`
	ErrorBody       string   `json:"error_body,omitempty"`
	RequestPreview  string   `json:"request_preview,omitempty"`
	ResponsePreview string   `json:"response_preview,omitempty"`

	// tool_call / tool_result fields
	ToolName    string `json:"tool_name,omitempty"`
	InputBytes  int    `json:"input_bytes,omitempty"`
	OutputBytes int    `json:"output_bytes,omitempty"`
	ErrorStr    string `json:"error,omitempty"`

	// step.classified fields; memory.note reuses Kind (outcome) and Detail
	Step   int    `json:"step,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Detail string `json:"detail,omitempty"`

	// memory.note: where the note came from — "model" or "digest"
	Source string `json:"source,omitempty"`
}

// Logger handles LLM request/response logging
type Logger struct {
	projectRoot string
	logPath     string
	errorPath   string
}

// NewLogger creates a new LLM logger
func NewLogger(projectRoot string) *Logger {
	if projectRoot == "" {
		return nil // No logging if no project root
	}
	return &Logger{
		projectRoot: projectRoot,
		logPath:     filepath.Join(projectRoot, ".orchestra", "llm_log.jsonl"),
		errorPath:   filepath.Join(projectRoot, ".orchestra", "llm_last_error.json"),
	}
}

// LogRequest logs an LLM request
func (l *Logger) LogRequest(url, model string, timeoutS int, requestBytes int, toolsCount, messagesCount int, messageRoles []string, requestPreview string) {
	if l == nil {
		return
	}
	entry := LLMLogEntry{
		TSUnix:         time.Now().Unix(),
		Event:          "llm_request",
		URL:            sanitizeSecrets(url),
		Model:          model,
		TimeoutS:       timeoutS,
		RequestBytes:   requestBytes,
		ToolsCount:     toolsCount,
		MessagesCount:  messagesCount,
		MessageRoles:   messageRoles,
		RequestPreview: truncateAndSanitize(requestPreview, 2048),
	}
	l.appendLog(entry)
}

// LogResponse logs a successful LLM response
func (l *Logger) LogResponse(responseBytes int, durationMS int64, responsePreview string) {
	if l == nil {
		return
	}
	entry := LLMLogEntry{
		TSUnix:          time.Now().Unix(),
		Event:           "llm_response",
		ResponseBytes:   responseBytes,
		DurationMS:      durationMS,
		ResponsePreview: truncateAndSanitize(responsePreview, 2048),
	}
	l.appendLog(entry)
}

// LogError logs an LLM error
func (l *Logger) LogError(httpCode int, errorBody string, durationMS int64) {
	if l == nil {
		return
	}
	entry := LLMLogEntry{
		TSUnix:     time.Now().Unix(),
		Event:      "llm_error",
		HTTPCode:   httpCode,
		ErrorBody:  truncateAndSanitize(errorBody, 2048),
		DurationMS: durationMS,
	}
	l.appendLog(entry)

	// Also save as last error for quick access
	errorData := map[string]interface{}{
		"ts_unix":     entry.TSUnix,
		"http_code":   httpCode,
		"error_body":  entry.ErrorBody,
		"duration_ms": durationMS,
	}
	l.writeLastError(errorData)
}

// LogToolCall logs a tool invocation before execution.
func (l *Logger) LogToolCall(toolName string, inputBytes int) {
	if l == nil {
		return
	}
	l.appendLog(LLMLogEntry{
		TSUnix:     time.Now().Unix(),
		Event:      "tool_call",
		ToolName:   toolName,
		InputBytes: inputBytes,
	})
}

// LogToolResult logs the result (or error) of a tool invocation.
func (l *Logger) LogToolResult(toolName string, outputBytes int, durationMS int64, errStr string) {
	if l == nil {
		return
	}
	l.appendLog(LLMLogEntry{
		TSUnix:      time.Now().Unix(),
		Event:       "tool_result",
		ToolName:    toolName,
		OutputBytes: outputBytes,
		DurationMS:  durationMS,
		ErrorStr:    errStr,
	})
}

// LogStepClassified logs a circuit-breaker / retry classification for eval harnesses.
// kind uses ROADMAP names: validation_error, tool_denied, tool_failed, resolve_failed, apply_recoverable.
func (l *Logger) LogStepClassified(step int, kind, toolName, detail string) {
	if l == nil {
		return
	}
	l.appendLog(LLMLogEntry{
		TSUnix:   time.Now().Unix(),
		Event:    "step.classified",
		Step:     step,
		Kind:     kind,
		ToolName: toolName,
		Detail:   truncateAndSanitize(detail, 512),
	})
}

// LogMemoryNote records what the end-of-turn memory writer did: outcome is
// written | skipped | failed, source is model | digest (empty for a skip).
// Memory used to report only to stderr, and the field run showed what that
// buys: one note in fifty-two sessions and no way to tell from the logs
// whether it had tried. This is the same file the run analysis reads.
func (l *Logger) LogMemoryNote(outcome, source, detail string) {
	if l == nil {
		return
	}
	l.appendLog(LLMLogEntry{
		TSUnix: time.Now().Unix(),
		Event:  "memory.note",
		Kind:   outcome,
		Source: source,
		Detail: truncateAndSanitize(detail, 512),
	})
}

// LogMemoryInject records what a turn's project-memory inject actually
// included — a compact per-layer byte breakdown against the budget, e.g.
// "orchestra=512B repo=0B global=0B total=512B/2048B"
// (memory.Store.FormatInjectReport). /memory refresh in the TUI reads this
// back; without it, "what got injected" is answerable only by re-deriving
// budgets from config and guessing at file sizes.
func (l *Logger) LogMemoryInject(detail string) {
	if l == nil {
		return
	}
	l.appendLog(LLMLogEntry{
		TSUnix: time.Now().Unix(),
		Event:  "memory.inject",
		Detail: truncateAndSanitize(detail, 512),
	})
}

// LogProviderSwitch records a failover from one provider to another. A
// failover that leaves no trace is indistinguishable from a slow day — and
// the usage ledger from that point on names a provider the config's llm:
// block never mentions, with nothing to explain why.
func (l *Logger) LogProviderSwitch(from, to, reason string) {
	if l == nil {
		return
	}
	l.appendLog(LLMLogEntry{
		TSUnix: time.Now().Unix(),
		Event:  "provider.switch",
		Kind:   from,
		Source: to,
		Detail: truncateAndSanitize(reason, 512),
	})
}

// maxLogBytes caps llm_log.jsonl growth: past the limit the file rotates to
// llm_log.jsonl.1 (one old generation kept), so the log never grows unbounded.
const maxLogBytes = 5 << 20 // 5 MB

// appendMu serializes append+rotate across goroutines. Parallel workers share
// one logger; without the lock the separate body/newline writes interleave
// and corrupt the JSONL stream ({"a":1}{"b":2}\n\n).
var appendMu sync.Mutex

func (l *Logger) appendLog(entry LLMLogEntry) {
	// Ensure directory exists
	dir := filepath.Dir(l.logPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return // Best-effort, don't fail on logging errors
	}

	// Marshal before taking the lock; a single write keeps each JSONL line atomic.
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	data = append(data, '\n')

	appendMu.Lock()
	defer appendMu.Unlock()

	rotateIfLarge(l.logPath, maxLogBytes)

	file, err := os.OpenFile(l.logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer file.Close()

	_, _ = file.Write(data)
}

// rotateIfLarge renames path to path+".1" (replacing a previous generation)
// when it exceeds maxBytes. Best-effort: rotation failure must never block
// logging.
func rotateIfLarge(path string, maxBytes int64) {
	info, err := os.Stat(path)
	if err != nil || info.Size() < maxBytes {
		return
	}
	old := path + ".1"
	_ = os.Remove(old)
	_ = os.Rename(path, old)
}

func (l *Logger) writeLastError(errorData map[string]interface{}) {
	dir := filepath.Dir(l.errorPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return
	}

	data, err := json.MarshalIndent(errorData, "", "  ")
	if err != nil {
		return
	}
	data = append(data, '\n')

	os.WriteFile(l.errorPath, data, 0600) // Best-effort
}

// truncateAndSanitize truncates string and removes API keys
func truncateAndSanitize(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return sanitizeSecrets(s)
	}
	return sanitizeSecrets(s[:maxBytes]) + "...(truncated)"
}

var (
	reBearer    = regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._\-]+`)
	reAPIKeyDbl = regexp.MustCompile(`(?i)("(?:api_key|apikey|api-key|authorization|x-api-key)"\s*:\s*")[^"]*("?)`)
	reAPIKeySgl = regexp.MustCompile(`(?i)('(?:api_key|apikey|api-key|authorization|x-api-key)'\s*:\s*')[^']*('?)`)
	// Bare provider key material that can be echoed back in error bodies:
	// OpenAI/OpenRouter (sk-, sk-or-v1-), Anthropic (sk-ant-), Google (AIza…),
	// GitHub (ghp_/gho_), generic 32+ char hex-ish tokens after key= params.
	reBareKey  = regexp.MustCompile(`\b(sk-[A-Za-z0-9_\-]{8,}|AIza[A-Za-z0-9_\-]{20,}|gh[pousr]_[A-Za-z0-9]{20,})\b`)
	reURLToken = regexp.MustCompile(`(?i)([?&](?:key|api_key|apikey|token|access_token)=)[^&\s"']+`)
)

// sanitizeSecrets removes Bearer tokens, api_key values and bare key
// material from strings before they land in llm_log.jsonl.
func sanitizeSecrets(s string) string {
	s = reBearer.ReplaceAllString(s, "${1}***")
	s = reAPIKeyDbl.ReplaceAllString(s, "${1}***${2}")
	s = reAPIKeySgl.ReplaceAllString(s, "${1}***${2}")
	s = reBareKey.ReplaceAllString(s, "***")
	s = reURLToken.ReplaceAllString(s, "${1}***")
	return s
}
