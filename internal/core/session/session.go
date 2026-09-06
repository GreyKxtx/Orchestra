package session

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/patch/ops"
	"github.com/orchestra/orchestra/internal/sessionfile"
	"github.com/orchestra/orchestra/internal/tools"
)

// Session holds a persistent multi-turn conversation for one user.
type Session struct {
	ID           string
	History      []llm.Message
	CreatedAt    time.Time
	LastActivity time.Time

	mu         sync.Mutex
	cancelFn   context.CancelFunc // non-nil while a turn is running
	pendingOps []ops.AnyOp        // ops from last dry-run turn, cleared on apply or new turn
	todos      []tools.TodoItem   // model's working checklist, persisted across turns
	planPath   string             // per-session plan markdown path (relative)

	// UI projection (v2 unified session schema).
	title       string
	model       string
	uiMessages  []sessionfile.UIMessage
	profile     string
	applyOutput string
	costUSD     float64

	// turnStarts[k] is the index into History at which the (k+1)-th user
	// turn's agent output begins — the only link between the UI projection
	// and the LLM history, since the user's prompt is never appended to
	// History. Appended by SessionMessage when a turn *starts*, truncated by
	// rewind, cleared by compaction. See sessionfile.Snapshot.TurnStarts.
	turnStarts []int

	// Fork lineage, carried so a branch's own saves do not drop it.
	parentID        string
	forkedFromIndex int

	// lastSnapshotAt is the UpdatedAt of the snapshot this in-memory state
	// was last synced with (own Snapshot() write or LoadFromDisk). Used by
	// RefreshFromDiskIfNewer to detect writes from *other* core processes
	// (a detached background core finishing a turn) without ever mistaking
	// our own saves for external updates.
	lastSnapshotAt time.Time
}

// New creates a new session with a sortable TUI-compatible ID.
func New() *Session {
	return NewWithID(sessionfile.NewID())
}

// NewWithID creates a session with the given canonical id.
func NewWithID(id string) *Session {
	now := time.Now()
	return &Session{
		ID:           strings.TrimSpace(id),
		History:      make([]llm.Message, 0, 16),
		CreatedAt:    now,
		LastActivity: now,
		uiMessages:   make([]sessionfile.UIMessage, 0, 8),
	}
}

// Lock acquires the session mutex. Must be paired with Unlock.
func (s *Session) Lock() { s.mu.Lock() }

// Unlock releases the session mutex.
func (s *Session) Unlock() { s.mu.Unlock() }

// IsBusy reports whether a turn is currently running. Must be called with lock held.
func (s *Session) IsBusy() bool { return s.cancelFn != nil }

// SetCancel stores the cancel func for the running turn. Must be called with lock held.
func (s *Session) SetCancel(fn context.CancelFunc) { s.cancelFn = fn }

// ClearCancel removes the cancel func after a turn completes. Must be called with lock held.
func (s *Session) ClearCancel() { s.cancelFn = nil }

// Cancel cancels the currently running turn (no-op if idle).
// Keeps cancelFn non-nil until ClearCancel so IsBusy stays true while the
// turn is still unwinding (and still holding core.runMu). Clearing it here
// let a second session.message pass the busy check and deadlock on runMu.
func (s *Session) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancelFn != nil {
		s.cancelFn()
	}
}

// AppendHistory appends messages and updates LastActivity. Must be called with lock held.
func (s *Session) AppendHistory(msgs []llm.Message) {
	s.History = append(s.History, msgs...)
	s.LastActivity = time.Now()
}

// ReplaceHistory replaces the full LLM history (e.g. after compaction rewrite).
// Must be called with lock held. UIMessages are intentionally untouched.
func (s *Session) ReplaceHistory(msgs []llm.Message) {
	if msgs == nil {
		s.History = make([]llm.Message, 0, 16)
	} else {
		out := make([]llm.Message, len(msgs))
		copy(out, msgs)
		s.History = out
	}
	s.LastActivity = time.Now()
}

// CopyHistory returns a shallow copy of the current history. Must be called with lock held.
func (s *Session) CopyHistory() []llm.Message {
	out := make([]llm.Message, len(s.History))
	copy(out, s.History)
	return out
}

// SetPending stores ops from a dry-run turn for later apply. Overwrites any previous pending.
// Must be called with lock held.
func (s *Session) SetPending(pending []ops.AnyOp) {
	s.pendingOps = pending
}

// TakePending returns and clears the pending ops. Returns nil if none.
// Must be called with lock held.
func (s *Session) TakePending() []ops.AnyOp {
	out := s.pendingOps
	s.pendingOps = nil
	return out
}

