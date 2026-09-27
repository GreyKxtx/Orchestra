package llm

import "strings"

// Prompt caching: where the breakpoints go, in one place for both paths.
//
// The rule is the same whoever encodes it (docs/architecture/prompt-cache.md):
// the system block, the tool schemas, and the conversation up to but not
// including its last message — the agent appends the volatile part (working
// state, todos, reminders) last, so everything before it is a stable prefix
// each step reads from the cache and writes only what it appended. The
// native Anthropic client marks its converted blocks (markToolsCacheBreakpoint,
// markPrefixCacheBreakpoint: the breakpoint must land after tool results
// were merged and off the thinking blocks); the same models reached through
// an OpenAI-compatible gateway get an Anthropic-shaped cache_control block
// inside array-form content (markGatewayPromptCache) — OpenRouter forwards
// it to the underlying API verbatim — and a gateway that rejects the field
// turns the markers off for the client's life.
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

// ── Anthropic (native) ───────────────────────────────────────────────────────

// markToolsCacheBreakpoint caches the tool schemas, which are identical on
// every step of an agent run and are several KB of prompt.
func markToolsCacheBreakpoint(tools []anthropicTool) {
	if len(tools) == 0 {
		return
	}
	tools[len(tools)-1].CacheControl = &anthropicCacheControl{Type: "ephemeral"}
}

// markPrefixCacheBreakpoint caches the conversation up to (but not including)
// the last message.
//
// The agent rebuilds volatile context — working state, todos, reminders — on
// every step and appends it last, so the last message is the one part of the
// prompt that reliably differs between steps. Everything before it is a stable,
// append-only prefix: putting the breakpoint there makes each step read the
// previous step's history from cache and write only what was appended, instead
// of re-paying for the whole transcript.
func markPrefixCacheBreakpoint(msgs []anthropicMessage) {
	if len(msgs) < 2 {
		return
	}
	m := &msgs[len(msgs)-2]
	blocks := userContentBlocks(m.Content)
	if len(blocks) == 0 {
		if arr, ok := m.Content.([]anthropicBlock); ok {
			blocks = arr
		}
	}
	// Thinking blocks cannot carry cache_control: mark the last other one.
	for i := len(blocks) - 1; i >= 0; i-- {
		if t := blocks[i].Type; t == "thinking" || t == "redacted_thinking" {
			continue
		}
		blocks[i].CacheControl = &anthropicCacheControl{Type: "ephemeral"}
		m.Content = blocks
		return
	}
}
