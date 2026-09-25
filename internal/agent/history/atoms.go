package history

import "github.com/orchestra/orchestra/llm"

// Atom is an assistant↔tool group or a standalone message block.
type Atom struct {
	msgs []llm.Message
	size int
}

func AtomHasToolRole(a Atom) bool {
	for _, m := range a.msgs {
		if m.Role == llm.RoleTool {
			return true
		}
	}
	return false
}

// AtomMessages returns a copy of the atom's messages.
func AtomMessages(a Atom) []llm.Message {
	if len(a.msgs) == 0 {
		return nil
	}
	out := make([]llm.Message, len(a.msgs))
	copy(out, a.msgs)
	return out
}

// OrderToolReplies moves every message that sits between an assistant's
// tool_calls and one of that assistant's tool replies to after the last of
// those replies. Providers require the replies to follow the assistant
// message directly: a hint appended after the first of two serial calls (an
// LSP diagnostic, "staged ready") used to split the batch, the second reply
// became an orphan, and every later request of the session failed with 400.
// Histories saved before the fix are repaired on the next request. The input
// is returned as is when nothing needs moving.
func OrderToolReplies(messages []llm.Message) []llm.Message {
	var out []llm.Message // allocated on the first move
	for i := 0; i < len(messages); i++ {
		m := messages[i]
		if m.Role != llm.RoleAssistant || len(m.ToolCalls) == 0 {
			continue
		}
		open := make(map[string]bool, len(m.ToolCalls))
		for _, tc := range m.ToolCalls {
			if tc.ID != "" {
				open[tc.ID] = true
			}
		}
		// The window is everything up to the next assistant message.
		end := i + 1
		for end < len(messages) && messages[end].Role != llm.RoleAssistant {
			end++
		}
		window := messages[i+1 : end]
		lastReply := -1
		for j, w := range window {
			if w.Role == llm.RoleTool && open[w.ToolCallID] {
				lastReply = j
			}
		}
		interleaved := false
		for j := 0; j < lastReply; j++ {
			if !(window[j].Role == llm.RoleTool && open[window[j].ToolCallID]) {
				interleaved = true
				break
			}
		}
		if !interleaved {
			continue
		}
		if out == nil {
			out = make([]llm.Message, len(messages))
			copy(out, messages)
		}
		reordered := make([]llm.Message, 0, len(window))
		var moved []llm.Message
		for j, w := range window {
			if (w.Role == llm.RoleTool && open[w.ToolCallID]) || j > lastReply {
				reordered = append(reordered, w)
				if j == lastReply {
					reordered = append(reordered, moved...)
				}
				continue
			}
			moved = append(moved, w)
		}
		copy(out[i+1:end], reordered)
		messages = out
		i = end - 1
	}
	if out == nil {
		return messages
	}
	return out
}

// BuildHistoryAtoms groups messages (from index 0) into atoms preserving
// assistant+tool_call ↔ tool reply pairing — same semantics as TruncateMessages.
func BuildHistoryAtoms(messages []llm.Message) []Atom {
	messages = OrderToolReplies(messages)
	atoms := make([]Atom, 0, len(messages))
	cur := Atom{}
	openCalls := map[string]bool{}

	flush := func(a *Atom, open map[string]bool) {
		if len(a.msgs) == 0 {
			return
		}
		if len(open) > 0 {
			a.msgs = sanitizeOrphanedToolCalls(a.msgs, open)
			a.size = 0
			for _, mm := range a.msgs {
				a.size += EstimateMessageSize(mm)
			}
		}
		if len(a.msgs) > 0 {
			atoms = append(atoms, *a)
		}
	}

	for i := 0; i < len(messages); i++ {
		m := messages[i]
		ms := EstimateMessageSize(m)
		switch {
		case m.Role == llm.RoleTool && m.ToolCallID != "" && openCalls[m.ToolCallID]:
			cur.msgs = append(cur.msgs, m)
			cur.size += ms
			delete(openCalls, m.ToolCallID)
		case m.Role == llm.RoleAssistant && len(m.ToolCalls) > 0:
			flush(&cur, openCalls)
			cur = Atom{msgs: []llm.Message{m}, size: ms}
			openCalls = map[string]bool{}
			for _, tc := range m.ToolCalls {
				openCalls[tc.ID] = true
			}
		default:
			flush(&cur, openCalls)
			cur = Atom{msgs: []llm.Message{m}, size: ms}
			openCalls = map[string]bool{}
		}
	}
	flush(&cur, openCalls)
	return atoms
}

// ToolCallMeta maps a tool_call_id to the originating call name + input JSON.
type ToolCallMeta struct {
	name  string
	input []byte
}

func ToolCallMapFromAtom(a Atom) map[string]ToolCallMeta {
	out := map[string]ToolCallMeta{}
	for _, m := range a.msgs {
		if m.Role != llm.RoleAssistant {
			continue
		}
		for _, tc := range m.ToolCalls {
			out[tc.ID] = ToolCallMeta{
				name:  tc.Function.Name,
				input: tc.Function.Arguments.Raw(),
			}
		}
	}
	return out
}
