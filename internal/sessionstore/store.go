package sessionstore

import (
	"fmt"
	"os"

	"github.com/orchestra/orchestra/internal/sessionfile"
	"github.com/orchestra/orchestra/internal/uimodel"
)

// SessionMeta describes a saved session without its full message list.
type SessionMeta = sessionfile.Meta

// SessionRecord is the client view of a saved session (UI messages only).
type SessionRecord struct {
	SessionMeta
	Messages []uimodel.Message `json:"messages"`
}

// NewID returns a sortable session id (delegates to sessionfile).
func NewID() string { return sessionfile.NewID() }

// TitleFromMessages returns a short title from chat messages.
func TitleFromMessages(msgs []uimodel.Message) string {
	return sessionfile.TitleFromUIMessages(uimodel.ToSessionfile(msgs))
}

// Save writes a v2 snapshot via sessionfile (legacy direct TUI path).
// Prefer core session.ui_sync when RPC is available.
func Save(workspaceRoot string, rec *SessionRecord) error {
	if rec == nil || rec.ID == "" {
		return fmt.Errorf("sessionstore: empty record/id")
	}
	snap, err := sessionfile.Load(workspaceRoot, rec.ID)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if snap == nil {
		snap = &sessionfile.Snapshot{ID: rec.ID}
	}
	snap.Title = rec.Title
	snap.Model = rec.Model
	if !rec.CreatedAt.IsZero() {
		snap.CreatedAt = rec.CreatedAt
	}
	snap.UIMessages = uimodel.ToSessionfile(rec.Messages)
	return sessionfile.Save(workspaceRoot, snap)
}

// Load reads a session and returns UI messages (agent history via core session.start).
func Load(workspaceRoot, id string) (*SessionRecord, error) {
	snap, err := sessionfile.Load(workspaceRoot, id)
	if err != nil {
		return nil, err
	}
	msgs := uimodel.FromSessionfile(snap.UIMessages)
	return &SessionRecord{
		SessionMeta: SessionMeta{
			ID:        snap.ID,
			Title:     snap.Title,
			Model:     snap.Model,
			CreatedAt: snap.CreatedAt,
			UpdatedAt: snap.UpdatedAt,
			MsgCount:  len(msgs),
		},
		Messages: msgs,
	}, nil
}

// List returns session metadata sorted by UpdatedAt desc.
func List(workspaceRoot string) ([]SessionMeta, error) {
	return sessionfile.ListMeta(workspaceRoot)
}

// Delete removes a session file.
func Delete(workspaceRoot, id string) error {
	return sessionfile.Delete(workspaceRoot, id)
}

// StateMessagesToUI converts chat messages to sessionfile projection.
// Deprecated: use uimodel.ToSessionfile.
func StateMessagesToUI(msgs []uimodel.Message) []sessionfile.UIMessage {
	return uimodel.ToSessionfile(msgs)
}

// UIMessagesToState converts sessionfile projection back to chat messages.
// Deprecated: use uimodel.FromSessionfile.
func UIMessagesToState(msgs []sessionfile.UIMessage) []uimodel.Message {
	return uimodel.FromSessionfile(msgs)
}
