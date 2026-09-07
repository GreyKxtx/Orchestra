package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/orchestra/orchestra/internal/mcp"
	"github.com/orchestra/orchestra/internal/permission"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
)

// mcpHost is the seam between the MCP servers this process runs and the
// person and model they may ask for. It answers the two requests a server can
// make of its client — sampling/createMessage and elicitation/create — over
// the channels Orchestra already has for talking to the user: the
// permission/request and question/ask server-initiated RPCs that the TUI and
// the IDE extension both implement. No new protocol method, so every client
// that exists today already supports this.
//
// Lifetimes: MCP servers start once with the Core and outlive every turn; the
// client's request channel appears once, when the RPC handler is attached,
// and also outlives every turn. Both halves live as long as the process, so
// the host binds them once rather than swapping per turn (which would race
// between concurrent sessions). The model is resolved at call time, because
// runtime.set_model can swap it under a running server.
type mcpHost struct {
	mu        sync.RWMutex
	requester permission.Requester // nil until a client attaches → fails closed
	asker     tools.QuestionAsker  // nil until a client attaches → declines
	// always remembers "approve, and stop asking" per server AND per kind.
	// Trusting a server with the model is not trusting it with the user's
	// attention, so the two are separate keys.
	always map[string]bool
	// model returns the client to sample with and its label for the reply.
	model func() (llm.Client, string)
}

func newMCPHost(model func() (llm.Client, string)) *mcpHost {
	return &mcpHost{always: map[string]bool{}, model: model}
}

// bind attaches the interactive channel. Called once per client connection.
func (h *mcpHost) bind(consent permission.Requester, ask tools.QuestionAsker) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.requester = consent
	h.asker = ask
}

// hooks is what mcp.NewManager takes.
func (h *mcpHost) hooks() mcp.Hooks {
	return mcp.Hooks{Consent: h.consent, Sample: h.sample, Elicit: h.elicit}
}

// mcpHooks is what a restarted Manager (ReplaceMCP) is handed. A Core built
// by hand in tests may have no host; those servers then serve nothing, which
// is the safe default.
func (c *Core) mcpHooks() mcp.Hooks {
	if c == nil || c.mcpHost == nil {
		return mcp.Hooks{}
	}
	return c.mcpHost.hooks()
}

// NewMCPHooks builds the same hooks for a caller that has its channels in hand
// from the start — the CLI's apply, where the terminal is the client.
func NewMCPHooks(model func() (llm.Client, string), consent permission.Requester, ask tools.QuestionAsker) mcp.Hooks {
	h := newMCPHost(model)
	h.bind(consent, ask)
	return h.hooks()
}

// consent is the mcp.ConsentFunc: one permission/request per ask, unless the
// client answered "always" for this server and kind earlier.
func (h *mcpHost) consent(ctx context.Context, req mcp.ConsentRequest) bool {
	key := req.Server + "\x00" + req.Kind
	h.mu.RLock()
	requester := h.requester
	remembered := h.always[key]
	h.mu.RUnlock()
	if remembered {
		return true
	}
	if requester == nil {
		return false
	}
	resp, err := requester.RequestPermission(ctx, permission.Request{
		// The tool name is what every client shows first. "mcp:linear" says
		// who is asking; the kind says for what.
		Tool:        "mcp:" + req.Server,
		Description: req.Summary,
		Reason:      consentReason(req.Kind),
		Kind:        req.Kind,
	})
	if err != nil || !resp.Approved {
		return false
	}
	if resp.Always {
		h.mu.Lock()
		h.always[key] = true
		h.mu.Unlock()
	}
	return true
}

func consentReason(kind string) string {
	switch kind {
	case mcp.ConsentSampling:
		return "the MCP server wants to run a completion on your configured model"
	case mcp.ConsentElicitation:
		return "the MCP server wants to ask you a question"
	}
	return ""
}

