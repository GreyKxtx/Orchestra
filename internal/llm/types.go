package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Role is an OpenAI-compatible chat message role.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message is an OpenAI-compatible chat message (subset).
type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content,omitempty"`

	// ToolCallID is required for messages with role="tool".
	ToolCallID string `json:"tool_call_id,omitempty"`

	// ToolCalls is returned by the model when it wants to call tools.
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// ToolDef describes a callable tool in OpenAI "tools" format.
type ToolDef struct {
	Type     string          `json:"type"` // must be "function"
	Function ToolFunctionDef `json:"function"`
}

// ToolFunctionDef is a tool signature (name + JSON Schema parameters).
type ToolFunctionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

// ToolCall is a single model tool call.
type ToolCall struct {
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type"` // "function"
	Function ToolCallFunc `json:"function"`
	Index    *int         `json:"index,omitempty"` // some providers include it
}

// ToolCallFunc is the tool call payload.
type ToolCallFunc struct {
	Name      string        `json:"name"`
	Arguments ToolArguments `json:"arguments"`
}

// ToolArguments is a tolerant parser for OpenAI-compatible tool call arguments.
//
// OpenAI sends arguments as a JSON string containing an object:
//
//	"arguments": "{\"path\":\"main.go\"}"
//
// Some providers may send arguments as an object directly.
type ToolArguments json.RawMessage

func (a *ToolArguments) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || bytes.Equal(b, []byte("null")) {
		*a = nil
		return nil
	}

	// Common case: JSON string that itself contains JSON.
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return fmt.Errorf("tool arguments: expected string: %w", err)
		}
		s = strings.TrimSpace(s)
		if s == "" {
			*a = ToolArguments([]byte(`{}`))
			return nil
		}
		*a = ToolArguments([]byte(s))
		return nil
	}

	// Fallback: treat as raw JSON value (typically an object).
	*a = ToolArguments(append([]byte(nil), b...))
	return nil
}

func (a ToolArguments) Raw() json.RawMessage {
	return json.RawMessage(bytes.TrimSpace([]byte(a)))
}

// MarshalJSON ensures ToolArguments is serialized as a JSON string (OpenAI-compatible format),
// not as base64-encoded bytes.
func (a ToolArguments) MarshalJSON() ([]byte, error) {
	raw := bytes.TrimSpace([]byte(a))
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	// Serialize as JSON string containing the JSON object
	// This matches OpenAI format: "arguments": "{\"path\":\"...\"}"
	return json.Marshal(string(raw))
}

// ResponseFormat requests structured output from the provider.
type ResponseFormat struct {
	// Type is "json_object" or "json_schema".
	Type string
	// Schema is the JSON Schema to enforce (only for Type="json_schema").
	// Must be a valid JSON Schema object understood by the provider.
	Schema []byte
	// SchemaName is the schema identifier sent to the provider (for json_schema mode).
	SchemaName string
}

// CompleteRequest is a single chat completion request.
type CompleteRequest struct {
	Messages       []Message
	Tools          []ToolDef
	ResponseFormat *ResponseFormat // optional; nil = no constraint
}

// CompleteResponse is a single assistant turn (content and/or tool calls).
type CompleteResponse struct {
	Message Message
}
