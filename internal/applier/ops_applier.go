package applier

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/orchestra/orchestra/internal/cache"
	"github.com/orchestra/orchestra/internal/ops"
	"github.com/orchestra/orchestra/internal/protocol"
	"github.com/orchestra/orchestra/internal/relpath"
)

// applyMu serialises all in-process apply runs so concurrent ApplyAnyOps
// calls on the same project can't clobber each other's `.orchestra.bak`
// backups (a second writer's atomic rename would overwrite the first
// writer's backup, permanently losing the pre-edit version of files in
// both runs). Cross-process races are still possible; H9 in the audit
// ledger notes that a real fix would need a per-project file lock.
var applyMu sync.Mutex

// filePlan is the per-file planning record built up during ApplyAnyOps:
// the canonical relative path, its absolute path (after symlink/junction
// validation), the original content (`before`), the post-apply content
// (`after`), whether the file already exists on disk and its permission
// mode. Hoisted out of ApplyAnyOps so writeBackupsParallel and other
// helpers can refer to the same shape without re-declaring it.
type filePlan struct {
	rel    string
	abs    string
	exists bool
	perm   os.FileMode
	before []byte
	after  []byte
}

// pathResolver caches absolute-path lookups for a single ApplyAnyOps run
// and centralises the workspace-rel canonicalisation. The cache is keyed
// on the canonical relative path so the same op kind doesn't pay
// safeAbsPath's symlink-resolution cost twice. Not safe for concurrent
// use — one resolver per ApplyAnyOps invocation.
type pathResolver struct {
	rootAbs  string
	rootReal string
	absByRel map[string]string
}

func newPathResolver(rootAbs, rootReal string) *pathResolver {
	return &pathResolver{
		rootAbs:  rootAbs,
		rootReal: rootReal,
		absByRel: make(map[string]string),
	}
}

// getAbs returns the validated absolute path for rel, memoising the
// safeAbsPath result.
func (r *pathResolver) getAbs(rel string) (string, error) {
	if v, ok := r.absByRel[rel]; ok {
		return v, nil
	}
	abs, err := safeAbsPath(r.rootAbs, r.rootReal, rel)
	if err != nil {
		return "", err
	}
	r.absByRel[rel] = abs
	return abs, nil
}

// canonRel canonicalises a user/LLM-supplied path into the workspace-
// relative slash form used as map keys and op identifiers. Empty / "."
// inputs are rejected; ".." escapes are NOT rejected here — that check
// lives in safeAbsPath, which compares the resolved absolute path against
// rootReal. Splitting the check this way lets a rel path like
// `foo/../bar.go` resolve to `bar.go` (cleaned) without rejection.
func (r *pathResolver) canonRel(p string) (string, error) {
	rp := filepath.ToSlash(strings.TrimSpace(p))
	if rp == "" {
		return "", protocol.NewError(protocol.InvalidLLMOutput, "path is empty", nil)
	}
	rp = filepath.Clean(filepath.FromSlash(rp))
	rp = filepath.ToSlash(rp)
	if rp == "." {
		return "", protocol.NewError(protocol.InvalidLLMOutput, "path is invalid", map[string]any{"path": p})
	}
	return rp, nil
}

// ApplyOps applies Internal Ops v1 (compat wrapper for file.replace_range).
//
// Safety properties (per spec):
// - Path traversal is rejected.
// - Each op checks `expected` strictly at `range` OR uses fuzzy fallback if enabled.
// - `conditions.file_hash` participates in stale detection (used to guard against applying to changed files).
func ApplyOps(root string, in []ops.ReplaceRangeOp, opts ApplyOptions) (*ApplyResult, error) {
	return ApplyAnyOps(root, ops.WrapReplaceRangeOps(in), opts)
}

