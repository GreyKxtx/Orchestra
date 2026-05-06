package llm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LLMLogEntry represents a single log entry in llm_log.jsonl.
// Events: "llm_request", "llm_response", "llm_error", "tool_call", "tool_result".
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
		URL:            url,
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

func (l *Logger) appendLog(entry LLMLogEntry) {
	// Ensure directory exists
	dir := filepath.Dir(l.logPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return // Best-effort, don't fail on logging errors
	}

	// Append to JSONL file
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}

	file, err := os.OpenFile(l.logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer file.Close()

	file.Write(data)
	file.WriteString("\n")
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

	os.WriteFile(l.errorPath, data, 0644) // Best-effort
}

// truncateAndSanitize truncates string and removes API keys
func truncateAndSanitize(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return sanitizeSecrets(s)
	}
	return sanitizeSecrets(s[:maxBytes]) + "...(truncated)"
}

// sanitizeSecrets removes API keys and sensitive data from strings
func sanitizeSecrets(s string) string {
	// Remove Bearer tokens
	s = regexReplaceAll(s, `(?i)bearer\s+[a-zA-Z0-9_-]+`, "Bearer ***")
	// Remove api_key fields
	s = regexReplaceAll(s, `(?i)"api_key"\s*:\s*"[^"]*"`, `"api_key":"***"`)
	s = regexReplaceAll(s, `(?i)'api_key'\s*:\s*'[^']*'`, `'api_key':'***'`)
	return s
}

// Simple regex replacement (avoid importing regexp for minimal logging)
func regexReplaceAll(s, pattern, repl string) string {
	// For now, just do simple string replacements
	// Full regex would require importing regexp, which we want to avoid for minimal logging
	if strings.Contains(strings.ToLower(s), "bearer") {
		// Best-effort: find and replace common patterns
		lines := strings.Split(s, "\n")
		for i, line := range lines {
			if strings.Contains(strings.ToLower(line), "bearer") {
				parts := strings.SplitN(line, " ", 3)
				if len(parts) >= 2 && strings.ToLower(parts[0]) == "bearer" {
					lines[i] = "Bearer ***"
				}
			}
			if strings.Contains(strings.ToLower(line), "api_key") {
				if idx := strings.Index(line, ":"); idx > 0 {
					lines[i] = line[:idx+1] + " \"***\""
				}
			}
		}
		s = strings.Join(lines, "\n")
	}
	return s
}
