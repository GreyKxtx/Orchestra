package fs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/orchestra/orchestra/internal/ckg"
	"github.com/orchestra/orchestra/patch/cache"
	"github.com/orchestra/orchestra/patch/ops"
	"github.com/orchestra/orchestra/patch/patches"
	"github.com/orchestra/orchestra/patch/resolver"
	"github.com/orchestra/orchestra/protocol"
)

// OverlayOptions configures a staging overlay.
type OverlayOptions struct {
	DryRun  bool
	ASTGate bool
}

// Overlay holds in-memory staged file state for dry-run mode.
type Overlay struct {
	root    string
	DryRun  bool
	ASTGate bool
	// commitsToDisk marks a run that stages only as a means to an end: write
	// and edit accumulate here and the agent commits each one to disk right
	// after the tool returns. Deletes and renames have nothing to stage — the
	// strict op format has no op for either — so they need to know whether the
	// run they are part of will land on disk at all.
	commitsToDisk bool

	mu     sync.RWMutex
	staged map[string]*stagedFile

	// A task layer (layer.go) has the overlay it forked from as parent, and
	// base holds, per file it wrote, the parent's hash at the first write:
	// what its change was made against. Nil on a turn's overlay.
	parent *Overlay
	base   map[string]string
	state  layerState
}

type stagedFile struct {
	content  string
	hash     string
	diskHash string
	isNew    bool
}

// NewOverlay creates a staging overlay for workspace root.
func NewOverlay(root string, opts OverlayOptions) *Overlay {
	return &Overlay{
		root:    root,
		DryRun:  opts.DryRun,
		ASTGate: opts.ASTGate,
		staged:  make(map[string]*stagedFile),
	}
}

// SetDryRun enables or disables staging mode. Disabling clears all staged state.
func (o *Overlay) SetDryRun(v bool) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.DryRun = v
	if !v {
		o.staged = make(map[string]*stagedFile)
	}
}

// SetCommitsToDisk records whether this run applies its changes. It is the
// same condition the agent uses to commit each staged write, and the only way
// delete and rename can tell a real run from a preview.
func (o *Overlay) SetCommitsToDisk(v bool) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.commitsToDisk = v
}

// CommitsToDisk reports whether this run applies its changes.
func (o *Overlay) CommitsToDisk() bool {
	if o == nil {
		return false
	}
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.commitsToDisk
}

// SetASTGate toggles tree-sitter syntax validation before staging.
func (o *Overlay) SetASTGate(v bool) {
	if o == nil {
		return
	}
	o.mu.Lock()
	o.ASTGate = v
	o.mu.Unlock()
}

func (o *Overlay) stageFile(c *Client, relSlash, content, hash string) error {
	if o == nil {
		return nil
	}
	if o.ASTGate {
		if err := ckg.ValidateSyntax(relSlash, []byte(content)); err != nil {
			return err
		}
	}
	o.mu.Lock()
	o.stageFileLocked(relSlash, content, hash)
	o.mu.Unlock()
	if c != nil && c.Hooks.OnStageSync != nil {
		c.Hooks.OnStageSync(relSlash, content)
	}
	return nil
}

func (o *Overlay) stageFileLocked(relSlash, content, hash string) {
	if sf, ok := o.staged[relSlash]; ok {
		sf.content = content
		sf.hash = hash
		return
	}
	if o.parent != nil {
		if _, ok := o.base[relSlash]; !ok {
			o.base[relSlash] = o.parent.currentHash(relSlash)
		}
	}
	absPath := filepath.Join(o.root, filepath.FromSlash(relSlash))
	diskBytes, err := os.ReadFile(absPath)
	isNew := os.IsNotExist(err)
	diskHash := ""
	if err == nil {
		diskHash = cache.ComputeSHA256(diskBytes)
	}
	o.staged[relSlash] = &stagedFile{
		content:  content,
		hash:     hash,
		diskHash: diskHash,
		isNew:    isNew,
	}
}

func (o *Overlay) fileExistsOnDisk(relSlash string) bool {
	absPath := filepath.Join(o.root, filepath.FromSlash(relSlash))
	_, err := os.Stat(absPath)
	return err == nil
}

