package llm

import (
	"context"

	"github.com/orchestra/orchestra/internal/config"
)

// RouterClient is a thin Client wrapper that dispatches each Complete call to
// either a fast model (cheap, small context) or a main model (full-power) based
// on the size of the incoming prompt.
//
// Routing logic (intentionally simple):
//
//   - When the request carries tool definitions OR the rough prompt size
//     (sum of message bytes + tool args + tool def names) exceeds
//     ThresholdBytes → main client.
//   - Otherwise → fast client.
//
// This keeps trivial / one-shot chats on the cheap model without sacrificing
// fidelity on agent loops or long histories. Streaming is delegated identically
// when the wrapped clients implement Streamer.
type RouterClient struct {
	Main           Client
	Fast           Client
	ThresholdBytes int
	// AlwaysMainOnTools keeps tool-using calls on the main model even when the
	// prompt is small. Default: true (set false to allow tools on fast model).
	AlwaysMainOnTools bool
}

// NewRouterClient constructs a router. ThresholdBytes <= 0 disables routing
// (every call goes to Main) — useful when only one provider is configured.
func NewRouterClient(main, fast Client, thresholdBytes int) *RouterClient {
	return &RouterClient{
		Main:              main,
		Fast:              fast,
		ThresholdBytes:    thresholdBytes,
		AlwaysMainOnTools: true,
	}
}

// pick decides which underlying client a given request belongs to.
func (r *RouterClient) pick(req CompleteRequest) Client {
	if r.Fast == nil || r.ThresholdBytes <= 0 {
		return r.Main
	}
	if r.AlwaysMainOnTools && len(req.Tools) > 0 {
		return r.Main
	}
	size := estimatePromptBytes(req)
	if size > r.ThresholdBytes {
		return r.Main
	}
	return r.Fast
}

func estimatePromptBytes(req CompleteRequest) int {
	n := 0
	for _, m := range req.Messages {
		n += m.TextLen()
		for _, tc := range m.ToolCalls {
			n += len(tc.Function.Name) + len(tc.Function.Arguments)
		}
	}
	for _, t := range req.Tools {
		n += len(t.Function.Name) + len(t.Function.Description) + len(t.Function.Parameters)
	}
	return n
}

// Complete implements Client. Dispatches based on prompt size + tool presence.
func (r *RouterClient) Complete(ctx context.Context, req CompleteRequest) (*CompleteResponse, error) {
	return r.pick(req).Complete(ctx, req)
}

// Plan implements Client. Plans always go to Main (rare/expensive enough that
// routing isn't worth the risk).
func (r *RouterClient) Plan(ctx context.Context, prompt string) (string, error) {
	return r.Main.Plan(ctx, prompt)
}

// CompleteStream delegates to the picked client's streaming path when it
// supports Streamer; otherwise returns ErrStreamingUnsupported.
func (r *RouterClient) CompleteStream(ctx context.Context, req CompleteRequest) (<-chan StreamEvent, error) {
	c := r.pick(req)
	if s, ok := c.(Streamer); ok {
		return s.CompleteStream(ctx, req)
	}
	return nil, ErrStreamingUnsupported
}

// ErrStreamingUnsupported is returned when the routed-to client does not
// implement Streamer.
var ErrStreamingUnsupported = streamingUnsupportedError("routed client does not support streaming")

type streamingUnsupportedError string

func (e streamingUnsupportedError) Error() string { return string(e) }

// MaybeWrapRouter returns a RouterClient when cfg defines an enabled router
// targeting an existing fast provider; otherwise returns main unwrapped. This
// is the single hook NewClient callers use to opt in to routing.
func MaybeWrapRouter(main Client, cfg *config.ProjectConfig) Client {
	if cfg == nil || !cfg.LLM.Router.Enabled || cfg.LLM.Router.FastProvider == "" {
		return main
	}
	fastCfg, ok := cfg.FindProvider(cfg.LLM.Router.FastProvider)
	if !ok {
		return main
	}
	fast := NewClient(fastCfg)
	threshold := cfg.LLM.Router.ThresholdBytes
	if threshold <= 0 {
		threshold = 2048 // sensible default: ~500 tokens
	}
	return NewRouterClient(main, fast, threshold)
}
