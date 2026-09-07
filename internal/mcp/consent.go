package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// maxSamplingTokens caps what one sampling request may ask for, whatever the
// server asked. The server names the number and the user pays it; without a
// ceiling a single tools/call could turn into an arbitrarily expensive
// completion.
const maxSamplingTokens = 4096

// Consent kinds, distinct because the two questions are different: one spends
// the user's tokens, the other puts a server's words in front of the user.
const (
	ConsentSampling    = "mcp.sampling"
	ConsentElicitation = "mcp.elicitation"
)

// ConsentRequest is what the user is being asked to allow.
type ConsentRequest struct {
	// Server is the configured MCP server name. Every consent prompt names
	// it: "something wants to use your model" is not a question anyone can
	// answer.
	Server string
	Kind   string
	// Summary is a one-line description of the specific request.
	Summary string
}

// ConsentFunc returns true when the user allows this request. A nil
// ConsentFunc refuses everything: a seam that spends money and prompts people
// fails closed.
type ConsentFunc func(context.Context, ConsentRequest) bool

// SamplingMessage is one message in a server's sampling request.
type SamplingMessage struct {
	Role string
	Text string
}

// SamplingRequest is a server asking Orchestra to run an LLM call.
type SamplingRequest struct {
	Server       string
	SystemPrompt string
	Messages     []SamplingMessage
	MaxTokens    int
}

// SamplingResult is Orchestra's answer.
type SamplingResult struct {
	Model      string
	Text       string
	StopReason string
}

// SampleFunc runs the completion. nil means this client cannot sample.
type SampleFunc func(context.Context, SamplingRequest) (SamplingResult, error)

// ElicitationRequest is a server asking the user for structured input.
type ElicitationRequest struct {
	Server  string
	Message string
	// Schema is the server's requested JSON schema, passed through unparsed.
	Schema json.RawMessage
}

// ElicitationResult mirrors the spec's three outcomes: accept, decline, cancel.
type ElicitationResult struct {
	Action  string
	Content map[string]any
}

// ElicitFunc asks the user. nil means there is nobody to ask.
type ElicitFunc func(context.Context, ElicitationRequest) (ElicitationResult, error)

// InboundOptions configures how one server's requests to us are answered.
type InboundOptions struct {
	// AllowSampling and AllowElicitation come from the server's own config
	// block. Off by default: an MCP server is third-party code, and these two
	// requests are how it reaches the user's wallet and the user's attention.
	AllowSampling    bool
	AllowElicitation bool

	Consent ConsentFunc
	Sample  SampleFunc
	Elicit  ElicitFunc
}

// newInboundHandler builds the handler for one server's inbound requests.
//
// Every path is gated twice: the server must be opted in through config, and
// the user must consent to the individual request. Config alone is permission
// to ask, not permission to proceed — a server that was trustworthy when it
// was added is not thereby trusted with every request it will ever make.
func newInboundHandler(server string, opts InboundOptions) inboundHandler {
	return func(ctx context.Context, method string, params json.RawMessage) (any, *rpcError) {
		switch method {
		case "sampling/createMessage":
			return handleSampling(ctx, server, opts, params)
		case "elicitation/create":
			return handleElicitation(ctx, server, opts, params)
		default:
			return nil, &rpcError{Code: rpcMethodNotFound, Message: "method not supported by this client: " + method}
		}
	}
}

