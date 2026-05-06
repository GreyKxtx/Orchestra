package applier

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/orchestra/orchestra/internal/ops"
	"github.com/orchestra/orchestra/internal/protocol"
	"github.com/orchestra/orchestra/internal/cache"
)

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
func ApplyAnyOps(root string, in []ops.AnyOp, opts ApplyOptions) (*ApplyResult, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("root is empty")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("abs root: %w", err)
	}
	rootReal := rootAbs
	if rp, err := filepath.EvalSymlinks(rootAbs); err == nil {
		rootReal = rp
	}

	type filePlan struct {
		rel    string
		abs    string
		exists bool
		perm   os.FileMode
		before []byte
		after  []byte
	}

	// Collect ops by kind/path (using canonical slash paths for stable ordering).
	replaceByPath := make(map[string][]ops.ReplaceRangeOp)
	writeByPath := make(map[string]ops.WriteAtomicOp)
	mkdirByPath := make(map[string]ops.MkdirAllOp)

	absByRel := make(map[string]string)
	getAbs := func(rel string) (string, error) {
		if v, ok := absByRel[rel]; ok {
			return v, nil
		}
		abs, err := safeAbsPath(rootAbs, rootReal, rel)
		if err != nil {
			return "", err
		}
		absByRel[rel] = abs
		return abs, nil
	}

	canonRel := func(p string) (string, error) {
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
			rel, err := canonRel(rr.Path)
			if err != nil {
				return nil, err
			}
			rr.Path = rel
			if _, err := getAbs(rel); err != nil {
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
			rel, err := canonRel(wa.Path)
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
			if _, err := getAbs(rel); err != nil {
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
			rel, err := canonRel(md.Path)
			if err != nil {
				return nil, err
			}
			md.Path = rel
			if _, err := getAbs(rel); err != nil {
				return nil, err
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
		abs, err := getAbs(rel)
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
		result.Diffs = append(result.Diffs, FileDiff{
			Path:   rel,
			Before: string(fp.before),
			After:  string(fp.after),
		})
		if !bytes.Equal(fp.before, fp.after) {
			result.ChangedFiles = append(result.ChangedFiles, rel)
		}
	}

	if opts.DryRun {
		return result, nil
	}

	// Apply mkdir_all (sorted for determinism).
	mkdirPaths := make([]string, 0, len(mkdirByPath))
	for p := range mkdirByPath {
		mkdirPaths = append(mkdirPaths, p)
	}
	sort.Strings(mkdirPaths)
	for _, rel := range mkdirPaths {
		md := mkdirByPath[rel]
		abs, err := getAbs(rel)
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

	// Apply file writes in deterministic path order.
	for _, rel := range paths {
		fp := plans[rel]
		if bytes.Equal(fp.before, fp.after) {
			continue
		}

		// Backup existing file if requested (never for new files).
		if opts.Backup && fp.exists && opts.BackupSuffix != "" {
			backupPath := fp.abs + opts.BackupSuffix
			if err := atomicWriteFile(backupPath, fp.before, fp.perm, rootReal); err != nil {
				return nil, fmt.Errorf("failed to create backup: %w", err)
			}
		}

		if err := atomicWriteFile(fp.abs, fp.after, fp.perm, rootReal); err != nil {
			return nil, fmt.Errorf("failed to write file: %w", err)
		}
	}

	return result, nil
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

func applyOpsToFile(rootAbs, rootReal, relPath string, fileOps []ops.ReplaceRangeOp, opts ApplyOptions) (*FileDiff, error) {
	filePath, err := safeAbsPath(rootAbs, rootReal, relPath)
	if err != nil {
		return nil, err
	}

	var before []byte
	var beforeMode os.FileMode = 0644
	if st, err := os.Stat(filePath); err == nil && !st.IsDir() {
		beforeMode = st.Mode().Perm()
		b, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read file %s: %w", relPath, err)
		}
		before = b
	}

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
			// Hash mismatch is a stale signal, but fuzzy may still recover (per spec).
			if op.Conditions.FileHash != "" && op.Conditions.FileHash != baseHash {
				// no-op; error payload will include hashes if fuzzy also fails
			}
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

	// Apply changes if not dry-run
	if !opts.DryRun {
		// Create backup if needed
		if opts.Backup && len(before) > 0 {
			backupPath := filePath + opts.BackupSuffix
			if err := atomicWriteFile(backupPath, before, beforeMode, rootReal); err != nil {
				return nil, fmt.Errorf("failed to create backup: %w", err)
			}
		}

		if err := atomicWriteFile(filePath, after, beforeMode, rootReal); err != nil {
			return nil, fmt.Errorf("failed to write file: %w", err)
		}
	}

	return &FileDiff{
		Path:   relPath,
		Before: string(before),
		After:  string(after),
	}, nil
}

func safeAbsPath(rootAbs, rootReal, relPath string) (string, error) {
	rp := filepath.ToSlash(strings.TrimSpace(relPath))
	if rp == "" {
		return "", protocol.NewError(protocol.InvalidLLMOutput, "path is empty", nil)
	}
	rp = filepath.Clean(filepath.FromSlash(rp))
	if rp == "." {
		return "", protocol.NewError(protocol.InvalidLLMOutput, "path is invalid", map[string]any{"path": relPath})
	}
	abs := filepath.Join(rootAbs, rp)
	abs = filepath.Clean(abs)

	// Security: ensure path is within root (lexical).
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
		_ = os.Chmod(path, perm)
		return nil
	}

	// Windows: os.Rename fails if destination exists.
	_ = os.Remove(path)
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}
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
