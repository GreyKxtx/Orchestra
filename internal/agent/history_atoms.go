package agent

import "github.com/orchestra/orchestra/internal/llm"

// historyAtom is an assistant↔tool group or a standalone message block.
type historyAtom struct {
	msgs []llm.Message
	size int
}

func atomHasToolRole(a historyAtom) bool {
	for _, m := range a.msgs {
		if m.Role == llm.RoleTool {
			return true
		}
	}
	return false
}

// buildHistoryAtoms groups messages (from index 0) into atoms preserving
// assistant+tool_call ↔ tool reply pairing — same semantics as truncateMessages.
func buildHistoryAtoms(messages []llm.Message) []historyAtom {
	atoms := make([]historyAtom, 0, len(messages))
	cur := historyAtom{}
	openCalls := map[string]bool{}

	flush := func(a *historyAtom, open map[string]bool) {
		if len(a.msgs) == 0 {
			return
		}
		if len(open) > 0 {
			a.msgs = sanitizeOrphanedToolCalls(a.msgs, open)
			a.size = 0
			for _, mm := range a.msgs {
				a.size += estimateMessageSize(mm)
			}
		}
		if len(a.msgs) > 0 {
			atoms = append(atoms, *a)
		}
	}

	for i := 0; i < len(messages); i++ {
		m := messages[i]
		ms := estimateMessageSize(m)
		switch {
		case m.Role == llm.RoleTool && m.ToolCallID != "" && openCalls[m.ToolCallID]:
			cur.msgs = append(cur.msgs, m)
			cur.size += ms
			delete(openCalls, m.ToolCallID)
		case m.Role == llm.RoleAssistant && len(m.ToolCalls) > 0:
			flush(&cur, openCalls)
			cur = historyAtom{msgs: []llm.Message{m}, size: ms}
			openCalls = map[string]bool{}
			for _, tc := range m.ToolCalls {
				openCalls[tc.ID] = true
			}
		default:
			flush(&cur, openCalls)
			cur = historyAtom{msgs: []llm.Message{m}, size: ms}
			openCalls = map[string]bool{}
		}
	}
	flush(&cur, openCalls)
	return atoms
}

// toolCallMeta maps a tool_call_id to the originating call name + input JSON.
type toolCallMeta struct {
	name  string
	input []byte
}

func toolCallMapFromAtom(a historyAtom) map[string]toolCallMeta {
	out := map[string]toolCallMeta{}
	for _, m := range a.msgs {
		if m.Role != llm.RoleAssistant {
			continue
		}
		for _, tc := range m.ToolCalls {
			out[tc.ID] = toolCallMeta{
				name:  tc.Function.Name,
				input: tc.Function.Arguments.Raw(),
			}
		}
	}
	return out
}