// ApplyAnyOps applies a mixed set of ops (replace_range, write_atomic, mkdir_all).
//
// Policy: all-or-nothing for validation (no writes on error). If validation succeeds
// and opts.DryRun=false, writes are applied in deterministic path order.
//
// Concurrency safety (H9 in audit ledger):
//   - applyMu serialises in-process callers so concurrent apply runs in the
//     same Orchestra process don't clobber each other's `.orchestra.bak`.
//   - acquireProjectLock takes an exclusive POSIX flock / Windows LockFileEx
//     on `<project>/.orchestra/apply.lock` so two SEPARATE Orchestra
//     processes (e.g. CLI + TUI core, two TUIs, CI parallel jobs) wait
//     on each other instead of racing the same files.
func ApplyAnyOps(root string, in []ops.AnyOp, opts ApplyOptions) (*ApplyResult, error) {
	applyMu.Lock()
	defer applyMu.Unlock()
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("root is empty")
	}
	if !opts.DryRun {
		// Only acquire the cross-process lock when we're actually going to
		// mutate disk. Dry-run callers (preview, plan) should never block.
		release, err := acquireProjectLock(root)
		if err != nil {
			return nil, fmt.Errorf("acquire project apply lock: %w", err)
		}
		defer release()
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("abs root: %w", err)
	}
	rootReal := rootAbs
	if rp, err := filepath.EvalSymlinks(rootAbs); err == nil {
		rootReal = rp
	}
	resolver := newPathResolver(rootAbs, rootReal)

	// Collect ops by kind/path (using canonical slash paths for stable ordering).
	replaceByPath := make(map[string][]ops.ReplaceRangeOp)
	writeByPath := make(map[string]ops.WriteAtomicOp)
	mkdirByPath := make(map[string]ops.MkdirAllOp)

	for _, anyOp := range in {
		opName := strings.TrimSpace(anyOp.Op)
		switch opName {
		case ops.OpFileReplaceRange:
			if anyOp.ReplaceRange == nil {
				return nil, protocol.NewError(protocol.InvalidLLMOutput, "missing replace_range payload", map[string]any{"op": opName})
			}
			rr := *anyOp.ReplaceRange
			if rr.Op == "" {
				rr.Op = ops.OpFileReplaceRange
			}
			rel, err := resolver.canonRel(rr.Path)
			if err != nil {
				return nil, err
			}
			rr.Path = rel
			if _, err := resolver.getAbs(rel); err != nil {
				return nil, err
			}
			replaceByPath[rel] = append(replaceByPath[rel], rr)

		case ops.OpFileWriteAtomic:
			if anyOp.WriteAtomic == nil {
				return nil, protocol.NewError(protocol.InvalidLLMOutput, "missing write_atomic payload", map[string]any{"op": opName})
			}
			wa := *anyOp.WriteAtomic
			if wa.Op == "" {
				wa.Op = ops.OpFileWriteAtomic
			}
			rel, err := resolver.canonRel(wa.Path)
			if err != nil {
				return nil, err
			}
			wa.Path = rel
			if _, ok := replaceByPath[rel]; ok {
				return nil, protocol.NewError(protocol.InvalidLLMOutput, "conflicting ops for same path", map[string]any{"path": rel})
			}
			if _, exists := writeByPath[rel]; exists {
				return nil, protocol.NewError(protocol.InvalidLLMOutput, "duplicate write_atomic for path", map[string]any{"path": rel})
			}
			if _, err := resolver.getAbs(rel); err != nil {
				return nil, err
			}
			writeByPath[rel] = wa

		case ops.OpFileMkdirAll:
			if anyOp.MkdirAll == nil {
				return nil, protocol.NewError(protocol.InvalidLLMOutput, "missing mkdir_all payload", map[string]any{"op": opName})
			}
			md := *anyOp.MkdirAll
			if md.Op == "" {
				md.Op = ops.OpFileMkdirAll
			}
			rel, err := resolver.canonRel(md.Path)
			if err != nil {
				return nil, err
			}
			md.Path = rel
			if _, err := resolver.getAbs(rel); err != nil {
				return nil, err
			}
			// M16 in audit ledger: dedupe by canonical rel path BUT reject
			// conflicting Mode values for the same path. Previously last-
			// write-wins silently picked one Mode, hiding patch bugs that
			// emit the same directory with different perms.
			if existing, ok := mkdirByPath[rel]; ok && existing.Mode != 0 && md.Mode != 0 && existing.Mode != md.Mode {
				return nil, protocol.NewError(protocol.InvalidLLMOutput,
					"conflicting mkdir_all mode for same path",
					map[string]any{"path": rel, "mode_a": existing.Mode, "mode_b": md.Mode})
			}
			mkdirByPath[rel] = md // dedupe by canonical rel path

		default:
			if opName == "" {
				opName = "<empty>"
			}
			return nil, protocol.NewError(protocol.InvalidLLMOutput, "unsupported op", map[string]any{"op": opName})
		}
	}

	plans := make(map[string]*filePlan, len(replaceByPath)+len(writeByPath))
	loadPlan := func(rel string) (*filePlan, error) {
		if fp, ok := plans[rel]; ok {
			return fp, nil
		}
		abs, err := resolver.getAbs(rel)
		if err != nil {
			return nil, err
		}

		st, statErr := os.Stat(abs)
		exists := false
		perm := os.FileMode(0644)
		var before []byte
		if statErr == nil {
			if st.IsDir() {
				return nil, protocol.NewError(protocol.InvalidLLMOutput, "path is a directory", map[string]any{"path": rel})
			}
			exists = true
			perm = st.Mode().Perm()
			b, rerr := os.ReadFile(abs)
			if rerr != nil {
				return nil, fmt.Errorf("failed to read file %s: %w", rel, rerr)
			}
			before = b
		} else if !os.IsNotExist(statErr) {
			return nil, fmt.Errorf("failed to stat file %s: %w", rel, statErr)
		}

		fp := &filePlan{
			rel:    rel,
			abs:    abs,
			exists: exists,
			perm:   perm,
			before: before,
			after:  append([]byte(nil), before...),
		}
		plans[rel] = fp
		return fp, nil
	}

	// Plan replace_range edits.
	for rel, fileOps := range replaceByPath {
		fp, err := loadPlan(rel)
		if err != nil {
			return nil, err
		}
		after, err := applyReplaceRangeOps(rel, fp.before, fileOps)
		if err != nil {
			return nil, err
		}
		fp.after = after
	}

	// Plan write_atomic writes.
	for rel, wa := range writeByPath {
		fp, err := loadPlan(rel)
		if err != nil {
			return nil, err
		}
		if wa.Conditions.MustNotExist && fp.exists {
			return nil, protocol.NewError(protocol.AlreadyExists, "file already exists", map[string]any{
				"path": rel,
			})
		}
		actualHash := cache.ComputeSHA256(fp.before)
		if strings.TrimSpace(wa.Conditions.FileHash) != "" && strings.TrimSpace(wa.Conditions.FileHash) != actualHash {
			return nil, protocol.NewError(protocol.StaleContent, "cannot apply op: file_hash mismatch", map[string]any{
				"path":          rel,
				"expected_hash": wa.Conditions.FileHash,
				"actual_hash":   actualHash,
			})
		}

		perm := fp.perm
		if wa.Mode != 0 {
			perm = os.FileMode(wa.Mode) & os.ModePerm
		} else if !fp.exists {
			perm = 0644
		}
		fp.perm = perm
		fp.after = []byte(wa.Content)
	}

	// Prepare deterministic output.
	paths := make([]string, 0, len(plans))
	for p := range plans {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	result := &ApplyResult{
		Diffs:        make([]FileDiff, 0, len(paths)),
		ChangedFiles: make([]string, 0, len(paths)),
	}
	for _, rel := range paths {
		fp := plans[rel]
		// M11 + M12 in audit ledger: cap each side of the diff at 64 KiB
		// and refuse binary content (NUL byte in first 8 KiB). A 100 MB
		// file with a 1-line change otherwise produced 200 MB of strings
		// in the JSON-RPC response; a binary edit silently corrupted on
		// non-UTF8 byte sequences during JSON encoding. Mutations still
		// land on disk — the cap only affects the preview payload returned
		// to the caller.
		result.Diffs = append(result.Diffs, FileDiff{
			Path:   rel,
			Before: diffPreviewBytes(fp.before),
			After:  diffPreviewBytes(fp.after),
		})
		if !bytes.Equal(fp.before, fp.after) {
			result.ChangedFiles = append(result.ChangedFiles, rel)
		}
	}

	if opts.DryRun {
		return result, nil
	}

	// TOCTOU re-validation (N2 in audit ledger, Sprint 6).
	//
	// The cross-process apply lock (acquireProjectLock) keeps two Orchestra
	// processes from racing each other, but a NON-Orchestra writer (vim,
	// IDE auto-save, build tool) can mutate a file between loadPlan reading
	// it above and atomicWriteFile below. If we noticed that during planning
	// (file_hash check at lines 257-264 and 395-399), we'd already have
	// returned StaleContent — but the planning read happened earlier, and
	// the mutation could land in the gap. Re-hash each modified file right
	// before we touch it and bail before any write if it has drifted.
	for _, rel := range paths {
		fp := plans[rel]
		if bytes.Equal(fp.before, fp.after) {
			// We're not going to write this file (no change planned), so
			// drift here doesn't matter.
			continue
		}
		current, statErr := os.ReadFile(fp.abs)
		switch {
		case statErr == nil:
			if !fp.exists {
				return nil, protocol.NewError(protocol.StaleContent,
					"file created between plan and apply",
					map[string]any{"path": rel})
			}
			if cache.ComputeSHA256(current) != cache.ComputeSHA256(fp.before) {
				return nil, protocol.NewError(protocol.StaleContent,
					"file changed between plan and apply",
					map[string]any{"path": rel})
			}
		case os.IsNotExist(statErr):
			if fp.exists {
				return nil, protocol.NewError(protocol.StaleContent,
					"file deleted between plan and apply",
					map[string]any{"path": rel})
			}
		default:
			return nil, fmt.Errorf("revalidate %s: %w", rel, statErr)
		}
	}

	// Apply mkdir_all (sorted for determinism).
	mkdirPaths := make([]string, 0, len(mkdirByPath))
	for p := range mkdirByPath {
		mkdirPaths = append(mkdirPaths, p)
	}
	sort.Strings(mkdirPaths)
	for _, rel := range mkdirPaths {
		md := mkdirByPath[rel]
		abs, err := resolver.getAbs(rel)
		if err != nil {
			return nil, err
		}
		mode := os.FileMode(0755)
		if md.Mode != 0 {
			mode = os.FileMode(md.Mode) & os.ModePerm
		}
		if err := os.MkdirAll(abs, mode); err != nil {
			return nil, fmt.Errorf("failed to create directory %s: %w", rel, err)
		}
		// Best-effort: ensure directory doesn't escape via symlink/junction.
		if realDir, err := filepath.EvalSymlinks(abs); err == nil && !isWithinRoot(rootReal, realDir) {
			return nil, protocol.NewError(protocol.PathTraversal, "path escapes workspace (via symlink/junction)", map[string]any{
				"path": rel,
			})
		}
	}

	// Phase 1: write backups in parallel for files that need them.
	//
	// P4 in audit ledger (Sprint 6): backups were written sequentially on
	// the hot path, one atomicWriteFile per file before each main write.
	// For a 1-3 file batch that was already fine; for larger batches the
	// sync I/O accumulated. Backups are independent — a parallel fan-out
	// (capped at 8 to avoid disk saturation) preserves the
	// "backup-before-write" invariant without changing the main-write
	// loop's determinism.
	if opts.Backup && opts.BackupSuffix != "" {
		var backupTargets []backupSpec
		for _, rel := range paths {
			fp := plans[rel]
			if fp.exists && !bytes.Equal(fp.before, fp.after) {
				backupTargets = append(backupTargets, backupSpec{
					rel:  rel,
					abs:  fp.abs,
					data: fp.before,
					perm: fp.perm,
				})
			}
		}
		if err := writeBackupsParallel(backupTargets, opts.BackupSuffix, rootReal); err != nil {
			return nil, err
		}
	}

	// Phase 2: apply file writes in deterministic path order. Sequential
	// because writes carry order-sensitive correctness (e.g. an mkdir
	// preceding a file write within the same batch).
	for _, rel := range paths {
		fp := plans[rel]
		if bytes.Equal(fp.before, fp.after) {
			continue
		}

		if err := atomicWriteFile(fp.abs, fp.after, fp.perm, rootReal); err != nil {
			return nil, fmt.Errorf("failed to write file: %w", err)
		}
	}

	return result, nil
}