// CopyPending returns a shallow copy of pending ops without clearing. Must be called with lock held.
func (s *Session) CopyPending() []ops.AnyOp {
	if len(s.pendingOps) == 0 {
		return nil
	}
	out := make([]ops.AnyOp, len(s.pendingOps))
	copy(out, s.pendingOps)
	return out
}

// HasPending reports whether there are pending ops. Must be called with lock held.
func (s *Session) HasPending() bool {
	return len(s.pendingOps) > 0
}

// CopyTodos returns a shallow copy of the current todo list. Must be called with lock held.
func (s *Session) CopyTodos() []tools.TodoItem {
	if len(s.todos) == 0 {
		return nil
	}
	out := make([]tools.TodoItem, len(s.todos))
	copy(out, s.todos)
	return out
}

// SetTodos replaces the todo list. Must be called with lock held.
func (s *Session) SetTodos(items []tools.TodoItem) {
	s.todos = items
}

// PlanPath returns the session plan file path (relative). Empty until first plan-mode turn.
func (s *Session) PlanPath() string {
	return s.planPath
}

// SetPlanPath stores the canonical plan markdown path for this session.
func (s *Session) SetPlanPath(path string) {
	s.planPath = strings.TrimSpace(path)
}

// Title returns the persisted chat title (UI projection).
func (s *Session) Title() string { return s.title }

// SetTitle updates the chat title. Must be called with lock held when mutating
// alongside other fields before Snapshot.
func (s *Session) SetTitle(title string) { s.title = strings.TrimSpace(title) }

// Model returns the last-used model name for this session.
func (s *Session) Model() string { return s.model }

// SetModel updates the model field.
func (s *Session) SetModel(model string) { s.model = strings.TrimSpace(model) }

// UIMessages returns a copy of the UI chat projection.
func (s *Session) UIMessages() []sessionfile.UIMessage {
	if len(s.uiMessages) == 0 {
		return nil
	}
	out := make([]sessionfile.UIMessage, len(s.uiMessages))
	copy(out, s.uiMessages)
	return out
}

// AppendUIMessage appends one UI projection message.
func (s *Session) AppendUIMessage(msg sessionfile.UIMessage) {
	s.uiMessages = append(s.uiMessages, msg)
}

// SetUIMessages replaces the UI chat projection.
func (s *Session) SetUIMessages(msgs []sessionfile.UIMessage) {
	if len(msgs) == 0 {
		s.uiMessages = nil
		return
	}
	s.uiMessages = append([]sessionfile.UIMessage(nil), msgs...)
}

// TurnStarts returns a copy of the recorded turn boundaries: entry k is the
// History index at which the (k+1)-th user turn's agent output begins.
// Must be called with lock held when read alongside History.
func (s *Session) TurnStarts() []int {
	if len(s.turnStarts) == 0 {
		return nil
	}
	out := make([]int, len(s.turnStarts))
	copy(out, s.turnStarts)
	return out
}

// AppendTurnStart records that a new user turn's output begins at index i.
// Called when a turn *starts*, not when it ends: mid-turn snapshots replace
// History with partial-turn content, so a turn-end computation would be wrong
// whenever one of those fired. Must be called with lock held.
func (s *Session) AppendTurnStart(i int) {
	s.turnStarts = append(s.turnStarts, i)
}

// SetTurnStarts replaces the recorded boundaries. Pass nil to clear them —
// which is what compaction must do, since it rewrites History wholesale and
// every recorded index points into the array that no longer exists.
// Must be called with lock held.
func (s *Session) SetTurnStarts(starts []int) {
	if len(starts) == 0 {
		s.turnStarts = nil
		return
	}
	s.turnStarts = append([]int(nil), starts...)
}

// Profile returns the last profile used in this session.
func (s *Session) Profile() string { return s.profile }

// SetProfile stores the profile name.
func (s *Session) SetProfile(p string) { s.profile = strings.TrimSpace(p) }

// ApplyOutput returns disk|patch preference for this session.
func (s *Session) ApplyOutput() string { return s.applyOutput }

// SetApplyOutput stores apply output mode.
func (s *Session) SetApplyOutput(v string) { s.applyOutput = strings.TrimSpace(v) }

// CostUSD returns accumulated session spend (paid providers).
func (s *Session) CostUSD() float64 { return s.costUSD }

// SetCostUSD replaces accumulated session spend.
func (s *Session) SetCostUSD(v float64) {
	if v < 0 {
		v = 0
	}
	s.costUSD = v
}
