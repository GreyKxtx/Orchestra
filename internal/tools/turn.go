package tools

import (
	"context"
	"fmt"
	"sync"

	"github.com/orchestra/orchestra/internal/memory"
	"github.com/orchestra/orchestra/internal/tools/fs"
)

// Turn is the state of one turn: its staging overlay, whether it previews
// or applies its edits, whether commands may run, and the session whose
// memory it writes. Tools reach it through the context the turn runs with
// (WithTurn); a call whose context carries none uses the runner's default
// turn — the CLI's direct path, tool.call, and tests that never start one.
//
// This used to be fields of the one Runner a core owns, so every turn of
// every session took one mutex for its whole length and two sessions could
// not run at once; a second session's turn also wiped the first one's staged
// edits (ARCH-5). A core now gives every session a turn of its own, and
// agent.run, workflow.run and skill.invoke each get a fresh one.
type Turn struct {
	r       *Runner
	overlay *fs.Overlay

	mu                     sync.RWMutex
	dryRun                 bool
	allowExecDespiteDryRun bool
	sessionID              string
	memoryCfg              memory.Config
	deptLessonWrites       int

	// shadowWS is where this turn's commands run while it previews (see
	// shadowWorkspace), made on the first command; shadowErr is why one
	// could not be made, remembered so the copy is not attempted again.
	shadowMu  sync.Mutex
	shadowWS  *shadowWorkspace
	shadowErr error
}

// TurnOptions says how a turn runs.
type TurnOptions struct {
	// DryRun stages write and edit in the turn's overlay instead of writing
	// to disk. Every core turn does; the CLI's `apply --apply` does not.
	DryRun bool
	// Apply: the turn commits its edits — the agent writes each staged edit
	// to disk once the tool returns, delete and rename act — and commands
	// may run even in a core that refuses them in a preview.
	Apply bool
	// SessionID names the session whose memory the turn's notes go to.
	SessionID string
	// Memory configures that memory. The zero value keeps the runner's.
	Memory memory.Config
}

type turnKey struct{}

// WithTurn attributes ctx to t: tools called with it stage, read and write
// in t's overlay, and t's flags gate them.
func WithTurn(ctx context.Context, t *Turn) context.Context {
	if t == nil {
		return ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return fs.WithOverlay(context.WithValue(ctx, turnKey{}, t), t.overlay)
}

// TurnFrom returns the turn ctx runs in, or nil.
func TurnFrom(ctx context.Context) *Turn {
	if ctx == nil {
		return nil
	}
	t, _ := ctx.Value(turnKey{}).(*Turn)
	return t
}

// NewTurn starts a turn of its own on r: a fresh overlay and its own flags.
// Close it when it is over.
func (r *Runner) NewTurn(opts TurnOptions) *Turn {
	overlay := fs.NewOverlay(r.workspaceRoot, fs.OverlayOptions{DryRun: opts.DryRun, ASTGate: r.astGate})
	t := newTurn(r, overlay, opts)
	r.turnsMu.Lock()
	r.turns[t] = struct{}{}
	r.turnsMu.Unlock()
	return t
}

func newTurn(r *Runner, overlay *fs.Overlay, opts TurnOptions) *Turn {
	cfg := opts.Memory
	if cfg == (memory.Config{}) {
		cfg = memory.DefaultConfig()
		if r != nil && r.turn != nil {
			cfg = r.turn.MemoryConfig()
		}
	}
	cfg.Normalize()
	overlay.SetCommitsToDisk(opts.Apply)
	return &Turn{
		r: r, overlay: overlay,
		dryRun: opts.DryRun, allowExecDespiteDryRun: opts.Apply,
		sessionID: opts.SessionID, memoryCfg: cfg,
	}
}

// TurnAt returns the turn the call behind ctx runs in: ctx's, else the
// runner's default turn.
func (r *Runner) TurnAt(ctx context.Context) *Turn {
	if t := TurnFrom(ctx); t != nil {
		return t
	}
	if r == nil {
		return nil
	}
	return r.turn
}

// liveTurns is the default turn and every open turn started with NewTurn.
func (r *Runner) liveTurns() []*Turn {
	if r == nil {
		return nil
	}
	r.turnsMu.Lock()
	defer r.turnsMu.Unlock()
	out := make([]*Turn, 0, len(r.turns)+1)
	if r.turn != nil {
		out = append(out, r.turn)
	}
	for t := range r.turns {
		out = append(out, t)
	}
	return out
}

// Begin readies a session's turn for one message: the previous message's
// staged edits are dropped (they live on as the session's pending ops), the
// apply flag and the memory context are set for this one. End puts the
// flags back once the message is answered.
func (t *Turn) Begin(apply bool, sessionID string, cfg memory.Config) {
	if t == nil {
		return
	}
	t.ClearStaged()
	cfg.Normalize()
	t.mu.Lock()
	t.allowExecDespiteDryRun = apply
	t.sessionID = sessionID
	t.memoryCfg = cfg
	t.deptLessonWrites = 0
	t.mu.Unlock()
	t.overlay.SetCommitsToDisk(apply)
}

// End is the counterpart of Begin: the turn previews again until the next
// message says otherwise. The staged edits stay for session.apply_pending.
func (t *Turn) End() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.allowExecDespiteDryRun = false
	t.mu.Unlock()
	t.overlay.SetCommitsToDisk(false)
}