// backupSpec is the minimum data writeBackupsParallel needs from a
// filePlan. Decoupled because filePlan is a function-local type inside
// ApplyAnyOps and writeBackupsParallel is a package-level helper.
type backupSpec struct {
	rel  string
	abs  string
	data []byte
	perm os.FileMode
}

// writeBackupsParallel fans out atomic backup writes with bounded
// concurrency. Returns the first error from any worker (subsequent
// errors are silently dropped — the caller bails on the first anyway).
// Order-of-backup doesn't affect correctness because each backup is
// independent.
func writeBackupsParallel(targets []backupSpec, suffix, rootReal string) error {
	if len(targets) == 0 {
		return nil
	}
	if len(targets) == 1 {
		// Fast path: avoid goroutine overhead for the common case.
		fp := targets[0]
		if err := atomicWriteFile(fp.abs+suffix, fp.data, fp.perm, rootReal); err != nil {
			return fmt.Errorf("failed to create backup: %w", err)
		}
		return nil
	}
	const maxParallel = 8
	workers := len(targets)
	if workers > maxParallel {
		workers = maxParallel
	}
	jobs := make(chan backupSpec, len(targets))
	for _, fp := range targets {
		jobs <- fp
	}
	close(jobs)

	var (
		wg      sync.WaitGroup
		firstMu sync.Mutex
		first   error
	)
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for fp := range jobs {
				firstMu.Lock()
				had := first != nil
				firstMu.Unlock()
				if had {
					return // sibling already failed — stop early
				}
				if werr := atomicWriteFile(fp.abs+suffix, fp.data, fp.perm, rootReal); werr != nil {
					firstMu.Lock()
					if first == nil {
						first = fmt.Errorf("failed to create backup for %s: %w", fp.rel, werr)
					}
					firstMu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	return first
}

func applyReplaceRangeOps(relPath string, before []byte, fileOps []ops.ReplaceRangeOp) ([]byte, error) {
	baseHash := cache.ComputeSHA256(before)

	// Apply from bottom to top so earlier edits don't shift later ranges.
	sort.Slice(fileOps, func(i, j int) bool {
		a := fileOps[i].Range.Start
		b := fileOps[j].Range.Start
		if a.Line != b.Line {
			return a.Line > b.Line
		}
		return a.Col > b.Col
	})

	after := append([]byte(nil), before...)

	for _, op := range fileOps {
		if op.Op == "" {
			op.Op = ops.OpFileReplaceRange
		}
		if op.Op != ops.OpFileReplaceRange {
			return nil, protocol.NewError(protocol.InvalidLLMOutput, "unsupported op", map[string]any{
				"op":   op.Op,
				"path": relPath,
			})
		}
		if strings.TrimSpace(op.Path) == "" {
			return nil, protocol.NewError(protocol.InvalidLLMOutput, "op.path is empty", nil)
		}
		if op.Range.Start.Line < 0 || op.Range.Start.Col < 0 || op.Range.End.Line < 0 || op.Range.End.Col < 0 {
			return nil, protocol.NewError(protocol.InvalidLLMOutput, "range must be non-negative", map[string]any{
				"path":  relPath,
				"range": op.Range,
			})
		}

		// If the caller provided a file_hash condition, enforce it strictly.
		// This makes --from-plan deterministic and prevents applying to a changed file.
		if strings.TrimSpace(op.Conditions.FileHash) != "" && strings.TrimSpace(op.Conditions.FileHash) != baseHash {
			return nil, staleContentErr(relPath, op, baseHash, "file_hash mismatch")
		}

		allowFuzzy := op.Conditions.AllowFuzzy
		fuzzyWindow := op.Conditions.FuzzyWindow
		if allowFuzzy && fuzzyWindow <= 0 {
			fuzzyWindow = 2
		}

		startOff, endOff, err := offsetsForRange(after, op.Range)
		if err == nil && startOff <= endOff {
			// Strict check first.
			if bytes.Equal(after[startOff:endOff], []byte(op.Expected)) {
				after = replaceBytes(after, startOff, endOff, []byte(op.Replacement))
				continue
			}
		}

		// Strict failed (range mismatch/out of bounds). Try fuzzy if allowed.
		if allowFuzzy {
			matchStart, matchEnd, matches, findErr := fuzzyFindInWindow(after, op.Expected, op.Range.Start.Line, fuzzyWindow)
			if findErr != nil {
				return nil, staleContentErr(relPath, op, baseHash, findErr.Error())
			}
			if matches == 0 {
				return nil, staleContentErr(relPath, op, baseHash, "fuzzy match not found")
			}
			if matches > 1 {
				return nil, protocol.NewError(protocol.AmbiguousMatch, "fuzzy match ambiguous", map[string]any{
					"path":     relPath,
					"matches":  matches,
					"window":   fuzzyWindow,
					"expected": preview(op.Expected, 200),
				})
			}
			after = replaceBytes(after, matchStart, matchEnd, []byte(op.Replacement))
			continue
		}

		return nil, staleContentErr(relPath, op, baseHash, "strict match failed (and fuzzy disabled)")
	}

	return after, nil
}

func safeAbsPath(rootAbs, rootReal, relPath string) (string, error) {
	// S5 in audit ledger (Sprint 6): lexical validation (empty / "." /
	// "..") is shared with resolver via internal/relpath. Symlink/junction
	// checks below stay here — they need rootAbs/rootReal context that
	// relpath doesn't know about.
	rp, perr := relpath.Normalize(relPath)
	if perr != nil {
		return "", perr
	}
	abs := filepath.Join(rootAbs, filepath.FromSlash(rp))
	abs = filepath.Clean(abs)

	// Defensive lexical recheck after filepath.Join — Windows drive-letter
	// shenanigans (`C:foo.go`) can sneak past relpath.Normalize because
	// Clean alone doesn't detect them.
	rel, err := filepath.Rel(rootAbs, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", protocol.NewError(protocol.PathTraversal, "path escapes workspace", map[string]any{
			"path": relPath,
		})
	}

	// Symlink/junction escape protection: validate the deepest existing parent directory.
	dir := filepath.Dir(abs)
	if realDir, ok, rerr := evalDeepestExistingDir(dir); rerr != nil {
		return "", protocol.NewError(protocol.PathTraversal, "cannot resolve path (symlink/junction)", map[string]any{
			"path":  relPath,
			"error": rerr.Error(),
		})
	} else if ok {
		if !isWithinRoot(rootReal, realDir) {
			return "", protocol.NewError(protocol.PathTraversal, "path escapes workspace (via symlink/junction)", map[string]any{
				"path": relPath,
			})
		}
	}

	// Refuse to write to symlink files (atomic rename would replace the symlink itself).
	if fi, err := os.Lstat(abs); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return "", protocol.NewError(protocol.PathTraversal, "path is a symlink", map[string]any{
				"path": relPath,
			})
		}
	}
	return abs, nil
}

