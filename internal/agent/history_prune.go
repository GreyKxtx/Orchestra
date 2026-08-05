package agent

import (
	"encoding/json"
	"strings"

	"github.com/orchestra/orchestra/internal/llm"
)

const defaultHistoryPruneKeepRecent = 2

func isDigestedToolContent(content string) bool {
	return strings.HasPrefix(strings.TrimSpace(content), "[digest tool:")
}

// pruneRetroactiveToolHistory re-digests large tool outputs in older history
// atoms while keeping the last keepRecent tool-bearing atoms intact.
// OpenCode-style retroactive prune — complements write-time digest.
func pruneRetroactiveToolHistory(messages []llm.Message, digestBudget, keepRecent int) []llm.Message {
	if digestBudget <= 0 || len(messages) <= 2 {
		return messages
	}
	if keepRecent <= 0 {
		keepRecent = defaultHistoryPruneKeepRecent
	}

	prefix := messages[:2]
	rest := messages[2:]
	if len(rest) == 0 {
		return messages
	}

	atoms := buildHistoryAtoms(rest)
	toolAtomIdx := make([]int, 0, len(atoms))
	for i, a := range atoms {
		if atomHasToolRole(a) {
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

	outAtoms := make([]historyAtom, len(atoms))
	for i, a := range atoms {
		if protected[i] {
			outAtoms[i] = a
			continue
		}
		meta := toolCallMapFromAtom(a)
		cloned := historyAtom{msgs: make([]llm.Message, len(a.msgs)), size: 0}
		for j, m := range a.msgs {
			cloned.msgs[j] = m
			if m.Role != llm.RoleTool || m.ToolCallID == "" {
				cloned.size += estimateMessageSize(m)
				continue
			}
			cm, ok := meta[m.ToolCallID]
			if !ok {
				cloned.size += estimateMessageSize(m)
				continue
			}
			content := m.Content
			if !isDigestedToolContent(content) && len(content) > digestBudget {
				name := normalizeToolName(cm.name)
				if digested, ok := DigestToolOutput(name, json.RawMessage(cm.input), []byte(content), digestBudget); ok {
					content = digested
				}
			}
			if content != m.Content {
				cloned.msgs[j].Content = content
			}
			cloned.size += estimateMessageSize(cloned.msgs[j])
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
