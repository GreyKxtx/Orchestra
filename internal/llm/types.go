package llm

import (
	"bytes"
	"encoding/base64"
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
//
// Multimodal: when Parts is non-empty, OpenAI-compatible clients serialise
// the message's content as the array form ([{type:"text",...},
// {type:"image_url",...}]) and the Content string is ignored on the wire.
// When Parts is empty, the string Content path is used (back-compat).
type Message struct {
	Role    Role          `json:"role"`
	Content string        `json:"content,omitempty"`
	Parts   []ContentPart `json:"-"`

	// ToolCallID is required for messages with role="tool".
	ToolCallID string `json:"tool_call_id,omitempty"`

	// ToolCalls is returned by the model when it wants to call tools.
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// ContentPartKind enumerates the kinds of content a multimodal message
// can carry. New kinds (audio, file) are added here as providers grow.
type ContentPartKind string

const (
	PartText  ContentPartKind = "text"
	PartImage ContentPartKind = "image"
)

// ContentPart is one fragment of a multimodal message. For PartText the
// Text field carries the content. For PartImage either ImageURL (remote
// or data: URI) or ImageData (raw bytes + MIME) is set; the client
// serialises whichever is non-empty as a data: URI when bytes are given.
type ContentPart struct {
	Kind ContentPartKind

	// Text is set for PartText.
	Text string

	// ImageURL is set for PartImage when the caller supplies a URL or
	// already-encoded data URI.
	ImageURL string

	// ImageData and ImageMIME are set for PartImage when raw bytes
	// should be inlined as a data: URI by the client.
	ImageData []byte
	ImageMIME string
}

// TextLen returns the number of text bytes carried by Parts (image
// content is not counted). Used by compaction/truncation heuristics so
// huge base64 payloads don't blow up the budget calculation.
func (m Message) TextLen() int {
	if len(m.Parts) == 0 {
		return len(m.Content)
	}
	n := 0
	for _, p := range m.Parts {
		if p.Kind == PartText {
			n += len(p.Text)
		}
	}
	return n
}

// HasImages reports whether the message carries any PartImage entries.
func (m Message) HasImages() bool {
	for _, p := range m.Parts {
		if p.Kind == PartImage {
			return true
		}
	}
	return false
}

// MarshalJSON emits the OpenAI multimodal array form when Parts is
// non-empty; otherwise it falls back to the default struct serialisation
// (Content as a string).
func (m Message) MarshalJSON() ([]byte, error) {
	if len(m.Parts) == 0 {
		type alias Message
		return json.Marshal(alias(m))
	}
	type imageURL struct {
		URL string `json:"url"`
	}
	type wirePart struct {
		Type     string    `json:"type"`
		Text     string    `json:"text,omitempty"`
		ImageURL *imageURL `json:"image_url,omitempty"`
	}
	wire := make([]wirePart, 0, len(m.Parts))
	for _, p := range m.Parts {
		switch p.Kind {
		case PartText:
			wire = append(wire, wirePart{Type: "text", Text: p.Text})
		case PartImage:
			url := p.ImageURL
			if url == "" && len(p.ImageData) > 0 {
				mime := p.ImageMIME
				if mime == "" {
					mime = "image/png"
				}
				url = fmt.Sprintf("data:%s;base64,%s", mime, base64.StdEncoding.EncodeToString(p.ImageData))
			}
			if url == "" {
				continue
			}
			wire = append(wire, wirePart{Type: "image_url", ImageURL: &imageURL{URL: url}})
		}
	}
	out := struct {
		Role       Role       `json:"role"`
		Content    []wirePart `json:"content"`
		ToolCallID string     `json:"tool_call_id,omitempty"`
		ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	}{
		Role:       m.Role,
		Content:    wire,
		ToolCallID: m.ToolCallID,
		ToolCalls:  m.ToolCalls,
	}
	return json.Marshal(out)
}

// UnmarshalJSON restores string or multimodal array content into Message.
func (m *Message) UnmarshalJSON(b []byte) error {
	type alias Message
	raw := struct {
		alias
		Content json.RawMessage `json:"content"`
	}{}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*m = Message(raw.alias)
	if len(raw.Content) == 0 || string(raw.Content) == "null" {
		return nil
	}
	// String content
	if raw.Content[0] == '"' {
		return json.Unmarshal(raw.Content, &m.Content)
	}
	if raw.Content[0] != '[' {
		return nil
	}
	type imageURL struct {
		URL string `json:"url"`
	}
	type wirePart struct {
		Type     string    `json:"type"`
		Text     string    `json:"text,omitempty"`
		ImageURL *imageURL `json:"image_url,omitempty"`
	}
	var parts []wirePart
	if err := json.Unmarshal(raw.Content, &parts); err != nil {
		return err
	}
	for _, wp := range parts {
		switch wp.Type {
		case "text":
			if wp.Text != "" {
				m.Parts = append(m.Parts, ContentPart{Kind: PartText, Text: wp.Text})
			}
		case "image_url":
			if wp.ImageURL == nil || wp.ImageURL.URL == "" {
				continue
			}
			url := wp.ImageURL.URL
			if mime, data, ok := decodeDataURI(url); ok {
				m.Parts = append(m.Parts, ContentPart{
					Kind:      PartImage,
					ImageData: data,
					ImageMIME: mime,
				})
			} else {
				m.Parts = append(m.Parts, ContentPart{Kind: PartImage, ImageURL: url})
			}
		}
	}
	return nil
}

func decodeDataURI(url string) (mime string, data []byte, ok bool) {
	if !strings.HasPrefix(url, "data:") {
		return "", nil, false
	}
	rest := url[5:]
	semi := strings.Index(rest, ";")
	if semi < 0 {
		return "", nil, false
	}
	mime = rest[:semi]
	payload := rest[semi+1:]
	if !strings.HasPrefix(payload, "base64,") {
		return "", nil, false
	}
	b64 := payload[len("base64,"):]
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", nil, false
	}
	return mime, data, true
}

// ToolDef describes a callable tool in OpenAI "tools" format.
//
// ParallelSafe and Mutating are out-of-band metadata used by the agent loop
// to decide whether several tool calls in a single LLM response can run
// concurrently. They are NOT serialised on the wire — they only travel inside
// the process from the registry to the agent scheduler.
type ToolDef struct {
	Type     string          `json:"type"` // must be "function"
	Function ToolFunctionDef `json:"function"`

	// ParallelSafe is true when this tool can be invoked concurrently with
	// other ParallelSafe tools without ordering or shared-state risks. Pure
	// reads (fs.read, fs.list, search.text, code.symbols, glob) are safe.
	ParallelSafe bool `json:"-"`

	// Mutating is true when invoking this tool has observable side effects
	// (writes to disk, runs shell commands). Mutating tools always execute
	// one-at-a-time even within a single LLM-response batch.
	Mutating bool `json:"-"`
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

// GrammarFromJSONSchema builds a ResponseFormat for grammar-constrained sampling
// via OpenAI-compatible response_format type=json_schema.
func GrammarFromJSONSchema(schema []byte, name string) *ResponseFormat {
	if len(schema) == 0 {
		return nil
	}
	if name == "" {
		name = "response"
	}
	return &ResponseFormat{
		Type:       "json_schema",
		Schema:     schema,
		SchemaName: name,
	}
}

// CompleteRequest is a single chat completion request.
type CompleteRequest struct {
	Messages []Message
	Tools    []ToolDef
	// ResponseFormat, if non-nil, is sent as response_format (when the client
	// capability allows it). Prefer this for explicit json_object / json_schema.
	ResponseFormat *ResponseFormat
	// ResponseGrammar is a convenience alias for JSON Schema grammar-constrained
	// sampling. When ResponseFormat is nil and ResponseGrammar is non-empty, the
	// client treats it as Type=json_schema (see GrammarFromJSONSchema).
	ResponseGrammar []byte
}

// EffectiveResponseFormat returns ResponseFormat, or derives one from ResponseGrammar.
func (r CompleteRequest) EffectiveResponseFormat() *ResponseFormat {
	if r.ResponseFormat != nil {
		return r.ResponseFormat
	}
	return GrammarFromJSONSchema(r.ResponseGrammar, "agent_step")
}

// CompleteResponse is a single assistant turn (content and/or tool calls).
type CompleteResponse struct {
	Message Message
	// Usage carries token accounting reported by the provider for this turn.
	// Nil when the provider did not return usage info (some local servers omit it).
	Usage *TokenUsage
}

// TokenUsage is provider-reported token accounting for a single completion.
// Fields mirror OpenAI's "usage" object; other providers map their equivalents.
type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}