func evalDeepestExistingDir(dir string) (real string, ok bool, _ error) {
	d := filepath.Clean(dir)
	for {
		st, err := os.Stat(d)
		if err == nil && st.IsDir() {
			rd, err := filepath.EvalSymlinks(d)
			if err != nil {
				return "", false, err
			}
			return rd, true, nil
		}
		parent := filepath.Dir(d)
		if parent == d || parent == "." || parent == string(os.PathSeparator) {
			return "", false, nil
		}
		d = parent
	}
}

// isWithinRoot checks if targetAbs is within rootAbs using realpath comparison.
// On Windows, handles case-insensitive comparison and extended paths (\\?\ prefix).
func isWithinRoot(rootAbs, targetAbs string) bool {
	// Normalize both paths
	r := filepath.Clean(rootAbs)
	t := filepath.Clean(targetAbs)

	// Handle Windows extended paths (\\?\ prefix)
	if runtime.GOOS == "windows" {
		// Remove \\?\ prefix if present for comparison
		r = strings.TrimPrefix(r, `\\?\`)
		t = strings.TrimPrefix(t, `\\?\`)
		// Case-insensitive comparison on Windows
		r = strings.ToLower(r)
		t = strings.ToLower(t)
	}

	// Exact match
	if r == t {
		return true
	}

	// Ensure root ends with separator for prefix check
	sep := string(os.PathSeparator)
	if !strings.HasSuffix(r, sep) {
		r += sep
	}

	// Check if target starts with root + separator
	// This prevents false positives like "C:\repo2" matching "C:\repo"
	return strings.HasPrefix(t, r)
}

func atomicWriteFile(path string, data []byte, perm os.FileMode, rootReal string) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	// Best-effort TOCTOU mitigation: after mkdir, ensure the real directory stays within root.
	if strings.TrimSpace(rootReal) != "" {
		if realDir, err := filepath.EvalSymlinks(dir); err == nil && !isWithinRoot(rootReal, realDir) {
			return protocol.NewError(protocol.PathTraversal, "path escapes workspace (via symlink/junction)", map[string]any{
				"path": filepath.ToSlash(path),
			})
		}
	}

	tmp, err := os.CreateTemp(dir, base+".tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpName := tmp.Name()

	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}

	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("failed to sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	// Best-effort atomic replace.
	// Note: on Unix this is atomic within the same directory; on Windows replace is best-effort.
	if err := os.Rename(tmpName, path); err == nil {
		// L5 in audit ledger: os.Chmod on Windows only flips the read-only
		// bit (everything else is ignored). Patch authors who care about
		// exact perms need a POSIX host — documented as a known limitation.
		_ = os.Chmod(path, perm)
		// M10 in audit ledger: fsync the parent directory so the rename's
		// metadata change is durable across a power loss on POSIX. No-op
		// on Windows.
		_ = syncDir(dir)
		return nil
	}

	// H9 in audit ledger: the previous fallback was `os.Remove(path) +
	// os.Rename(tmp, path)`, which deletes the target before the second
	// rename — if THAT rename also fails (cross-device, locked handle on
	// Windows, FS full), the original file is permanently gone. Safer:
	// re-write the contents directly to the target via os.WriteFile. Not
	// atomic with respect to readers, but the target is never absent
	// between the two ops — concurrent readers see either the old bytes
	// or the new bytes, never ENOENT. Backup (.orchestra.bak) was already
	// written earlier in ApplyAnyOps for any pre-existing file, so a
	// crash here is recoverable from .bak.
	if err := os.WriteFile(path, data, perm); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("rename failed and direct overwrite also failed: %w", err)
	}
	_ = os.Remove(tmpName) // best-effort cleanup of the orphan temp
	_ = os.Chmod(path, perm)
	return nil
}

func staleContentErr(path string, op ops.ReplaceRangeOp, actualHash string, reason string) error {
	return protocol.NewError(protocol.StaleContent, "cannot apply op: "+reason, map[string]any{
		"path":          path,
		"expected_hash": op.Conditions.FileHash,
		"actual_hash":   actualHash,
		"range":         op.Range,
		"expected":      preview(op.Expected, 200),
	})
}

func preview(s string, max int) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "\n...(truncated)"
}