func (o *Overlay) mergeStagedFilesIntoList(files []FSFileMeta, listPath string, includeHash bool, limit int) []FSFileMeta {
	if o == nil || !o.DryRun {
		return files
	}
	staged := o.effectiveStaged()
	if len(staged) == 0 {
		return files
	}

	prefix := filepath.ToSlash(strings.TrimSpace(listPath))
	if prefix == "" || prefix == "." {
		prefix = ""
	} else {
		prefix = strings.TrimSuffix(prefix, "/") + "/"
	}

	byPath := make(map[string]int, len(files))
	for i, f := range files {
		byPath[f.Path] = i
	}

	for path, sf := range staged {
		if prefix != "" && !strings.HasPrefix(path, prefix) {
			continue
		}
		if idx, ok := byPath[path]; ok {
			files[idx].Size = int64(len(sf.content))
			if includeHash {
				files[idx].FileHash = sf.hash
			}
			continue
		}
		meta := FSFileMeta{Path: path, Size: int64(len(sf.content))}
		if includeHash {
			meta.FileHash = sf.hash
		}
		files = append(files, meta)
		if limit > 0 && len(files) >= limit {
			break
		}
	}
	return files
}

func (o *Overlay) mergeStagedFilesIntoGlob(files []FSFileMeta, pattern string, includeHash bool, limit int) []FSFileMeta {
	if o == nil || !o.DryRun {
		return files
	}
	staged := o.effectiveStaged()
	if len(staged) == 0 {
		return files
	}

	seen := make(map[string]bool, len(files))
	for _, f := range files {
		seen[f.Path] = true
	}

	for path, sf := range staged {
		if seen[path] || !matchGlobPath(pattern, path) {
			continue
		}
		meta := FSFileMeta{Path: path, Size: int64(len(sf.content))}
		if includeHash {
			meta.FileHash = sf.hash
		}
		files = append(files, meta)
		if limit > 0 && len(files) >= limit {
			break
		}
	}
	return files
}

func (o *Overlay) stagedContent(relSlash string) (content, hash string, ok bool) {
	if o == nil {
		return "", "", false
	}
	o.mu.RLock()
	sf, ok := o.staged[relSlash]
	parent := o.parent
	if ok {
		content, hash = sf.content, sf.hash
	}
	o.mu.RUnlock()
	if ok {
		return content, hash, true
	}
	// A task layer sees what its owner sees, live.
	return parent.stagedContent(relSlash)
}

// EffectiveContent returns staged content for relPath when present.
func (o *Overlay) EffectiveContent(relPath string) (string, bool) {
	relPath = filepath.ToSlash(relPath)
	content, _, ok := o.stagedContent(relPath)
	return content, ok
}

