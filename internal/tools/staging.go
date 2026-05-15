package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/orchestra/orchestra/internal/cache"
	"github.com/orchestra/orchestra/internal/ops"
	"github.com/orchestra/orchestra/internal/patches"
	"github.com/orchestra/orchestra/internal/protocol"
	"github.com/orchestra/orchestra/internal/resolver"
)

// stagedFile holds the in-memory state of a file written or edited during a dry-run pass.
type stagedFile struct {
	content  string // current staged content
	hash     string // sha256 of content
	diskHash string // sha256 of file on disk at first-stage time (empty if file was new)
	isNew    bool   // true if file didn't exist on disk when first staged
}

// stageFile records new staged content for relSlash (forward-slash relative path).
// On first call for a given path, captures original disk state for plan.json conditions.
// Subsequent calls update content but keep original disk conditions.
func (r *Runner) stageFile(relSlash, content, hash string) {
	r.stagedMu.Lock()
	defer r.stagedMu.Unlock()
	r.stageFileLocked(relSlash, content, hash)
}

func (r *Runner) stageFileLocked(relSlash, content, hash string) {
	if sf, ok := r.staged[relSlash]; ok {
		sf.content = content
		sf.hash = hash
		return
	}
	absPath := filepath.Join(r.workspaceRoot, filepath.FromSlash(relSlash))
	diskBytes, err := os.ReadFile(absPath)
	isNew := os.IsNotExist(err)
	diskHash := ""
	if err == nil {
		diskHash = cache.ComputeSHA256(diskBytes)
	}
	r.staged[relSlash] = &stagedFile{
		content:  content,
		hash:     hash,
		diskHash: diskHash,
		isNew:    isNew,
	}
}

// stagedContent returns staged content and hash for relSlash, or ok=false if not staged.
func (r *Runner) stagedContent(relSlash string) (content, hash string, ok bool) {
	r.stagedMu.Lock()
	defer r.stagedMu.Unlock()
	sf, ok := r.staged[relSlash]
	if !ok {
		return "", "", false
	}
	return sf.content, sf.hash, true
}

// currentHash returns sha256 of relSlash from overlay (if staged) or disk.
// Returns empty string if the file doesn't exist anywhere.
func (r *Runner) currentHash(relSlash string) string {
	if _, hash, ok := r.stagedContent(relSlash); ok {
		return hash
	}
	absPath := filepath.Join(r.workspaceRoot, filepath.FromSlash(relSlash))
	b, err := os.ReadFile(absPath)
	if err != nil {
		return ""
	}
	return cache.ComputeSHA256(b)
}

// StagedOps returns write_atomic ops for all staged files.
// Each op carries original disk conditions (must_not_exist or file_hash)
// so --from-plan replay detects stale files correctly.
func (r *Runner) StagedOps() []ops.AnyOp {
	r.stagedMu.Lock()
	defer r.stagedMu.Unlock()
	if len(r.staged) == 0 {
		return nil
	}
	out := make([]ops.AnyOp, 0, len(r.staged))
	for path, sf := range r.staged {
		wa := ops.WriteAtomicOp{
			Op:      ops.OpFileWriteAtomic,
			Path:    path,
			Content: sf.content,
			Conditions: ops.WriteAtomicConditions{
				MustNotExist: sf.isNew,
				FileHash:     sf.diskHash,
			},
		}
		if sf.isNew {
			wa.Conditions.FileHash = ""
		}
		waCopy := wa
		out = append(out, ops.AnyOp{Op: waCopy.Op, Path: waCopy.Path, WriteAtomic: &waCopy})
	}
	return out
}

// StagedFileContent returns a snapshot of the staging overlay as a path→content map.
// Used by the agent to pass overlay to patch resolvers.
func (r *Runner) StagedFileContent() map[string]string {
	r.stagedMu.Lock()
	defer r.stagedMu.Unlock()
	if len(r.staged) == 0 {
		return nil
	}
	out := make(map[string]string, len(r.staged))
	for path, sf := range r.staged {
		out[path] = sf.content
	}
	return out
}

// ApplyPatchesToStaged applies external patches to the staging overlay.
// For each patch, reads from overlay (if staged) or disk, applies the patch,
// stores result in overlay. Called in dry-run mode after model returns final.patches.
func (r *Runner) ApplyPatchesToStaged(patchList []patches.Patch) error {
	for _, p := range patchList {
		relSlash := filepath.ToSlash(strings.TrimSpace(p.Path))
		if relSlash == "" {
			return protocol.NewError(protocol.InvalidLLMOutput, "patch path is empty", nil)
		}

		// Get current content: staged or disk.
		var currentContent []byte
		if content, _, ok := r.stagedContent(relSlash); ok {
			currentContent = []byte(content)
		} else {
			absPath := filepath.Join(r.workspaceRoot, filepath.FromSlash(relSlash))
			b, err := os.ReadFile(absPath)
			if err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("read %s: %w", relSlash, err)
			}
			currentContent = b
		}

		var newContent []byte
		var err error
		switch p.Type {
		case patches.TypeFileSearchReplace:
			newContent, err = resolver.ApplySearchReplace(currentContent, p.Search, p.Replace)
		case patches.TypeFileUnifiedDiff:
			newContent, err = resolver.ApplyUnifiedDiff(currentContent, p.Diff)
		case patches.TypeFileWriteAtomic:
			newContent = []byte(p.Content)
		default:
			return protocol.NewError(protocol.InvalidLLMOutput, "unsupported patch type", map[string]any{"type": p.Type})
		}
		if err != nil {
			return err
		}

		newHash := cache.ComputeSHA256(newContent)
		r.stageFile(relSlash, string(newContent), newHash)
	}
	return nil
}

// ClearStaged removes all staged changes. Called before each agent.run in core mode.
func (r *Runner) ClearStaged() {
	r.stagedMu.Lock()
	defer r.stagedMu.Unlock()
	r.staged = make(map[string]*stagedFile)
}

// HasStagedChanges reports whether any files have been staged.
func (r *Runner) HasStagedChanges() bool {
	r.stagedMu.Lock()
	defer r.stagedMu.Unlock()
	return len(r.staged) > 0
}