// diffPreviewSize caps each side of a FileDiff. 64 KiB is enough to show a
// few hundred lines of context — anything beyond is noise on the wire.
const diffPreviewSize = 64 * 1024

// diffPreviewBytes converts raw file bytes to a JSON-safe preview string
// for FileDiff. Binary files (NUL byte in first 8 KiB) are reported as a
// placeholder so a downstream JSON encoder doesn't choke on non-UTF8
// bytes. M11 + M12 in audit ledger.
func diffPreviewBytes(b []byte) string {
	sniff := 8 * 1024
	if sniff > len(b) {
		sniff = len(b)
	}
	for i := 0; i < sniff; i++ {
		if b[i] == 0 {
			return "<binary file omitted from diff preview>"
		}
	}
	if len(b) <= diffPreviewSize {
		return string(b)
	}
	return string(b[:diffPreviewSize]) + "\n...(truncated; full file written to disk)"
}

func replaceBytes(in []byte, start, end int, replacement []byte) []byte {
	out := make([]byte, 0, len(in)-max(0, end-start)+len(replacement))
	out = append(out, in[:start]...)
	out = append(out, replacement...)
	out = append(out, in[end:]...)
	return out
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func offsetsForRange(content []byte, r ops.Range) (start, end int, _ error) {
	lineStarts := computeLineStarts(content)
	start, err := offsetForPos(lineStarts, content, r.Start)
	if err != nil {
		return 0, 0, err
	}
	end, err = offsetForPos(lineStarts, content, r.End)
	if err != nil {
		return 0, 0, err
	}
	if end < start {
		return 0, 0, fmt.Errorf("range end precedes start")
	}
	return start, end, nil
}

func computeLineStarts(content []byte) []int {
	// lineStarts contains the byte offset for each line start.
	// line 0 always starts at offset 0, even for empty file.
	starts := make([]int, 0, 64)
	starts = append(starts, 0)
	for i := 0; i < len(content); i++ {
		if content[i] == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

func offsetForPos(lineStarts []int, content []byte, p ops.Position) (int, error) {
	if p.Line < 0 || p.Col < 0 {
		return 0, fmt.Errorf("negative position")
	}
	if p.Line >= len(lineStarts) {
		return 0, fmt.Errorf("line out of range")
	}
	lineStart := lineStarts[p.Line]

	// Find line end (exclusive, without '\n').
	lineEnd := len(content)
	if p.Line+1 < len(lineStarts) {
		// next line start - 1 is '\n' (or EOF)
		lineEnd = lineStarts[p.Line+1] - 1
		if lineEnd < lineStart {
			lineEnd = lineStart
		}
	}
	lineLen := lineEnd - lineStart
	if p.Col > lineLen {
		return 0, fmt.Errorf("col out of range")
	}
	return lineStart + p.Col, nil
}

func fuzzyFindInWindow(content []byte, expected string, startLine int, window int) (matchStart, matchEnd, matches int, _ error) {
	if window < 0 {
		return 0, 0, 0, fmt.Errorf("invalid fuzzy window")
	}

	lineStarts := computeLineStarts(content)
	if len(lineStarts) == 0 {
		lineStarts = []int{0}
	}
	if startLine < 0 {
		startLine = 0
	}
	if startLine >= len(lineStarts) {
		startLine = len(lineStarts) - 1
	}

	startL := startLine - window
	if startL < 0 {
		startL = 0
	}
	endL := startLine + window
	if endL >= len(lineStarts) {
		endL = len(lineStarts) - 1
	}

	winStart := lineStarts[startL]
	// endL is inclusive; take start offset of line endL+1 as the end, or EOF.
	winEnd := len(content)
	if endL+1 < len(lineStarts) {
		winEnd = lineStarts[endL+1]
	}
	if winEnd < winStart {
		winEnd = winStart
	}

	hay := content[winStart:winEnd]
	needle := []byte(expected)

	// Count occurrences (up to 2).
	idx := 0
	for {
		j := bytes.Index(hay[idx:], needle)
		if j < 0 {
			break
		}
		matches++
		if matches > 1 {
			return 0, 0, matches, nil
		}
		matchStart = winStart + idx + j
		matchEnd = matchStart + len(needle)
		idx = idx + j + max(1, len(needle))
	}

	return matchStart, matchEnd, matches, nil
}
