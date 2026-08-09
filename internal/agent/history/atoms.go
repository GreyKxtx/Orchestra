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

// BuildHistoryAtoms groups messages (from index 0) into atoms preserving
// assistant+tool_call ↔ tool reply pairing — same semantics as TruncateMessages.
func BuildHistoryAtoms(messages []llm.Message) []Atom {
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
