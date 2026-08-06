package history

import (
	"encoding/json"

	"github.com/orchestra/orchestra/internal/agent/digest"
	"github.com/orchestra/orchestra/internal/llm"
)

const DefaultHistoryPruneKeepRecent = 2

// PruneRetroactiveToolHistory re-digests large tool outputs in older history
// atoms while keeping the last keepRecent tool-bearing atoms intact.
// OpenCode-style retroactive prune — complements write-time digest.
func PruneRetroactiveToolHistory(messages []llm.Message, digestBudget, keepRecent int) []llm.Message {
	if digestBudget <= 0 || len(messages) <= 2 {
		return messages
	}
	if keepRecent <= 0 {
		keepRecent = DefaultHistoryPruneKeepRecent
	}

	prefix := messages[:2]
	rest := messages[2:]
	if len(rest) == 0 {
		return messages
	}

	atoms := BuildHistoryAtoms(rest)
	toolAtomIdx := make([]int, 0, len(atoms))
	for i, a := range atoms {
		if AtomHasToolRole(a) {
			toolAtomIdx = append(toolAtomIdx, i)
		}
	}
	if len(toolAtomIdx) == 0 {
		return messages
	}

	protectFrom := 0
	if len(toolAtomIdx) > keepRecent {
		protectFrom = toolAtomIdx[len(toolAtomIdx)-keepRecent]
	}
	protected := map[int]bool{}
	for _, idx := range toolAtomIdx {
		if idx >= protectFrom {
			protected[idx] = true
		}
	}

	outAtoms := make([]Atom, len(atoms))
	for i, a := range atoms {
		if protected[i] {
			outAtoms[i] = a
			continue
		}
		meta := ToolCallMapFromAtom(a)
		cloned := Atom{msgs: make([]llm.Message, len(a.msgs)), size: 0}
		for j, m := range a.msgs {
			cloned.msgs[j] = m
			if m.Role != llm.RoleTool || m.ToolCallID == "" {
				cloned.size += EstimateMessageSize(m)
				continue
			}
			cm, ok := meta[m.ToolCallID]
			if !ok {
				cloned.size += EstimateMessageSize(m)
				continue
			}
			content := m.Content
			if !digest.IsDigestedToolContent(content) && len(content) > digestBudget {
				name := digest.NormalizeToolName(cm.name)
				if digested, ok := digest.DigestToolOutput(name, json.RawMessage(cm.input), []byte(content), digestBudget); ok {
					content = digested
				}
			}
			if content != m.Content {
				cloned.msgs[j].Content = content
			}
			cloned.size += EstimateMessageSize(cloned.msgs[j])
		}
		outAtoms[i] = cloned
	}

	result := make([]llm.Message, 0, len(messages))
	result = append(result, prefix...)
	for _, a := range outAtoms {
		result = append(result, a.msgs...)
	}
	return result
}
