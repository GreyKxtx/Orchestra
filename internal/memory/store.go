package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	layerOrchestra = "orchestra"
	layerSession   = "session"
	layerRepo      = "repo"
	layerGlobal    = "global"
)

// LayerSummary describes one memory source for memory_read listing.
type LayerSummary struct {
	Layer   string `json:"layer"`
	Path    string `json:"path"`
	Bytes   int    `json:"bytes"`
	Preview string `json:"preview,omitempty"`
}

// ReadResult is returned by memory_read.
type ReadResult struct {
	Layer   string `json:"layer,omitempty"`
	Path    string `json:"path,omitempty"`
	Content string `json:"content,omitempty"`
	Entries []LayerSummary `json:"entries,omitempty"`
	Truncated bool `json:"truncated,omitempty"`
}

// Store reads and writes layered project memory on disk.
type Store struct {
	workspaceRoot string
	sessionID     string
	cfg           Config
}

// NewStore creates a memory store for a workspace and optional session.
func NewStore(workspaceRoot, sessionID string, cfg Config) *Store {
	cfg.Normalize()
	return &Store{
		workspaceRoot: strings.TrimSpace(workspaceRoot),
		sessionID:     strings.TrimSpace(sessionID),
		cfg:           cfg,
	}
}

// FormatInject returns the <project_memory> block for system prompt injection.
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
			parts = append(parts, "[agent memory — recent first]\n"+recent)
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

func (s *Store) readLayerRaw(layer string) string {
	switch layer {
	case layerOrchestra:
		if s.workspaceRoot == "" {
			return ""
		}
		data, err := os.ReadFile(filepath.Join(s.workspaceRoot, "ORCHESTRA.md"))
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(data))
	case layerGlobal:
		if !s.cfg.GlobalEnabled {
			return ""
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		data, err := os.ReadFile(filepath.Join(home, ".orchestra", "memory.md"))
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(data))
	default:
		return ""
	}
}

func (s *Store) sessionFilePath() string {
	return filepath.Join(s.workspaceRoot, ".orchestra", "memory", "sessions", s.sessionID+".md")
}

func (s *Store) readSessionFile(maxBytes int) string {
	if !s.cfg.SessionEnabled || s.sessionID == "" {
		return ""
	}
	data, err := os.ReadFile(s.sessionFilePath())
	if err != nil {
		return ""
	}
	raw := strings.TrimSpace(string(data))
	if maxBytes > 0 && len(raw) > maxBytes {
		return tailBytes(raw, maxBytes)
	}
	return raw
}

// List returns summaries of all memory layers.
func (s *Store) List() []LayerSummary {
	var out []LayerSummary
	if raw := s.readLayerRaw(layerOrchestra); raw != "" {
		out = append(out, LayerSummary{
			Layer: layerOrchestra, Path: "ORCHESTRA.md", Bytes: len(raw),
			Preview: preview(raw, 120),
		})
	}
	if s.cfg.SessionEnabled && s.sessionID != "" {
		if raw := s.readSessionFile(0); raw != "" {
			out = append(out, LayerSummary{
				Layer: layerSession, Path: relPath(s.sessionFilePath(), s.workspaceRoot),
				Bytes: len(raw), Preview: preview(raw, 120),
			})
		}
	}
	memDir := filepath.Join(s.workspaceRoot, ".orchestra", "memory")
	if entries, err := os.ReadDir(memDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			f := filepath.Join(memDir, e.Name())
			data, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			raw := strings.TrimSpace(string(data))
			if raw == "" {
				continue
			}
			out = append(out, LayerSummary{
				Layer: layerRepo, Path: relPath(f, s.workspaceRoot),
				Bytes: len(raw), Preview: preview(raw, 120),
			})
		}
	}
	if s.cfg.GlobalEnabled {
		if raw := s.readLayerRaw(layerGlobal); raw != "" {
			out = append(out, LayerSummary{
				Layer: layerGlobal, Path: "~/.orchestra/memory.md",
				Bytes: len(raw), Preview: preview(raw, 120),
			})
		}
	}
	return out
}