// ListStagedPaths returns sorted forward-slash paths in the overlay.
func (o *Overlay) ListStagedPaths() []string {
	staged := o.effectiveStaged()
	if len(staged) == 0 {
		return nil
	}
	out := make([]string, 0, len(staged))
	for p := range staged {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func (o *Overlay) currentHash(relSlash string) string {
	if _, hash, ok := o.stagedContent(relSlash); ok {
		return hash
	}
	absPath := filepath.Join(o.root, filepath.FromSlash(relSlash))
	b, err := os.ReadFile(absPath)
	if err != nil {
		return ""
	}
	return cache.ComputeSHA256(b)
}

// currentContent returns what relSlash holds right now — staged content in a
// dry run, otherwise what is on disk. ok is false when the file does not
// exist either way.
func (o *Overlay) currentContent(_ *Client, relSlash string) (string, bool) {
	if content, _, ok := o.stagedContent(relSlash); ok {
		return content, true
	}
	b, err := os.ReadFile(filepath.Join(o.root, filepath.FromSlash(relSlash)))
	if err != nil {
		return "", false
	}
	return string(b), true
}

func (o *Overlay) StagedContent(relSlash string) (content, hash string, ok bool) {
	return o.stagedContent(relSlash)
}

// CurrentHash returns sha256 of relSlash from overlay (if staged) or disk.
func (o *Overlay) CurrentHash(relSlash string) string {
	return o.currentHash(relSlash)
}

// StagedOps returns write_atomic ops for all staged files.
func (o *Overlay) StagedOps() []ops.AnyOp {
	if o == nil {
		return nil
	}
	o.mu.RLock()
	defer o.mu.RUnlock()
	if len(o.staged) == 0 {
		return nil
	}
	out := make([]ops.AnyOp, 0, len(o.staged))
	for path, sf := range o.staged {
		wa := ops.WriteAtomicOp{
			Op:      ops.OpFileWriteAtomic,
			Path:    path,
			Content: sf.content,
			Conditions: ops.WriteAtomicConditions{
				MustNotExist: sf.isNew,
				FileHash:     sf.diskHash,
			},
		}
		waCopy := wa
		out = append(out, ops.AnyOp{Op: waCopy.Op, Path: waCopy.Path, WriteAtomic: &waCopy})
	}
	return out
}

func (o *Overlay) stagedOpForPath(relSlash string) (ops.AnyOp, bool) {
	if o == nil {
		return ops.AnyOp{}, false
	}
	o.mu.RLock()
	defer o.mu.RUnlock()
	sf, ok := o.staged[relSlash]
	if !ok {
		return ops.AnyOp{}, false
	}
	wa := ops.WriteAtomicOp{
		Op:      ops.OpFileWriteAtomic,
		Path:    relSlash,
		Content: sf.content,
		Conditions: ops.WriteAtomicConditions{
			MustNotExist: sf.isNew,
			FileHash:     sf.diskHash,
		},
	}
	return ops.AnyOp{Op: wa.Op, Path: wa.Path, WriteAtomic: &wa}, true
}

func (o *Overlay) unstagePath(relSlash string) {
	if o == nil {
		return
	}
	o.mu.Lock()
	delete(o.staged, relSlash)
	o.mu.Unlock()
}

// UnstagePath removes relSlash from the overlay (exported for post-apply cleanup).
func (o *Overlay) UnstagePath(relSlash string) {
	o.unstagePath(relSlash)
}

// StagedFileContent returns path→content snapshot of the overlay — for a task
// layer, everything it sees staged: its owners' files with its own on top.
func (o *Overlay) StagedFileContent() map[string]string {
	staged := o.effectiveStaged()
	if len(staged) == 0 {
		return nil
	}
	out := make(map[string]string, len(staged))
	for path, sf := range staged {
		out[path] = sf.content
	}
	return out
}

// ApplyPatchesToStaged applies external patches to the overlay.
func (o *Overlay) ApplyPatchesToStaged(c *Client, patchList []patches.Patch) error {
	if o == nil {
		return fmt.Errorf("overlay is nil")
	}
	for _, p := range patchList {
		relSlash := filepath.ToSlash(strings.TrimSpace(p.Path))
		if relSlash == "" {
			return protocol.NewError(protocol.InvalidLLMOutput, "patch path is empty", nil)
		}

		var currentContent []byte
		if content, _, ok := o.stagedContent(relSlash); ok {
			currentContent = []byte(content)
		} else {
			absPath := filepath.Join(o.root, filepath.FromSlash(relSlash))
			b, err := os.ReadFile(absPath)
			if err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("read %s: %w", relSlash, err)
			}
			currentContent = b
		}

		if p.FileHash != "" {
			current := o.currentHash(relSlash)
			if current != "" && current != p.FileHash {
				return protocol.NewError(protocol.StaleContent, "file hash mismatch", map[string]any{
					"path":     relSlash,
					"expected": p.FileHash,
					"actual":   current,
				})
			}
		}

		var newContent []byte
		var err error
		switch p.Type {
		case patches.TypeFileSearchReplace:
			newContent, err = resolver.ApplySearchReplace(currentContent, p.Search, p.Replace)
		case patches.TypeFileUnifiedDiff:
			newContent, err = resolver.ApplyUnifiedDiff(currentContent, p.Diff)
		case patches.TypeFileWriteAtomic:
			if reason := writeAtomicPatchRefusal(p, string(currentContent)); reason != "" {
				return protocol.NewError(protocol.InvalidLLMOutput, reason, map[string]any{"path": relSlash})
			}
			if p.Conditions != nil {
				if p.Conditions.MustNotExist && o.fileExistsOnDisk(relSlash) {
					return protocol.NewError(protocol.AlreadyExists, "file already exists", map[string]any{"path": relSlash})
				}
				if p.Conditions.FileHash != "" {
					current := o.currentHash(relSlash)
					if current != "" && current != p.Conditions.FileHash {
						return protocol.NewError(protocol.StaleContent, "file hash mismatch in write_atomic conditions", map[string]any{
							"path":     relSlash,
							"expected": p.Conditions.FileHash,
							"actual":   current,
						})
					}
				}
			}
			newContent = []byte(p.Content)
		default:
			return protocol.NewError(protocol.InvalidLLMOutput, "unsupported patch type", map[string]any{"type": p.Type})
		}
		if err != nil {
			return err
		}

		newHash := cache.ComputeSHA256(newContent)
		if err := o.stageFile(c, relSlash, string(newContent), newHash); err != nil {
			return err
		}
	}
	return nil
}

// ClearStaged removes all staged changes.
func (o *Overlay) ClearStaged() {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.staged = make(map[string]*stagedFile)
}

// HasStagedChanges reports whether any files are staged.
func (o *Overlay) HasStagedChanges() bool {
	if o == nil {
		return false
	}
	o.mu.RLock()
	defer o.mu.RUnlock()
	return len(o.staged) > 0
}

// CommitStagedPath writes one staged file to disk and removes it from the overlay.
func (c *Client) CommitStagedPath(ctx context.Context, path string, backup bool) (*FSApplyOpsResponse, error) {
	c = c.at(ctx)
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "client is nil", nil)
	}
	if c.Overlay == nil || !c.isDryRun() {
		return nil, nil
	}
	_, relSlash, err := resolveWorkspacePath(c.Root, path)
	if err != nil {
		return nil, err
	}
	op, ok := c.Overlay.stagedOpForPath(relSlash)
	if !ok {
		return nil, nil
	}
	resp, err := c.ApplyOps(ctx, FSApplyOpsRequest{
		Ops:    []ops.AnyOp{op},
		DryRun: false,
		Backup: backup,
	})
	if err != nil {
		return nil, err
	}
	c.Overlay.unstagePath(relSlash)
	return resp, nil
}

