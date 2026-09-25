package fs

import (
	"context"
	"errors"
	"fmt"
	iofs "io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/orchestra/orchestra/internal/wsview"
	"github.com/orchestra/orchestra/patch/fsutil"
)

// Task layers.
//
// Every agent of a turn used to stage into the one overlay: a worker that
// failed, was cancelled or timed out left its edits staged, and the turn's
// final apply wrote them (ORC-1). A worker's checks built everything staged,
// so it failed on a sibling's half-done file or passed because a sibling
// fixed it (ORC-12).
//
// A layer is a task's own overlay over its owner's. Reads fall through it to
// the owner's view and then the disk, live; writes stay in the layer. Commit
// merges the layer into the owner when the task succeeded, and Discard drops
// it when it did not. Commit refuses a file the owner's view changed since
// the layer first wrote it — another task committed there in between — so two
// workers on one file are a conflict, not a silent overwrite.

type layerState int

const (
	layerOpen layerState = iota
	layerMerged
	layerDiscarded
)

// ErrLayerDiscarded is Commit on a layer whose owner was discarded: the task
// that would have received the change failed, so the change goes with it.
var ErrLayerDiscarded = errors.New("the owning task's changes were discarded")

// MergeConflict is Commit refusing files the owner changed since the layer
// first wrote them.
type MergeConflict struct {
	Paths []string
}

func (e *MergeConflict) Error() string {
	return fmt.Sprintf("merge conflict: %s changed since this task wrote it (another task committed a change there first)",
		strings.Join(e.Paths, ", "))
}

// Fork returns a layer over o. A layer that has been merged has nothing of its
// own left, so a fork of it is a fork of the overlay it merged into. Nil when
// o is nil or not staging: outside a dry run writes go to disk and there is
// nothing to layer.
func (o *Overlay) Fork() *Overlay {
	for o != nil && o.stateOf() == layerMerged {
		o = o.parent
	}
	if o == nil {
		return nil
	}
	o.mu.RLock()
	defer o.mu.RUnlock()
	if !o.DryRun {
		return nil
	}
	return &Overlay{
		root:          o.root,
		DryRun:        o.DryRun,
		ASTGate:       o.ASTGate,
		commitsToDisk: o.commitsToDisk,
		staged:        make(map[string]*stagedFile),
		parent:        o,
		base:          make(map[string]string),
	}
}

// Owner is the overlay o's changes go to when it commits: its parent, or the
// first one up that has not itself merged. Nil for a turn's overlay.
func (o *Overlay) Owner() *Overlay {
	if o == nil {
		return nil
	}
	p := o.parent
	for p != nil && p.stateOf() == layerMerged {
		p = p.parent
	}
	return p
}

// IsLayer reports whether o is a task layer rather than a turn's overlay.
func (o *Overlay) IsLayer() bool { return o != nil && o.parent != nil }

func (o *Overlay) stateOf() layerState {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.state
}

