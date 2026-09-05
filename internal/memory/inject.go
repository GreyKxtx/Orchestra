package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (s *Store) FormatInject(maxBytes int) string {
	block, _, _ := s.FormatInjectReport(maxBytes)
	return block
}

// FormatInjectReport is FormatInject plus a compact per-layer byte
// breakdown, e.g. "orchestra=512B repo=204B global=0B total=716B/2048B" —
// what /memory refresh in the TUI and llm_log.jsonl's memory.inject event
// answer "what actually got injected on this turn" from. Without it that
// question is answerable only by re-deriving budgets from config and
// guessing at file sizes.
func (s *Store) FormatInjectReport(maxBytes int) (block, detail string, totalBytes int) {
	if maxBytes <= 0 {
		maxBytes = s.cfg.InjectBytes()
	}
	content, stats := s.buildInjectContentReport(maxBytes)
	totalBytes = len(content)
	detail = formatInjectDetail(stats, totalBytes, maxBytes)
	if content == "" {
		return "", detail, 0
	}
	if totalBytes > maxBytes {
		content = truncateToMax(content, maxBytes)
	}
	block = "<project_memory>\n" + content + "\n</project_memory>"
	if s.cfg.Mode == ModeLazy || s.cfg.Mode == ModeHybrid {
		block += "\n\n<memory_hint>\nUse memory_read to load additional project/session memory when needed.\n</memory_hint>"
	}
	return block, detail, totalBytes
}

// layerStat is one layer's contribution to an inject pass, for reporting.
type layerStat struct {
	name  string
	bytes int
}

func formatInjectDetail(stats []layerStat, totalBytes, budget int) string {
	var b strings.Builder
	for _, st := range stats {
		fmt.Fprintf(&b, "%s=%dB ", st.name, st.bytes)
	}
	fmt.Fprintf(&b, "total=%dB/%dB", totalBytes, budget)
	return b.String()
}

// injectScope selects which layers an inject pass is allowed to reach. It is
// what separates hybrid from eager: hybrid keeps the prompt to the layers a
// small local model needs on every step and leaves the rest to memory_read.
type injectScope struct {
	global    bool // ~/.orchestra/memory.md
	repoFiles bool // .orchestra/memory/*.md beyond agent.md
}

// fullScope reaches every layer — eager inject and memory_read layer=all.
func fullScope() injectScope { return injectScope{global: true, repoFiles: true} }

func (s *Store) buildInjectContent(maxBytes int) string {
	content, _ := s.buildInjectContentReport(maxBytes)
	return content
}

func (s *Store) buildInjectContentReport(maxBytes int) (string, []layerStat) {
	switch s.cfg.Mode {
	case ModeLazy:
		chunk := s.sliceLayer(layerOrchestra, maxBytes/2)
		if chunk == "" {
			return "", nil
		}
		return chunk, []layerStat{{layerOrchestra, len(chunk)}}
	case ModeHybrid:
		return s.tieredInjectReport(maxBytes, injectScope{})
	default:
		return s.tieredInjectReport(maxBytes, fullScope())
	}
}

// tieredInject allocates budget: orchestra 35%, session 25%, repo 30%, global remainder.
func (s *Store) tieredInject(maxBytes int, scope injectScope) string {
	content, _ := s.tieredInjectReport(maxBytes, scope)
	return content
}

func (s *Store) tieredInjectReport(maxBytes int, scope injectScope) (string, []layerStat) {
	if maxBytes <= 0 {
		return "", nil
	}
	oBudget := maxBytes * 35 / 100
	sBudget := 0
	rBudget := maxBytes * 30 / 100
	gBudget := maxBytes - oBudget - rBudget
	if s.cfg.SessionEnabled && s.sessionID != "" {
		sBudget = maxBytes * 25 / 100
		gBudget = maxBytes - oBudget - sBudget - rBudget
	}
	includeGlobal := scope.global && s.cfg.GlobalEnabled
	if !includeGlobal {
		// Spend the unused global slice on recent agent.md entries rather than
		// shrinking the whole block — those are the facts this project earned.
		rBudget += gBudget
		gBudget = 0
	}

	var parts []string
	var stats []layerStat
	if chunk := s.sliceLayer(layerOrchestra, oBudget); chunk != "" {
		parts = append(parts, chunk)
		stats = append(stats, layerStat{layerOrchestra, len(chunk)})
	}
	if sBudget > 0 {
		if chunk := s.readSessionFile(sBudget); chunk != "" {
			parts = append(parts, "[session]\n"+chunk)
			stats = append(stats, layerStat{layerSession, len(chunk)})
		}
	}
	if chunk := s.sliceRepoMemory(rBudget, scope.repoFiles); chunk != "" {
		parts = append(parts, chunk)
		stats = append(stats, layerStat{layerRepo, len(chunk)})
	}
	if includeGlobal && gBudget > 0 {
		if chunk := s.sliceLayer(layerGlobal, gBudget); chunk != "" {
			parts = append(parts, chunk)
			stats = append(stats, layerStat{layerGlobal, len(chunk)})
		}
	}
	return strings.Join(parts, "\n\n---\n\n"), stats
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

// sliceRepoMemory returns recent agent.md entries and, when includeOtherFiles
// is set, the remaining .orchestra/memory/*.md files.
func (s *Store) sliceRepoMemory(maxBytes int, includeOtherFiles bool) string {
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
			// Ordered by type, then recency, so the budget is spent on the
			// entries most expensive to lose rather than simply the newest.
			// What does not fit becomes a one-line index rather than being
			// cut off: memory the model cannot see is memory it cannot ask
			// for either.
			recent := sliceEntriesWithIndex(entries, remaining)
			parts = append(parts, "[agent memory — feedback, user, project, reference; recent first within each]\n"+recent)
			remaining -= len(recent)
		}
	}

	if !includeOtherFiles || remaining <= 0 {
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
	content, _ := s.LazyOrchestraFile(dir)
	return content
}

// LazyOrchestraFile is LazyOrchestra plus the filename that actually
// supplied the content — "ORCHESTRA.md" unless a fallback (AGENTS.md,
// CLAUDE.md, .cursorrules) is what was found. Callers that label the text
// for the model (discoverInstructions) need the real name: claiming
// ORCHESTRA.md when the file is actually AGENTS.md points the model at a
// path that doesn't exist.
func (s *Store) LazyOrchestraFile(dir string) (content, name string) {
	if s.workspaceRoot == "" {
		return "", ""
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", ""
	}
	for {
		if raw, found := readOrchestraFile(absDir); raw != "" {
			cap := s.cfg.LazyBytes()
			return truncateToMax(raw, cap), found
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
	return "", ""
}