// writeAtomicPatchRefusal states why a whole-file patch must not be applied,
// or "" when it is safe.
//
// The staging path used to apply write_atomic as `newContent = p.Content` with
// no check at all, while the resolver path (resolveWriteAtomic) required a
// safety condition and ran the destructive-write guard. A model that declared
// file.write_atomic and filled in the search/replace fields therefore handed
// over an EMPTY Content, and the final apply wrote that empty string over the
// file — after the edit/write tools had already put the right content on disk.
//
// That is the eval task two_files, failing about one run in four:
//
//	{"path":"util.go","type":"file.write_atomic",
//	 "search":"","replace":"package main\n\nfunc Double(n int) int {…"}
//
//	disk_commit util.go        55   ← the tool wrote it correctly
//	disk_commit final:util.go   0   ← the final apply emptied it
//
// Both files ended at zero bytes and the run reported success. The model was
// wrong to send that patch; nothing should have carried the mistake through to
// erasing a file.
func writeAtomicPatchRefusal(p patches.Patch, current string) string {
	// A patch whose declared type disagrees with the fields it carries. Naming
	// the contradiction is what lets the model fix it; "empty content" alone
	// would send it looking in the wrong place.
	if p.Content == "" && (p.Search != "" || p.Replace != "") {
		return "patch declares type file.write_atomic but carries search/replace and no content — " +
			"use type file.search_replace for a partial edit, or put the complete new file in content"
	}
	if reason := resolver.DestructiveWriteReason(current, p.Content); reason != "" {
		return "refusing to write " + p.Path + ": " + reason
	}
	return ""
}

// StagedSnapshot is one staged file as a checkpoint keeps it: the content,
// and the disk version it was made against, which the final apply checks.
type StagedSnapshot struct {
	Path     string
	Content  string
	DiskHash string
	IsNew    bool
}

// SnapshotStaged returns the overlay's own staged files, sorted by path — not
// its tasks' layers: an uncommitted layer dies with its task.
func (o *Overlay) SnapshotStaged() []StagedSnapshot {
	if o == nil {
		return nil
	}
	o.mu.RLock()
	defer o.mu.RUnlock()
	out := make([]StagedSnapshot, 0, len(o.staged))
	for p, sf := range o.staged {
		out = append(out, StagedSnapshot{Path: p, Content: sf.content, DiskHash: sf.diskHash, IsNew: sf.isNew})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// RestoreStaged stages files as a checkpoint recorded them. Each keeps the
// disk version it was made against: a file changed on disk since then fails
// the final apply as stale, as it would have in the run that staged it.
func (o *Overlay) RestoreStaged(files []StagedSnapshot) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, f := range files {
		o.staged[f.Path] = &stagedFile{
			content:  f.Content,
			hash:     cache.ComputeSHA256([]byte(f.Content)),
			diskHash: f.DiskHash,
			isNew:    f.IsNew,
		}
	}
}