// Commit merges the layer into its owner and returns the paths it merged.
// Nothing is merged on a conflict: the layer is discarded and the error names
// the files.
func (o *Overlay) Commit() ([]string, error) {
	if o == nil || o.parent == nil {
		return nil, nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	switch o.state {
	case layerMerged:
		return nil, nil
	case layerDiscarded:
		return nil, ErrLayerDiscarded
	}
	parent := o.parent
	for parent != nil && parent.stateOf() == layerMerged {
		parent = parent.parent
	}
	if parent == nil || parent.stateOf() == layerDiscarded {
		o.discardLocked()
		return nil, ErrLayerDiscarded
	}
	parent.mu.Lock()
	defer parent.mu.Unlock()

	var conflicts []string
	for p, sf := range o.staged {
		current := parent.currentHashLocked(p)
		if current != o.base[p] && current != sf.hash {
			conflicts = append(conflicts, p)
		}
	}
	if len(conflicts) > 0 {
		sort.Strings(conflicts)
		o.discardLocked()
		return nil, &MergeConflict{Paths: conflicts}
	}
	paths := make([]string, 0, len(o.staged))
	for p, sf := range o.staged {
		parent.mergeLocked(p, sf, o.base[p])
		paths = append(paths, p)
	}
	sort.Strings(paths)
	o.staged = make(map[string]*stagedFile)
	o.state = layerMerged
	return paths, nil
}

// Discard drops the layer's changes and returns the paths it dropped. A
// no-op on a layer already merged.
func (o *Overlay) Discard() []string {
	if o == nil || o.parent == nil {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.state != layerOpen {
		return nil
	}
	paths := make([]string, 0, len(o.staged))
	for p := range o.staged {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	o.discardLocked()
	return paths
}

// CurrentContent is what relSlash holds as o sees it: staged in o or an owner,
// else on disk. ok is false when the file exists in neither.
func (o *Overlay) CurrentContent(relSlash string) (string, bool) {
	return o.currentContent(nil, relSlash)
}

// ReadFile is relSlash as o sees it, for the runtime's checks (wsview.View):
// staged in o or an owner, else on disk.
func (o *Overlay) ReadFile(relSlash string) ([]byte, error) {
	c, ok := o.currentContent(nil, wsview.Clean(relSlash))
	if !ok {
		return nil, fmt.Errorf("%s: %w", relSlash, iofs.ErrNotExist)
	}
	return []byte(c), nil
}

func (o *Overlay) discardLocked() {
	o.staged = make(map[string]*stagedFile)
	o.state = layerDiscarded
}

// currentHashLocked is currentHash for a caller holding o.mu: o's own staged
// version, else its owner's view, else the disk.
func (o *Overlay) currentHashLocked(relSlash string) string {
	if sf, ok := o.staged[relSlash]; ok {
		return sf.hash
	}
	if o.parent != nil {
		return o.parent.currentHash(relSlash)
	}
	return diskHash(o.root, relSlash)
}

// mergeLocked takes a committed child's version of a file. The child's base is
// what the change was made against, and becomes this layer's base when it had
// not written the file itself — so its own commit is checked against the same
// version further up.
func (o *Overlay) mergeLocked(relSlash string, child *stagedFile, childBase string) {
	if sf, ok := o.staged[relSlash]; ok {
		sf.content = child.content
		sf.hash = child.hash
		return
	}
	o.staged[relSlash] = &stagedFile{
		content:  child.content,
		hash:     child.hash,
		diskHash: child.diskHash,
		isNew:    child.isNew,
	}
	if o.parent != nil {
		if _, ok := o.base[relSlash]; !ok {
			o.base[relSlash] = childBase
		}
	}
}

// effectiveStaged is every staged file this overlay can see: its owners'
// first, its own on top.
func (o *Overlay) effectiveStaged() map[string]*stagedFile {
	if o == nil {
		return nil
	}
	o.mu.RLock()
	parent := o.parent
	own := make(map[string]*stagedFile, len(o.staged))
	for p, sf := range o.staged {
		own[p] = sf
	}
	o.mu.RUnlock()
	if parent == nil {
		return own
	}
	out := parent.effectiveStaged()
	if out == nil {
		out = make(map[string]*stagedFile, len(own))
	}
	for p, sf := range own {
		out[p] = sf
	}
	return out
}

func diskHash(root, relSlash string) string {
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relSlash)))
	if err != nil {
		return ""
	}
	return fsutil.ComputeSHA256(b)
}

type overlayKey struct{}

// WithOverlay attributes ctx to a task's layer: the filesystem tools called
// with it read and write there.
func WithOverlay(ctx context.Context, o *Overlay) context.Context {
	if o == nil {
		return ctx
	}
	return context.WithValue(ctx, overlayKey{}, o)
}

// OverlayFrom returns the layer ctx was attributed to, or nil.
func OverlayFrom(ctx context.Context) *Overlay {
	if ctx == nil {
		return nil
	}
	o, _ := ctx.Value(overlayKey{}).(*Overlay)
	return o
}

// at returns the client as the task behind ctx sees the workspace: its layer
// in place of the turn's overlay. The client is a few plain fields, so the
// copy shares everything else.
func (c *Client) at(ctx context.Context) *Client {
	if c == nil {
		return nil
	}
	o := OverlayFrom(ctx)
	if o == nil || o == c.Overlay {
		return c
	}
	cp := *c
	cp.Overlay = o
	return &cp
}