// sample runs the server's prompt on the configured model. Orchestra's tools
// are deliberately not offered: the server wrote this prompt, and it must not
// be able to drive the agent's tools through it. The model's own configured
// max_tokens applies; the server's requested ceiling was already clamped by
// the mcp package.
func (h *mcpHost) sample(ctx context.Context, req mcp.SamplingRequest) (mcp.SamplingResult, error) {
	if h.model == nil {
		return mcp.SamplingResult{}, errors.New("no model configured for sampling")
	}
	client, label := h.model()
	if client == nil {
		return mcp.SamplingResult{}, errors.New("no model configured for sampling")
	}
	msgs := make([]llm.Message, 0, len(req.Messages)+1)
	if req.SystemPrompt != "" {
		msgs = append(msgs, llm.Message{Role: llm.RoleSystem, Content: req.SystemPrompt})
	}
	for _, m := range req.Messages {
		role := llm.RoleUser
		if m.Role == "assistant" {
			role = llm.RoleAssistant
		}
		msgs = append(msgs, llm.Message{Role: role, Content: m.Text})
	}
	resp, err := client.Complete(ctx, llm.CompleteRequest{Messages: msgs})
	if err != nil {
		return mcp.SamplingResult{}, err
	}
	if resp == nil {
		return mcp.SamplingResult{}, errors.New("model returned no completion")
	}
	return mcp.SamplingResult{Model: label, Text: resp.Message.Content, StopReason: "endTurn"}, nil
}

// elicit turns the server's schema into a questionnaire, asks it through the
// client, and turns the typed answers back into the types the schema named.
func (h *mcpHost) elicit(ctx context.Context, req mcp.ElicitationRequest) (mcp.ElicitationResult, error) {
	fields, err := elicitationFields(req.Schema)
	if err != nil {
		return mcp.ElicitationResult{}, fmt.Errorf("elicitation schema: %w", err)
	}
	h.mu.RLock()
	asker := h.asker
	h.mu.RUnlock()
	if asker == nil {
		return mcp.ElicitationResult{Action: "decline"}, nil
	}
	answers, err := asker.Ask(ctx, elicitationQuestions(req.Message, fields))
	if err != nil {
		// The client went away or has no handler: the dialog never reached a
		// person, which is what "cancel" means.
		return mcp.ElicitationResult{Action: "cancel"}, nil
	}
	content, action := elicitationContent(fields, answers)
	return mcp.ElicitationResult{Action: action, Content: content}, nil
}

// elicitField is one property of the requested schema, in schema order.
type elicitField struct {
	name        string
	typ         string // string | number | integer | boolean
	title       string
	description string
	options     []string // enum values; "yes"/"no" for booleans
	required    bool
}