// Close ends a turn started with NewTurn: its staged edits are dropped and
// their documents closed in the language servers.
func (t *Turn) Close() {
	if t == nil || t.r == nil {
		return
	}
	t.r.turnsMu.Lock()
	delete(t.r.turns, t)
	t.r.turnsMu.Unlock()
	t.ClearStaged()
	t.closeShadow()
}

// Overlay is the turn's staging overlay.
func (t *Turn) Overlay() *fs.Overlay {
	if t == nil {
		return nil
	}
	return t.overlay
}

// DryRun reports whether the turn stages its edits.
func (t *Turn) DryRun() bool {
	if t == nil {
		return false
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.dryRun
}

// SetDryRun switches staging on or off; off drops the staged edits.
func (t *Turn) SetDryRun(v bool) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.dryRun = v
	t.mu.Unlock()
	t.overlay.SetDryRun(v)
}

// SetAllowExecDespiteDryRun lets commands run while file tools stay in the
// staging overlay.
func (t *Turn) SetAllowExecDespiteDryRun(v bool) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.allowExecDespiteDryRun = v
	t.mu.Unlock()
}

// SetCommitsToDisk records whether the turn applies its changes, which is
// how delete and rename tell a real run from a preview.
func (t *Turn) SetCommitsToDisk(v bool) {
	if t == nil {
		return
	}
	t.overlay.SetCommitsToDisk(v)
}

// ExecRefused reports whether every command is refused in this turn: a
// preview on a runner that blocks commands there (the core), not unlocked by
// apply, with no shadow to run them in.
func (t *Turn) ExecRefused() bool {
	if t == nil || t.r == nil {
		return false
	}
	return t.previewBlocksCommands() && !t.r.shadowExec
}

// CommandsInShadow reports whether this turn's commands run in a shadow of
// the workspace: a preview the runner would otherwise refuse them in.
func (t *Turn) CommandsInShadow() bool {
	if t == nil || t.r == nil {
		return false
	}
	return t.previewBlocksCommands() && t.r.shadowExec
}

func (t *Turn) previewBlocksCommands() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.dryRun && t.r.blockExecInDryRun && !t.allowExecDespiteDryRun
}

// shadow is the turn's shadow workspace, made on first use. An error is
// remembered: a workspace too large to copy stays so for the turn.
func (t *Turn) shadow() (*shadowWorkspace, error) {
	t.shadowMu.Lock()
	defer t.shadowMu.Unlock()
	if t.shadowWS != nil || t.shadowErr != nil {
		return t.shadowWS, t.shadowErr
	}
	t.shadowWS, t.shadowErr = newShadowWorkspace(t.r.workspaceRoot, t.r.excludeDirs)
	return t.shadowWS, t.shadowErr
}

