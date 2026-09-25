package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/orchestra/orchestra/internal/sessionfile"
	"github.com/orchestra/orchestra/patch/fsutil"
)

const (
	layerOrchestra = "orchestra"
	layerSession   = "session"
	layerRepo      = "repo"
	layerLessons   = "lessons"
	layerGlobal    = "global"
	// layerUserOrchestra is the user's own standing instructions,
	// ~/.orchestra/ORCHESTRA.md — "how I want you to work", as opposed to
	// layerGlobal's ~/.orchestra/memory.md, which is "what you remembered".
	// Both used to be the same file, so the agent's own compaction could
	// rewrite a human's standing instructions.
	layerUserOrchestra = "orchestra-user"
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
		sessionID:     validSessionID(sessionID),
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

// AppendTyped stores one entry with an explicit type. It is AppendEntry for
// callers that do not care whether the write updated an existing fact.
func (s *Store) AppendTyped(scope, entryType, content string) (relPath string, written int, err error) {
	res, err := s.AppendEntry(scope, entryType, content)
	return res.Path, res.Written, err
}

// AppendResult describes what one write did.
type AppendResult struct {
	Path    string
	Written int
	Type    string
	// Replaced is true when the note restated a fact already in memory and
	// updated it in place instead of adding a second copy.
	Replaced bool
}

// AppendEntry stores one entry with an explicit type, which decides where it
// sits when memory is sliced into a prompt (see entrytype.go), and reports
// whether it updated an existing fact rather than adding one.
func (s *Store) AppendEntry(scope, entryType, content string) (AppendResult, error) {
	return s.AppendEntryFrom(scope, entryType, content, "")
}

// AppendEntryFrom is AppendEntry with the entry's provenance (see
// formatEntryFrom): who wrote it, recorded on its header line.
//
// The whole write — reading what is there, replacing a restated fact or
// appending, compacting — happens under the target file's lock, and every
// rewrite is atomic. Parallel subagents and two cores on one project (the VS
// Code extension and the TUI) used to interleave read-modify-write cycles:
// with 60 writers, 8 to 29 of 30 new facts survived, and a compaction racing
// an append could leave agent.md truncated (DATA-1).
func (s *Store) AppendEntryFrom(scope, entryType, content, source string) (AppendResult, error) {
	content = sanitizeMemoryText(strings.TrimSpace(content))
	if content == "" {
		return AppendResult{}, fmt.Errorf("content must not be empty")
	}
	scope = strings.ToLower(strings.TrimSpace(scope))
	if scope == "" || scope == "project" {
		scope = "project"
	}

	var target, rel string
	switch scope {
	case "session":
		if !s.cfg.SessionEnabled {
			return AppendResult{}, fmt.Errorf("session memory is disabled in config")
		}
		if s.sessionID == "" {
			return AppendResult{}, fmt.Errorf("no active session — use scope project or start a session")
		}
		dir := filepath.Join(s.workspaceRoot, ".orchestra", "memory", "sessions")
		if err := os.MkdirAll(dir, 0755); err != nil {
			return AppendResult{}, err
		}
		target = s.sessionFilePath()
		rel = filepath.ToSlash(filepath.Join(".orchestra", "memory", "sessions", s.sessionID+".md"))
	case "global":
		if !s.cfg.GlobalEnabled {
			return AppendResult{}, fmt.Errorf("global memory is disabled in config")
		}
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return AppendResult{}, fmt.Errorf("resolve home directory: %w", homeErr)
		}
		dir := filepath.Join(home, ".orchestra")
		if err := os.MkdirAll(dir, 0755); err != nil {
			return AppendResult{}, err
		}
		target = filepath.Join(dir, "memory.md")
		rel = "~/.orchestra/memory.md"
	default:
		dir := filepath.Join(s.workspaceRoot, ".orchestra", "memory")
		if err := os.MkdirAll(dir, 0755); err != nil {
			return AppendResult{}, err
		}
		target = filepath.Join(dir, "agent.md")
		rel = ".orchestra/memory/agent.md"
	}

	unlock, err := fsutil.LockFile(target + ".lock")
	if err != nil {
		return AppendResult{}, err
	}
	defer unlock()

	entry := formatEntryFrom(timestampUTC(), entryType, content, source)

	// A fact restated is an update, not a second fact. Only project memory
	// deduplicates: session memory is the running log of one conversation,
	// where collapsing repetition would erase that something recurred, and
	// global memory is small and hand-curated.
	if scope == "project" {
		replaced, n, err := s.replaceNearDuplicate(target, content, entry)
		if err != nil {
			return AppendResult{}, err
		}
		if replaced {
			return AppendResult{Path: rel, Written: n, Type: NormalizeEntryType(entryType), Replaced: true}, nil
		}
	}

	f, err := os.OpenFile(target, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return AppendResult{}, err
	}
	defer f.Close()
	n, err := f.WriteString(entry)
	if err != nil {
		return AppendResult{}, err
	}

	res := AppendResult{Path: rel, Written: n, Type: NormalizeEntryType(entryType)}
	if scope == "project" {
		if compactErr := s.compactAgentFile(target); compactErr != nil {
			return res, compactErr
		}
	}
	return res, nil
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

// validSessionID drops an id that cannot name a file under
// .orchestra/memory/sessions/: a session memory at "<id>.md" must not land
// elsewhere. Such a store simply has no session layer.
func validSessionID(id string) string {
	id = strings.TrimSpace(id)
	if !sessionfile.ValidID(id) {
		return ""
	}
	return id
}
