package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	layerOrchestra = "orchestra"
	layerSession   = "session"
	layerRepo      = "repo"
	layerLessons   = "lessons"
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

// Append stores a project fact. It is AppendTyped with the default type,
// kept so the many existing callers do not all have to say "project".
func (s *Store) Append(scope, content string) (relPath string, written int, err error) {
	return s.AppendTyped(scope, TypeProject, content)
}

// AppendTyped stores one entry with an explicit type, which decides where it
// sits when memory is sliced into a prompt (see entrytype.go).
func (s *Store) AppendTyped(scope, entryType, content string) (relPath string, written int, err error) {
	content = sanitizeMemoryText(strings.TrimSpace(content))
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
	case "global":
		if !s.cfg.GlobalEnabled {
			return "", 0, fmt.Errorf("global memory is disabled in config")
		}
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return "", 0, fmt.Errorf("resolve home directory: %w", homeErr)
		}
		dir := filepath.Join(home, ".orchestra")
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", 0, err
		}
		target = filepath.Join(dir, "memory.md")
		relPath = "~/.orchestra/memory.md"
	default:
		dir := filepath.Join(s.workspaceRoot, ".orchestra", "memory")
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", 0, err
		}
		target = filepath.Join(dir, "agent.md")
		relPath = ".orchestra/memory/agent.md"
	}

	entry := formatEntry(timestampUTC(), entryType, content)
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

// sanitizeMemoryText replaces invalid UTF-8 before anything is persisted.
// Tool output on Windows can arrive in the console codepage (a cp1251 `ls`
// error is how this got in), and memory is re-injected into every later
// prompt — so one bad byte is re-sent on every step of every session that
// follows, not just the one that wrote it.
func sanitizeMemoryText(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	return strings.ToValidUTF8(s, "�")
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