func (t *Turn) closeShadow() {
	if t == nil {
		return
	}
	t.shadowMu.Lock()
	ws := t.shadowWS
	t.shadowWS, t.shadowErr = nil, nil
	t.shadowMu.Unlock()
	ws.Close()
}

// SessionID is the session whose memory the turn writes.
func (t *Turn) SessionID() string {
	if t == nil {
		return ""
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.sessionID
}

// MemoryConfig is the memory configuration the turn's notes follow.
func (t *Turn) MemoryConfig() memory.Config {
	if t == nil {
		return memory.DefaultConfig()
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.memoryCfg
}

// SetMemoryContext names the session and the memory configuration.
func (t *Turn) SetMemoryContext(sessionID string, cfg memory.Config) {
	if t == nil {
		return
	}
	cfg.Normalize()
	t.mu.Lock()
	t.sessionID = sessionID
	t.memoryCfg = cfg
	t.mu.Unlock()
}

// ListStagedPaths is every path staged in the turn's overlay.
func (t *Turn) ListStagedPaths() []string {
	if t == nil || t.overlay == nil {
		return nil
	}
	return t.overlay.ListStagedPaths()
}

// HasStagedChanges reports whether anything is staged.
func (t *Turn) HasStagedChanges() bool {
	if t == nil || t.overlay == nil {
		return false
	}
	return t.overlay.HasStagedChanges()
}

// EffectiveContent is the staged content of relPath, when there is one.
func (t *Turn) EffectiveContent(relPath string) (string, bool) {
	if t == nil || t.overlay == nil {
		return "", false
	}
	return t.overlay.EffectiveContent(relPath)
}

// UnstagePath removes one path from the overlay after it was committed.
func (t *Turn) UnstagePath(relSlash string) {
	if t == nil || t.overlay == nil || relSlash == "" {
		return
	}
	t.overlay.UnstagePath(relSlash)
}

// StagedSnapshot is the turn's staged files, for a checkpoint.
func (t *Turn) StagedSnapshot() []fs.StagedSnapshot {
	if t == nil || t.overlay == nil {
		return nil
	}
	return t.overlay.SnapshotStaged()
}

// RestoreStaged stages a checkpoint's files and shows them to the language
// servers, as staging them in the run did.
func (t *Turn) RestoreStaged(files []fs.StagedSnapshot) {
	if t == nil || t.overlay == nil {
		return
	}
	t.overlay.RestoreStaged(files)
	if t.r == nil || t.r.lspManager == nil || t.r.lspManager.IsEmpty() {
		return
	}
	for _, f := range files {
		_ = t.r.lspManager.SyncStaged(context.Background(), f.Path, f.Content)
	}
}

// ClearStaged drops the turn's staged edits and closes their documents in
// the language servers: they were opened with the staged content, and a
// server that kept them would answer from a draft that no longer exists
// (DATA-8). The file is what the disk says again.
func (t *Turn) ClearStaged() {
	if t == nil || t.overlay == nil {
		return
	}
	paths := t.overlay.ListStagedPaths()
	t.overlay.ClearStaged()
	if len(paths) == 0 || t.r == nil || t.r.lspManager == nil || t.r.lspManager.IsEmpty() {
		return
	}
	ctx := context.Background()
	for _, p := range paths {
		t.r.lspManager.DidClose(ctx, p)
	}
}

const maxDeptLessonWritesPerRun = 3

// resetDeptLessonBudget clears the per-run cap on memory_write dept scopes.
func (t *Turn) resetDeptLessonBudget() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.deptLessonWrites = 0
	t.mu.Unlock()
}

func (t *Turn) consumeDeptLessonWrite() error {
	if t == nil {
		return fmt.Errorf("turn is nil")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.deptLessonWrites >= maxDeptLessonWritesPerRun {
		return fmt.Errorf("dept lesson budget exhausted (max %d memory_write calls with dept scope per agent run)", maxDeptLessonWritesPerRun)
	}
	t.deptLessonWrites++
	return nil
}
