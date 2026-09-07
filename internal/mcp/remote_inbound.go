package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// remoteClientOptions builds the SDK client options that serve a remote
// server's inbound requests.
//
// The SDK derives the advertised capabilities from which handlers are
// non-nil, so a server the user did not opt in for is never offered the
// capability in the first place — the same promise-only-what-you-serve rule
// the stdio client follows, expressed through the SDK's own mechanism.
//
// Both handlers marshal the SDK's params back into the wire shape and go
// through newInboundHandler, so the config gate, the consent gate, the token
// cap and the refusal wording are identical across transports. Duplicating
// that logic per transport is how the two would drift, and the half that
// drifts is a gate.
func remoteClientOptions(server string, in InboundOptions) *mcpsdk.ClientOptions {
	if !in.AllowSampling && !in.AllowElicitation {
		return nil
	}
	h := newInboundHandler(server, in)
	opts := &mcpsdk.ClientOptions{}

	if in.AllowSampling && in.Sample != nil {
		opts.CreateMessageHandler = func(ctx context.Context, req *mcpsdk.CreateMessageRequest) (*mcpsdk.CreateMessageResult, error) {
			params, err := json.Marshal(req.Params)
			if err != nil {
				return nil, fmt.Errorf("mcp %q: encoding sampling params: %w", server, err)
			}
			out, rerr := h(ctx, "sampling/createMessage", params)
			if rerr != nil {
				return nil, fmt.Errorf("%s", rerr.Message)
			}
			m, _ := out.(map[string]any)
			text, _ := m["content"].(map[string]any)["text"].(string)
			model, _ := m["model"].(string)
			stop, _ := m["stopReason"].(string)
			return &mcpsdk.CreateMessageResult{
				Model:      model,
				Role:       "assistant",
				Content:    &mcpsdk.TextContent{Text: text},
				StopReason: stop,
			}, nil
		}
	}

	if in.AllowElicitation {
		opts.ElicitationHandler = func(ctx context.Context, req *mcpsdk.ElicitRequest) (*mcpsdk.ElicitResult, error) {
			params, err := json.Marshal(req.Params)
			if err != nil {
				return nil, fmt.Errorf("mcp %q: encoding elicitation params: %w", server, err)
			}
			out, rerr := h(ctx, "elicitation/create", params)
			if rerr != nil {
				return nil, fmt.Errorf("%s", rerr.Message)
			}
			m, _ := out.(map[string]any)
			action, _ := m["action"].(string)
			content, _ := m["content"].(map[string]any)
			return &mcpsdk.ElicitResult{Action: action, Content: content}, nil
		}
	}
	return opts
}