// Read loads memory by layer or path. Empty layer+path lists all layers (metadata only).
func (s *Store) Read(layer, path string, maxBytes int) ReadResult {
	layer = strings.ToLower(strings.TrimSpace(layer))
	path = strings.TrimSpace(path)
	if maxBytes <= 0 {
		maxBytes = s.cfg.InjectBytes()
	}

	if layer == "" && path == "" {
		return ReadResult{Entries: s.List()}
	}

	if path != "" {
		content, resolvedLayer, err := s.readByPath(path, maxBytes)
		if err != nil {
			return ReadResult{Content: "error: " + err.Error()}
		}
		truncated := len(content) >= maxBytes
		return ReadResult{Layer: resolvedLayer, Path: path, Content: content, Truncated: truncated}
	}

	switch layer {
	case layerOrchestra, "project":
		raw := s.sliceLayer(layerOrchestra, maxBytes)
		return ReadResult{Layer: layerOrchestra, Path: "ORCHESTRA.md", Content: raw, Truncated: len(raw) >= maxBytes}
	case layerSession:
		if s.sessionID == "" {
			return ReadResult{Content: "no active session_id"}
		}
		raw := s.readSessionFile(maxBytes)
		return ReadResult{Layer: layerSession, Path: relPath(s.sessionFilePath(), s.workspaceRoot), Content: raw}
	case layerRepo:
		raw := s.sliceRepoMemory(maxBytes)
		return ReadResult{Layer: layerRepo, Path: ".orchestra/memory/", Content: raw, Truncated: len(raw) >= maxBytes}
	case layerGlobal:
		raw := s.sliceLayer(layerGlobal, maxBytes)
		return ReadResult{Layer: layerGlobal, Path: "~/.orchestra/memory.md", Content: raw}
	case "all":
		return ReadResult{Content: s.tieredInject(maxBytes), Truncated: true}
	default:
		return ReadResult{Content: fmt.Sprintf("unknown layer %q (want orchestra|session|repo|global|all)", layer)}
	}
}

func (s *Store) readByPath(path string, maxBytes int) (content, layer string, err error) {
	path = filepath.ToSlash(path)
	switch {
	case path == "ORCHESTRA.md":
		layer = layerOrchestra
		content = s.sliceLayer(layerOrchestra, maxBytes)
	case strings.HasPrefix(path, ".orchestra/memory/"):
		abs := filepath.Join(s.workspaceRoot, filepath.FromSlash(path))
		data, readErr := os.ReadFile(abs)
		if readErr != nil {
			return "", layerRepo, readErr
		}
		layer = layerRepo
		content = strings.TrimSpace(string(data))
		if len(content) > maxBytes {
			content = tailBytes(content, maxBytes)
		}
	case path == "~/.orchestra/memory.md":
		layer = layerGlobal
		content = s.sliceLayer(layerGlobal, maxBytes)
	default:
		return "", "", fmt.Errorf("path not in memory store: %s", path)
	}
	return content, layer, nil
}

// Append writes a timestamped entry. scope is "project" (agent.md) or "session".
func (s *Store) Append(scope, content string) (relPath string, written int, err error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", 0, fmt.Errorf("content must not be empty")
	}
	scope = strings.ToLower(strings.TrimSpace(scope))
	if scope == "" || scope == "project" {
		scope = "project"
	}

	var target string
	switch scope {
	case "session":
		if !s.cfg.SessionEnabled {
			return "", 0, fmt.Errorf("session memory is disabled in config")
		}
		if s.sessionID == "" {
			return "", 0, fmt.Errorf("no active session — use scope project or start a session")
		}
		dir := filepath.Join(s.workspaceRoot, ".orchestra", "memory", "sessions")
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", 0, err
		}
		target = s.sessionFilePath()
		relPath = filepath.ToSlash(filepath.Join(".orchestra", "memory", "sessions", s.sessionID+".md"))
	default:
		dir := filepath.Join(s.workspaceRoot, ".orchestra", "memory")
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", 0, err
		}
		target = filepath.Join(dir, "agent.md")
		relPath = ".orchestra/memory/agent.md"
	}

	entry := fmt.Sprintf("\n---\n*%s*\n\n%s\n", timestampUTC(), content)
	f, err := os.OpenFile(target, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	n, err := f.WriteString(entry)
	if err != nil {
		return "", 0, err
	}

	if scope == "project" {
		if compactErr := s.compactAgentFile(target); compactErr != nil {
			return relPath, n, compactErr
		}
	}
	return relPath, n, nil
}

func (s *Store) compactAgentFile(path string) error {
	maxBytes := s.cfg.MaxAgentBytes()
	if maxBytes <= 0 {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() <= int64(maxBytes) {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	entries := splitEntries(string(data))
	if len(entries) <= 1 {
		trimmed := tailBytes(string(data), maxBytes)
		return os.WriteFile(path, []byte(trimmed), 0644)
	}
	// Keep recent entries until under budget.
	var kept []string
	total := 0
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		add := len(e) + len(entrySep)
		if total+add > maxBytes && len(kept) > 0 {
			break
		}
		kept = append([]string{e}, kept...)
		total += add
	}
	body := strings.Join(kept, entrySep)
	if !strings.HasPrefix(body, "---") {
		body = entrySep + body
	}
	return os.WriteFile(path, []byte(body+"\n"), 0644)
}

// LazyOrchestra returns capped ORCHESTRA.md for fs.read lazy injection.
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

func preview(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func relPath(abs, root string) string {
	if rel, err := filepath.Rel(root, abs); err == nil {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(abs)
}

func timestampUTC() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05Z")
}
