package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/orchestra/orchestra/patch/cache"
	"github.com/orchestra/orchestra/internal/ckg"
	"github.com/orchestra/orchestra/patch/ops"
	"github.com/orchestra/orchestra/patch/patches"
	"github.com/orchestra/orchestra/protocol"
	"github.com/orchestra/orchestra/patch/resolver"
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
// Returns SyntaxError when AST gate rejects content (see Runner.astGate).
func (r *Runner) stageFile(relSlash, content, hash string) error {
	if r.astGate {
		if err := ckg.ValidateSyntax(relSlash, []byte(content)); err != nil {
			return err
		}
	}
	r.stagedMu.Lock()
	r.stageFileLocked(relSlash, content, hash)
	r.stagedMu.Unlock()
	if r.lspManager != nil && !r.lspManager.IsEmpty() {
		if err := r.lspManager.SyncStaged(context.Background(), relSlash, content); err != nil {
			fmt.Fprintf(os.Stderr, "tools: staging LSP sync %s: %v\n", relSlash, err)
		}
	}
	return nil
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

// fileExistsOnDisk reports whether relSlash exists on disk (ignoring staging overlay).
func (r *Runner) fileExistsOnDisk(relSlash string) bool {
	absPath := filepath.Join(r.workspaceRoot, filepath.FromSlash(relSlash))
	_, err := os.Stat(absPath)
	return err == nil
}

// mergeStagedFilesIntoList adds staged-only files to an ls result and refreshes
// size/hash for staged paths that also appear on disk. In dry-run the model
// must see files it created via write/edit even before /apply.
func (r *Runner) mergeStagedFilesIntoList(files []FSFileMeta, listPath string, includeHash bool, limit int) []FSFileMeta {
	if !r.dryRun {
		return files
	}
	r.stagedMu.RLock()
	defer r.stagedMu.RUnlock()
	if len(r.staged) == 0 {
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

	for path, sf := range r.staged {
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

// mergeStagedFilesIntoGlob adds staged paths that match pattern but are absent
// from the disk walk result.
func (r *Runner) mergeStagedFilesIntoGlob(files []FSFileMeta, pattern string, includeHash bool, limit int) []FSFileMeta {
	if !r.dryRun {
		return files
	}
	r.stagedMu.RLock()
	defer r.stagedMu.RUnlock()
	if len(r.staged) == 0 {
		return files
	}

	seen := make(map[string]bool, len(files))
	for _, f := range files {
		seen[f.Path] = true
	}

	for path, sf := range r.staged {
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

// stagedContent returns staged content and hash for relSlash, or ok=false if not staged.
func (r *Runner) stagedContent(relSlash string) (content, hash string, ok bool) {
	r.stagedMu.RLock()
	defer r.stagedMu.RUnlock()
	sf, ok := r.staged[relSlash]
	if !ok {
		return "", "", false
	}
	return sf.content, sf.hash, true
}

// EffectiveContent implements lsp.ContentProvider for dry-run staging overlay.
func (r *Runner) EffectiveContent(relPath string) (string, bool) {
	relPath = filepath.ToSlash(relPath)
	content, _, ok := r.stagedContent(relPath)
	return content, ok
}

// ListStagedPaths returns sorted forward-slash paths currently in the staging overlay.
func (r *Runner) ListStagedPaths() []string {
	r.stagedMu.RLock()
	defer r.stagedMu.RUnlock()
	if len(r.staged) == 0 {
		return nil
	}
	out := make([]string, 0, len(r.staged))
	for p := range r.staged {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
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
	r.stagedMu.RLock()
	defer r.stagedMu.RUnlock()
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
		waCopy := wa
		out = append(out, ops.AnyOp{Op: waCopy.Op, Path: waCopy.Path, WriteAtomic: &waCopy})
	}
	return out
}

// stagedOpForPath returns the write_atomic op for one staged path.
func (r *Runner) stagedOpForPath(relSlash string) (ops.AnyOp, bool) {
	r.stagedMu.RLock()
	defer r.stagedMu.RUnlock()
	sf, ok := r.staged[relSlash]
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

// unstagePath removes relSlash from the overlay after a successful disk commit.
func (r *Runner) unstagePath(relSlash string) {
	r.stagedMu.Lock()
	delete(r.staged, relSlash)
	r.stagedMu.Unlock()
}

// CommitStagedPath writes one staged file to disk and removes it from the overlay.
// No-op when path is not staged or dry-run is off (already written directly).
func (r *Runner) CommitStagedPath(ctx context.Context, path string, backup bool) (*FSApplyOpsResponse, error) {
	if r == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "runner is nil", nil)
	}
	if !r.dryRun {
		return nil, nil
	}
	_, relSlash, err := resolveWorkspacePath(r.workspaceRoot, path)
	if err != nil {
		return nil, err
	}
	op, ok := r.stagedOpForPath(relSlash)
	if !ok {
		return nil, nil
	}
	resp, err := r.FSApplyOps(ctx, FSApplyOpsRequest{
		Ops:    []ops.AnyOp{op},
		DryRun: false,
		Backup: backup,
	})
	if err != nil {
		return nil, err
	}
	r.unstagePath(relSlash)
	return resp, nil
}

// StagedFileContent returns a snapshot of the staging overlay as a path→content map.
// Used by the agent to pass overlay to patch resolvers.
func (r *Runner) StagedFileContent() map[string]string {
	r.stagedMu.RLock()
	defer r.stagedMu.RUnlock()
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

		// Validate file_hash before applying (belt-and-suspenders: search/replace
		// and unified_diff already fail on content mismatch; write_atomic does not).
		if p.FileHash != "" {
			current := r.currentHash(relSlash)
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
			if p.Conditions != nil {
				if p.Conditions.MustNotExist && r.fileExistsOnDisk(relSlash) {
					return protocol.NewError(protocol.AlreadyExists, "file already exists", map[string]any{"path": relSlash})
				}
				if p.Conditions.FileHash != "" {
					current := r.currentHash(relSlash)
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
		if err := r.stageFile(relSlash, string(newContent), newHash); err != nil {
			return err
		}
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
	r.stagedMu.RLock()
	defer r.stagedMu.RUnlock()
	return len(r.staged) > 0
}