// elicitationFields reads the flat object schema the MCP spec allows for
// elicitation. Property order is preserved by walking the JSON tokens: a Go
// map would shuffle the questions the server wrote in a deliberate sequence.
func elicitationFields(schema json.RawMessage) ([]elicitField, error) {
	if len(strings.TrimSpace(string(schema))) == 0 {
		return nil, nil
	}
	var top struct {
		Properties map[string]struct {
			Type        string   `json:"type"`
			Title       string   `json:"title"`
			Description string   `json:"description"`
			Enum        []string `json:"enum"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(schema, &top); err != nil {
		return nil, err
	}
	order, err := propertyOrder(schema)
	if err != nil {
		return nil, err
	}
	required := map[string]bool{}
	for _, r := range top.Required {
		required[r] = true
	}
	fields := make([]elicitField, 0, len(order))
	for _, name := range order {
		p := top.Properties[name]
		f := elicitField{
			name:        name,
			typ:         p.Type,
			title:       p.Title,
			description: p.Description,
			required:    required[name],
		}
		switch {
		case len(p.Enum) > 0:
			f.options = append([]string(nil), p.Enum...)
		case p.Type == "boolean":
			f.options = []string{"yes", "no"}
		}
		fields = append(fields, f)
	}
	return fields, nil
}

// propertyOrder returns the keys of "properties" in document order.
func propertyOrder(schema json.RawMessage) ([]string, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(schema, &raw); err != nil {
		return nil, err
	}
	props, ok := raw["properties"]
	if !ok {
		return nil, nil
	}
	dec := json.NewDecoder(strings.NewReader(string(props)))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if tok != json.Delim('{') {
		return nil, fmt.Errorf("properties is not an object")
	}
	var order []string
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return nil, err
		}
		name, _ := key.(string)
		order = append(order, name)
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return nil, err
		}
	}
	return order, nil
}

// elicitationQuestions renders the fields as questions. The server's message
// rides on the first question only: the TUI shows one question at a time and
// the context belongs at the top, not repeated four times.
func elicitationQuestions(message string, fields []elicitField) []tools.QuestionItem {
	message = strings.TrimSpace(message)
	if len(fields) == 0 {
		// Nothing to fill in: the message itself is the question.
		return []tools.QuestionItem{{Question: message, Options: []string{"ok", "decline"}}}
	}
	qs := make([]tools.QuestionItem, 0, len(fields))
	for i, f := range fields {
		label := f.title
		if label == "" {
			label = f.name
		}
		prompt := label
		if f.description != "" {
			prompt += ": " + f.description
		}
		if f.required {
			prompt += " (required)"
		}
		if i == 0 && message != "" {
			prompt = message + "\n\n" + prompt
		}
		qs = append(qs, tools.QuestionItem{Question: prompt, Options: append([]string(nil), f.options...)})
	}
	return qs
}

// elicitationContent maps typed answers onto the schema. The clients send the
// raw text the user typed, so "2" against a pick list is the second option and
// "да" against a boolean is true. Returns the spec's action: accept, decline
// (a required field was left blank, or the confirmation was refused), or
// cancel (the dialog never produced answers).
func elicitationContent(fields []elicitField, answers []string) (map[string]any, string) {
	if len(answers) == 0 {
		return nil, "cancel"
	}
	if len(fields) == 0 {
		if strings.EqualFold(strings.TrimSpace(answers[0]), "ok") {
			return map[string]any{}, "accept"
		}
		return nil, "decline"
	}
	content := map[string]any{}
	for i, f := range fields {
		if i >= len(answers) {
			if f.required {
				return nil, "decline"
			}
			continue
		}
		ans := strings.TrimSpace(answers[i])
		if ans == "" {
			if f.required {
				return nil, "decline"
			}
			continue
		}
		v, ok := coerceAnswer(f, ans)
		if !ok {
			if f.required {
				return nil, "decline"
			}
			continue
		}
		content[f.name] = v
	}
	return content, "accept"
}

func coerceAnswer(f elicitField, ans string) (any, bool) {
	// A number typed against a pick list is a 1-based choice — unless the
	// number is itself one of the options.
	if len(f.options) > 0 {
		if idx, err := strconv.Atoi(ans); err == nil && idx >= 1 && idx <= len(f.options) && !hasOption(f.options, ans) {
			ans = f.options[idx-1]
		} else if canon, ok := matchOption(f.options, ans); ok {
			ans = canon
		}
	}
	switch f.typ {
	case "boolean":
		switch strings.ToLower(ans) {
		case "yes", "y", "true", "1", "да", "д":
			return true, true
		case "no", "n", "false", "0", "нет", "н":
			return false, true
		}
		return nil, false
	case "integer":
		n, err := strconv.ParseInt(ans, 10, 64)
		return n, err == nil
	case "number":
		x, err := strconv.ParseFloat(ans, 64)
		return x, err == nil
	}
	return ans, true
}

func hasOption(options []string, ans string) bool {
	_, ok := matchOption(options, ans)
	return ok
}

// matchOption returns the option in its schema spelling, matched case-insensitively.
func matchOption(options []string, ans string) (string, bool) {
	for _, o := range options {
		if strings.EqualFold(o, ans) {
			return o, true
		}
	}
	return "", false
}
