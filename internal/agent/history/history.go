package history

import (
	"github.com/orchestra/orchestra/internal/llm"
)

// History truncation + sanitization. Extracted from agent.go in C3
// (architecture audit) so the byte-budget bookkeeping lives separately
// from the agent's control loop.

// sanitizeOrphanedToolCalls returns a copy of msgs where every tool_call
// whose ID is in orphans is removed from the first message (the assistant
// opener of an atom). If that leaves the assistant with no tool_calls AND
// no other content, the assistant message is dropped entirely so we don't
// emit an empty turn to the LLM. Subsequent messages in msgs are returned
// unchanged.
//
// N4 in audit ledger (Sprint 6): without this, an assistant that opens
// three tool_calls but only receives two replies (network glitch, mid-
// batch error) would keep the orphan in history forever, hard-failing
// every subsequent LLM step with "tool_call_id not found".
func sanitizeOrphanedToolCalls(msgs []llm.Message, orphans map[string]bool) []llm.Message {
	if len(msgs) == 0 || len(orphans) == 0 {
		return msgs
	}
	head := msgs[0]
	if head.Role != llm.RoleAssistant || len(head.ToolCalls) == 0 {
		return msgs
	}
	kept := make([]llm.ToolCall, 0, len(head.ToolCalls))
	for _, tc := range head.ToolCalls {
		if !orphans[tc.ID] {
			kept = append(kept, tc)
		}
	}
	head.ToolCalls = kept
	out := make([]llm.Message, 0, len(msgs))
	if len(kept) > 0 || head.TextLen() > 0 || len(head.Parts) > 0 {
		out = append(out, head)
	}
	out = append(out, msgs[1:]...)
	return out
}

// truncateMessages truncates message history to fit within byte budget.
// Always keeps system and first user message, then keeps as many history
// messages as possible from the tail, preserving every assistant↔tool group
// as one atom.
//
// "Atom" semantics: an assistant message that carries `tool_calls` MUST stay
// together with every subsequent `tool` message whose `tool_call_id` matches
// one of those calls — splitting them produces an orphaned `tool` (or a
// dangling assistant whose calls have no replies), which OpenAI / Anthropic
// reject with "tool_call_id not found", hard-failing the next LLM call and
// killing the whole run. We therefore build atoms first, then greedy-pick
// the most recent atoms whole.
//
// This replaces an earlier per-message loop that could keep a lone `tool`
// message when its paired assistant exceeded the budget (C1 in the audit).
//
// Complexity: O(n) in the number of messages. estimateMessageSize is called
// exactly once per message during atom-build and the cached atom.size is
// reused by the greedy-pick. The sanitize path re-estimates only the atoms
// that had orphan tool_calls — typically zero. P2 in audit ledger (Sprint 6)
// flagged a suspected double estimate; on re-reading the code, the second
// pass it claimed doesn't exist.
func TruncateMessages(messages []llm.Message, maxBytes int) []llm.Message {
	if maxBytes <= 0 || len(messages) <= 2 {
		return messages
	}

	// Always keep system (0) and first user (1)
	required := messages[:2]
	requiredSize := 0
	for _, m := range required {
		requiredSize += EstimateMessageSize(m)
	}

	if requiredSize >= maxBytes {
		return required
	}

	budget := maxBytes - requiredSize
	atoms := BuildHistoryAtoms(messages[2:])

	// Greedy-pick atoms from the tail until budget is exhausted.
	tailStart := len(atoms)
	size := 0
	for i := len(atoms) - 1; i >= 0; i-- {
		if size+atoms[i].size > budget {
			break
		}
		size += atoms[i].size
		tailStart = i
	}

	result := make([]llm.Message, 0, len(required)+len(messages)-2)
	result = append(result, required...)
	for i := tailStart; i < len(atoms); i++ {
		result = append(result, atoms[i].msgs...)
	}
	return result
}

// estimateMessageSize estimates the byte size of a message for truncation purposes.
// Accounts for Content/Parts text, ToolCalls (JSON serialization overhead), and ToolCallID.
// Image bytes are counted with a fixed per-image penalty rather than their raw
// base64 length — huge image payloads would otherwise dominate the budget and
// force compaction to evict useful tool results.
//
// M2 in audit ledger: the raw size estimate consistently undercounts real
// serialised JSON (per-tool-call overhead, role/content key names, escapes
// in strings). We apply a ×1.2 safety margin so the truncation budget
// stays below the real prompt size — overshooting MaxPromptBytes triggers
// LLM context-length errors that are hard to debug.
func EstimateMessageSize(msg llm.Message) int {
	size := msg.TextLen()
	for _, p := range msg.Parts {
		if p.Kind == llm.PartImage {
			size += 4096 // fixed image budget per part
		}
	}
	if msg.ToolCallID != "" {
		// ToolCallID adds to JSON size (field name + value)
		size += len(msg.ToolCallID) + 20 // approximate overhead for "tool_call_id":"..."
	}
	if len(msg.ToolCalls) > 0 {
		// Estimate tool_calls size: each tool call has id, type, function.name, function.arguments
		for _, tc := range msg.ToolCalls {
			size += len(tc.ID) + len(tc.Type) + len(tc.Function.Name)
			// Arguments are already in Content or as separate field, but add overhead
			size += len(tc.Function.Arguments.Raw()) + 50 // JSON structure overhead
		}
	}
	// Safety margin: real JSON serialisation adds role keys, content keys,
	// escapes, and per-message envelope. Without this ×1.2, truncate's
	// "fits in budget" picks atoms that actually overflow on the wire.
	return size * 12 / 10
}