func handleSampling(ctx context.Context, server string, opts InboundOptions, params json.RawMessage) (any, *rpcError) {
	if !opts.AllowSampling || opts.Sample == nil {
		return nil, &rpcError{
			Code: rpcMethodNotFound,
			Message: fmt.Sprintf("sampling is not enabled for mcp server %q "+
				"(set allow_sampling: true on it to permit asking)", server),
		}
	}
	var p struct {
		MaxTokens    int    `json:"maxTokens"`
		SystemPrompt string `json:"systemPrompt"`
		Messages     []struct {
			Role    string `json:"role"`
			Content struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpcError{Code: rpcInternalError, Message: "malformed sampling params: " + err.Error()}
	}

	req := SamplingRequest{
		Server:       server,
		SystemPrompt: p.SystemPrompt,
		MaxTokens:    p.MaxTokens,
	}
	if req.MaxTokens <= 0 || req.MaxTokens > maxSamplingTokens {
		req.MaxTokens = maxSamplingTokens
	}
	for _, m := range p.Messages {
		// Only text is carried. The spec allows image and audio content in a
		// sampling request; forwarding those means handing a server's binary
		// payload to the user's model, and no server observed needs it.
		if m.Content.Type != "" && m.Content.Type != "text" {
			continue
		}
		req.Messages = append(req.Messages, SamplingMessage{Role: m.Role, Text: m.Content.Text})
	}

	if !granted(ctx, opts.Consent, ConsentRequest{
		Server:  server,
		Kind:    ConsentSampling,
		Summary: samplingSummary(req),
	}) {
		return nil, &rpcError{
			Code:    rpcMethodNotFound,
			Message: fmt.Sprintf("sampling declined by the user for mcp server %q", server),
		}
	}

	res, err := opts.Sample(ctx, req)
	if err != nil {
		return nil, &rpcError{Code: rpcInternalError, Message: "sampling failed: " + err.Error()}
	}
	stop := res.StopReason
	if stop == "" {
		stop = "endTurn"
	}
	return map[string]any{
		"model":      res.Model,
		"role":       "assistant",
		"content":    map[string]any{"type": "text", "text": res.Text},
		"stopReason": stop,
	}, nil
}

func handleElicitation(ctx context.Context, server string, opts InboundOptions, params json.RawMessage) (any, *rpcError) {
	if !opts.AllowElicitation {
		return nil, &rpcError{
			Code: rpcMethodNotFound,
			Message: fmt.Sprintf("elicitation is not enabled for mcp server %q "+
				"(set allow_elicitation: true on it to permit asking)", server),
		}
	}
	var p struct {
		Message string          `json:"message"`
		Schema  json.RawMessage `json:"requestedSchema"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpcError{Code: rpcInternalError, Message: "malformed elicitation params: " + err.Error()}
	}

	// Enabled, but nobody to ask — orchestra core with no interactive channel.
	// "decline" is the spec's own outcome for "the user did not accept" and
	// lets the server carry on; an error would read as a client fault and
	// invites the server to retry.
	if opts.Elicit == nil {
		return map[string]any{"action": "decline"}, nil
	}

	if !granted(ctx, opts.Consent, ConsentRequest{
		Server:  server,
		Kind:    ConsentElicitation,
		Summary: firstLine(p.Message, 120),
	}) {
		return map[string]any{"action": "decline"}, nil
	}

	res, err := opts.Elicit(ctx, ElicitationRequest{Server: server, Message: p.Message, Schema: p.Schema})
	if err != nil {
		return nil, &rpcError{Code: rpcInternalError, Message: "elicitation failed: " + err.Error()}
	}
	action := res.Action
	if action == "" {
		action = "cancel"
	}
	out := map[string]any{"action": action}
	if action == "accept" && res.Content != nil {
		out["content"] = res.Content
	}
	return out, nil
}

// granted fails closed: no consent function means no way to obtain consent,
// which is not the same as consent.
func granted(ctx context.Context, f ConsentFunc, req ConsentRequest) bool {
	if f == nil {
		return false
	}
	return f(ctx, req)
}

func samplingSummary(req SamplingRequest) string {
	var last string
	if n := len(req.Messages); n > 0 {
		last = req.Messages[n-1].Text
	}
	return fmt.Sprintf("%d message(s), up to %d tokens: %s",
		len(req.Messages), req.MaxTokens, firstLine(last, 100))
}

func firstLine(s string, max int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	r := []rune(s)
	if len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}
