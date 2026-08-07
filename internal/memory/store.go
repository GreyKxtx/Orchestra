package memory

import (
	"fmt"
	"os"
	"path/filepath"
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
	Layer     string         `json:"layer,omitempty"`
	Path      string         `json:"path,omitempty"`
	Content   string         `json:"content,omitempty"`
	Entries   []LayerSummary `json:"entries,omitempty"`
	Truncated bool           `json:"truncated,omitempty"`
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
			return "", 0, fmt.Errorf("no active session вЂ” use scope project or start a session")
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
