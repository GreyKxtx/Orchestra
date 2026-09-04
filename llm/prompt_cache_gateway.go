package llm

import "strings"

// Prompt caching through an OpenAI-compatible gateway.
//
// The native Anthropic client marks its own breakpoints (see anthropic.go).
// The same models reached through a gateway go out on the OpenAI-compatible
// path instead, where the only way to ask for caching is an Anthropic-shaped
// cache_control block inside array-form content — OpenRouter forwards it to
// the underlying API verbatim.
//
// Without this an agent step re-sends and re-pays for the entire transcript:
// one field turn spent 983k prompt tokens across 15 calls.

// gatewayPromptCacheSupported reports whether cache_control markers are safe
// to send for this model.
//
// Only the namespaced gateway form ("anthropic/claude-…") qualifies. Other
// providers cache by prefix automatically and gain nothing, and a self-hosted
// OpenAI-compatible server may reject the unknown field outright. A bare
// Anthropic model name means a direct endpoint, which uses the native client.
func gatewayPromptCacheSupported(model string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "anthropic/")
}

// markGatewayPromptCache returns a copy of msgs with cache-breakpoint markers
// on the system block and on the last message before the volatile tail.
//
// The agent rebuilds working state, todos and reminders on every step and
// appends them last, so the final message is the one part of the prompt that
// reliably differs between steps. Marking it could never hit; marking the one
// before it makes each step read the previous prefix from cache and write only
// what was appended.
func markGatewayPromptCache(msgs []Message) []Message {
	if len(msgs) == 0 {
		return msgs
	}
	out := append([]Message(nil), msgs...)
	for i := range out {
		if out[i].Role == RoleSystem {
			markMessageCached(&out[i])
			break
		}
	}
	if len(out) >= 2 {
		markMessageCached(&out[len(out)-2])
	}
	return out
}

// markMessageCached marks a message's last text block as a breakpoint.
// Messages with no text (an assistant turn that is only tool_calls) are left
// untouched: rewriting their content into an empty array would drop the calls.
func markMessageCached(m *Message) {
	if len(m.Parts) > 0 {
		for i := len(m.Parts) - 1; i >= 0; i-- {
			if m.Parts[i].Kind != PartText {
				continue
			}
			parts := append([]ContentPart(nil), m.Parts...)
			parts[i].CacheControl = true
			m.Parts = parts
			return
		}
		return
	}
	if strings.TrimSpace(m.Content) == "" {
		return
	}
	m.Parts = []ContentPart{{Kind: PartText, Text: m.Content, CacheControl: true}}
}

// isPromptCacheRejection reports whether an error looks like the endpoint
// refusing the cache_control field rather than failing for another reason.
func isPromptCacheRejection(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "cache_control")
}

// promptCacheMarkersEnabled reports whether this client should mark breakpoints.
func (c *OpenAIClient) promptCacheMarkersEnabled() bool {
	if c == nil || !gatewayPromptCacheSupported(c.model) {
		return false
	}
	c.supportsMu.Lock()
	defer c.supportsMu.Unlock()
	return !c.promptCacheDisabled
}

// disablePromptCacheMarkers stops marking for the rest of this client's life
// after the endpoint rejected the field. Every later step would fail the same
// way, so one retry buys back the whole run.
func (c *OpenAIClient) disablePromptCacheMarkers(reason string) {
	if c == nil {
		return
	}
	c.supportsMu.Lock()
	c.promptCacheDisabled = true
	c.supportsMu.Unlock()
	if c.logger != nil {
		c.logger.LogError(400, "prompt cache_control rejected — retrying without breakpoints: "+reason, 0)
	}
}
