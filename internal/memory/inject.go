package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (s *Store) FormatInject(maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = s.cfg.InjectBytes()
	}
	content := s.buildInjectContent(maxBytes)
	if content == "" {
		return ""
	}
	if len(content) > maxBytes {
		content = truncateToMax(content, maxBytes)
	}
	block := "<project_memory>\n" + content + "\n</project_memory>"
	if s.cfg.Mode == ModeLazy || s.cfg.Mode == ModeHybrid {
		block += "\n\n<memory_hint>\nUse memory_read to load additional project/session memory when needed.\n</memory_hint>"
	}
	return block
}

func (s *Store) buildInjectContent(maxBytes int) string {
	switch s.cfg.Mode {
	case ModeLazy:
		return s.sliceLayer(layerOrchestra, maxBytes/2)
	default:
		return s.tieredInject(maxBytes)
	}
}

// tieredInject allocates budget: orchestra 35%, session 25%, repo 30%, global remainder.
func (s *Store) tieredInject(maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	oBudget := maxBytes * 35 / 100
	sBudget := 0
	rBudget := maxBytes * 30 / 100
	gBudget := maxBytes - oBudget - rBudget
	if s.cfg.SessionEnabled && s.sessionID != "" {
		sBudget = maxBytes * 25 / 100
		gBudget = maxBytes - oBudget - sBudget - rBudget
	}

	var parts []string
	if chunk := s.sliceLayer(layerOrchestra, oBudget); chunk != "" {
		parts = append(parts, chunk)
	}
	if sBudget > 0 {
		if chunk := s.readSessionFile(sBudget); chunk != "" {
			parts = append(parts, "[session]\n"+chunk)
		}
	}
	if chunk := s.sliceRepoMemory(rBudget); chunk != "" {
		parts = append(parts, chunk)
	}
	if s.cfg.GlobalEnabled && gBudget > 0 {
		if chunk := s.sliceLayer(layerGlobal, gBudget); chunk != "" {
			parts = append(parts, chunk)
		}
	}
	return strings.Join(parts, "\n\n---\n\n")
}

func truncateToMax(s string, maxBytes int) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	const marker = "\n...(truncated)"
	keep := maxBytes - len(marker)
	if keep <= 0 {
		return marker[1:]
	}
	return tailBytes(s, keep) + marker
}

func (s *Store) sliceLayer(layer string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	raw := s.readLayerRaw(layer)
	if raw == "" {
		return ""
	}
	return truncateToMax(raw, maxBytes)
}

func (s *Store) sliceRepoMemory(maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	// Recent agent.md entries first, then other sorted repo files.
	var parts []string
	remaining := maxBytes

	agentPath := filepath.Join(s.workspaceRoot, ".orchestra", "memory", "agent.md")
	if data, err := os.ReadFile(agentPath); err == nil {
		entries := splitEntries(string(data))
		if len(entries) > 0 {
			recent := joinEntriesRecentFirst(entries)
			if len(recent) > remaining {
				recent = tailBytes(recent, remaining)
			}
			parts = append(parts, "[agent memory вЂ” recent first]\n"+recent)
			remaining -= len(recent)
		}
	}

	if remaining <= 0 {
		return strings.Join(parts, "\n\n")
	}

	memDir := filepath.Join(s.workspaceRoot, ".orchestra", "memory")
	entries, err := os.ReadDir(memDir)
	if err != nil {
		return strings.Join(parts, "\n\n")
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".md") || name == "agent.md" {
			continue
		}
		files = append(files, filepath.Join(memDir, name))
	}
	sort.Strings(files)
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		trimmed := strings.TrimSpace(string(data))
		if trimmed == "" {
			continue
		}
		label := filepath.Base(f)
		block := fmt.Sprintf("[%s]\n%s", label, trimmed)
		if len(block) > remaining {
			block = tailBytes(block, remaining)
		}
		parts = append(parts, block)
		remaining -= len(block)
		if remaining <= 0 {
			break
		}
	}
	return strings.Join(parts, "\n\n---\n\n")
}

func (s *Store) LazyOrchestra(dir string) string {
	if s.workspaceRoot == "" {
		return ""
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	for {
		candidate := filepath.Join(absDir, "ORCHESTRA.md")
		if data, err := os.ReadFile(candidate); err == nil {
			raw := strings.TrimSpace(string(data))
			if raw == "" {
				return ""
			}
			cap := s.cfg.LazyBytes()
			return truncateToMax(raw, cap)
		}
		if absDir == s.workspaceRoot {
			break
		}
		parent := filepath.Dir(absDir)
		if parent == absDir {
			break
		}
		absDir = parent
	}
	return ""
}
