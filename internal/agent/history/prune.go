package history

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/orchestra/orchestra/internal/agent/digest"
	"github.com/orchestra/orchestra/internal/llm"
)

const DefaultHistoryPruneKeepRecent = 2

// PruneRetroactiveToolHistory re-digests large tool outputs in older history
// atoms while keeping the last keepRecent tool-bearing atoms intact.
// OpenCode-style retroactive prune — complements write-time digest.
//
// protectPaths (optional): tool results whose input path matches any entry
// (exact or path suffix) are kept full even outside the keep-recent window —
// active/pinned files should not be evicted mid-turn.
func PruneRetroactiveToolHistory(messages []llm.Message, digestBudget, keepRecent int, protectPaths ...string) []llm.Message {
	if digestBudget <= 0 || len(messages) <= 2 {
		return messages
	}
	if keepRecent <= 0 {
		keepRecent = DefaultHistoryPruneKeepRecent
	}
	protect := normalizeProtectPaths(protectPaths)

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
			if pathProtected(protect, pathFromToolInput(cm.input)) ||
				pathProtected(protect, pathFromToolContent(content)) {
				cloned.size += EstimateMessageSize(m)
				continue
			}
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

func normalizeProtectPaths(paths []string) map[string]bool {
	if len(paths) == 0 {
		return nil
	}
	out := make(map[string]bool, len(paths))
	for _, p := range paths {
		p = filepath.ToSlash(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		out[strings.ToLower(p)] = true
		if base := filepath.Base(p); base != "" && base != "." && base != "/" {
			out[strings.ToLower(base)] = true
		}
	}
	return out
}

func pathProtected(protect map[string]bool, path string) bool {
	if len(protect) == 0 {
		return false
	}
	path = filepath.ToSlash(strings.TrimSpace(path))
	if path == "" {
		return false
	}
	low := strings.ToLower(path)
	if protect[low] {
		return true
	}
	if base := strings.ToLower(filepath.Base(path)); base != "" && protect[base] {
		return true
	}
	for p := range protect {
		if !strings.Contains(p, "/") {
			continue
		}
		if low == p || strings.HasSuffix(low, "/"+p) {
			return true
		}
	}
	return false
}

func pathFromToolInput(input []byte) string {
	if len(input) == 0 {
		return ""
	}
	var m map[string]any
	if json.Unmarshal(input, &m) != nil {
		return ""
	}
	for _, k := range []string{"path", "file", "file_path", "target"} {
		if v, ok := m[k].(string); ok && strings.TrimSpace(v) != "" {
			return filepath.ToSlash(strings.TrimSpace(v))
		}
	}
	return ""
}

func pathFromToolContent(content string) string {
	if !strings.Contains(content, `"path"`) {
		return ""
	}
	var resp struct {
		Path string `json:"path"`
	}
	if json.Unmarshal([]byte(content), &resp) != nil {
		return ""
	}
	return filepath.ToSlash(strings.TrimSpace(resp.Path))
}
