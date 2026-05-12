// Package sessionstore persists TUI chat sessions to .orchestra/sessions/*.json
// so they can be reopened across launches. Each record is one session with
// metadata and the full message list.
package sessionstore

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/orchestra/orchestra/ui/tui/state"
)

// SessionMeta describes a saved session without its full message list. Used
// for listing past sessions in the picker.
type SessionMeta struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Model     string    `json:"model,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	MsgCount  int       `json:"msg_count"`
}

// SessionRecord is the full saved form of a session.
type SessionRecord struct {
	SessionMeta
	Messages []state.Message `json:"messages"`
}

// NewID returns a sortable, unique session id like "20260510T143055-7f3a".
func NewID() string {
	ts := time.Now().UTC().Format("20060102T150405")
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Fall back to nanoseconds if the random source is unavailable.
		return fmt.Sprintf("%s-%04x", ts, time.Now().UnixNano()&0xffff)
	}
	return ts + "-" + hex.EncodeToString(b[:])
}

// TitleFromMessages returns a short title derived from the first user
// message, truncated to ~60 visible chars. Falls back to "(empty session)".
func TitleFromMessages(msgs []state.Message) string {
	for _, m := range msgs {
		if m.Role != state.RoleUser {
			continue
		}
		t := strings.TrimSpace(m.Text)
		if t == "" {
			continue
		}
		// First line, normalized whitespace.
		if i := strings.IndexByte(t, '\n'); i >= 0 {
			t = t[:i]
		}
		const limit = 60
		if len([]rune(t)) > limit {
			r := []rune(t)
			t = string(r[:limit-1]) + "…"
		}
		return t
	}
	return "(empty session)"
}

func sessionsDir(workspaceRoot string) string {
	return filepath.Join(workspaceRoot, ".orchestra", "sessions")
}

// Save writes rec atomically to <root>/.orchestra/sessions/<id>.json.
// Updates rec.UpdatedAt to now and ensures CreatedAt is set.
func Save(workspaceRoot string, rec *SessionRecord) error {
	if rec.ID == "" {
		return fmt.Errorf("sessionstore: empty id")
	}
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now().UTC()
	}
	rec.UpdatedAt = time.Now().UTC()
	rec.MsgCount = len(rec.Messages)

	dir := sessionsDir(workspaceRoot)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("sessionstore: mkdir: %w", err)
	}

	path := filepath.Join(dir, rec.ID+".json")
	tmpPath := path + ".tmp"

	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("sessionstore: marshal: %w", err)
	}
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return fmt.Errorf("sessionstore: write: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("sessionstore: rename: %w", err)
	}
	return nil
}

// Load reads a session record by id from disk.
func Load(workspaceRoot, id string) (*SessionRecord, error) {
	if id == "" {
		return nil, fmt.Errorf("sessionstore: empty id")
	}
	path := filepath.Join(sessionsDir(workspaceRoot), id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rec SessionRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("sessionstore: parse %s: %w", id, err)
	}
	return &rec, nil
}

// List returns metadata for every session in <root>/.orchestra/sessions/,
// sorted by UpdatedAt descending. Empty list is returned if the directory
// doesn't exist or contains no sessions.
func List(workspaceRoot string) ([]SessionMeta, error) {
	dir := sessionsDir(workspaceRoot)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]SessionMeta, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var meta SessionMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			continue
		}
		if meta.ID == "" {
			meta.ID = strings.TrimSuffix(e.Name(), ".json")
		}
		out = append(out, meta)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}

// Delete removes a session file. Missing file is not an error.
func Delete(workspaceRoot, id string) error {
	path := filepath.Join(sessionsDir(workspaceRoot), id+".json")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
