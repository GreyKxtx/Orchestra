package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	toolsfs "github.com/orchestra/orchestra/internal/tools/fs"
	"github.com/orchestra/orchestra/patch/fsutil"
)

// shadowWorkspace is where a preview turn runs its commands: a copy of the
// workspace with the turn's staged edits written over it. A command sees the
// edits the model made (a test runs against the code it just changed, not
// the code on disk), what the command writes comes back to the turn as
// staged edits, and the workspace itself is never touched — a preview
// changes nothing, which is what a remote client was promised when the core
// refused every command in one.
//
// The copy leaves out .git, .orchestra and the project's exclude_dirs; an
// excluded directory (node_modules, a build cache) is linked in so the
// command can still read it. Files are copied once, on the turn's first
// command; before each command the copy is brought up to date with the
// files that changed on disk and with the staged edits, and after it the
// files the command changed are staged.
type shadowWorkspace struct {
	root     string
	src      string
	excludes map[string]bool

	mu sync.Mutex
	// shadow is what the shadow holds for each path, as this side wrote it;
	// a file whose stamp moved was written by a command.
	shadow map[string]fileStamp
	// disk is the workspace file each unstaged copy came from; a file whose
	// stamp moved on disk is copied again.
	disk map[string]fileStamp
	// staged is the set of paths the last sync wrote from the overlay.
	staged map[string]bool
	// linked is the set of excluded directories linked in, not copied.
	linked map[string]bool
}

type fileStamp struct {
	size    int64
	modTime time.Time
	hash    string
}

// Limits for the copy: a workspace past them keeps refusing commands in a
// preview, with the reason. Variables so a test can lower them.
var (
	shadowMaxFiles       = 20000
	shadowMaxBytes int64 = 512 << 20
)

// shadowMaxStagedBytes is the largest file a command wrote that is staged
// back; a build output past it stays in the shadow.
const shadowMaxStagedBytes = 1 << 20

var errShadowTooLarge = errors.New("the workspace is too large to copy for a preview")

// shadowAlwaysSkipped are never part of the shadow, whatever exclude_dirs
// says: the repository's own store and Orchestra's records.
var shadowAlwaysSkipped = map[string]bool{".git": true, ".orchestra": true}

func newShadowWorkspace(src string, excludeDirs []string) (*shadowWorkspace, error) {
	root, err := os.MkdirTemp("", "orchestra-shadow-*")
	if err != nil {
		return nil, fmt.Errorf("shadow dir: %w", err)
	}
	s := &shadowWorkspace{
		root:     root,
		src:      src,
		excludes: make(map[string]bool, len(excludeDirs)),
		shadow:   make(map[string]fileStamp),
		disk:     make(map[string]fileStamp),
		staged:   make(map[string]bool),
		linked:   make(map[string]bool),
	}
	for _, d := range excludeDirs {
		if d = strings.Trim(filepath.ToSlash(strings.TrimSpace(d)), "/"); d != "" {
			s.excludes[d] = true
		}
	}
	if err := s.refreshFromDisk(true); err != nil {
		_ = os.RemoveAll(root)
		return nil, err
	}
	return s, nil
}

// Root is the directory commands run in.
func (s *shadowWorkspace) Root() string { return s.root }

// Close removes the copy.
func (s *shadowWorkspace) Close() {
	if s == nil {
		return
	}
	_ = os.RemoveAll(s.root)
}

// skipDir reports whether a directory of the workspace stays out of the
// copy: the always-skipped ones and exclude_dirs, matched by name at any
// depth and by workspace-relative path.
func (s *shadowWorkspace) skipDir(rel, name string) bool {
	return shadowAlwaysSkipped[name] || s.excludes[name] || s.excludes[rel]
}

// refreshFromDisk copies into the shadow the workspace files that are not
// there yet or changed since they were copied, and drops the ones that are
// gone. Staged paths are left to sync. initial enforces the size limits.
func (s *shadowWorkspace) refreshFromDisk(initial bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := make(map[string]bool, len(s.disk))
	files, bytes := 0, int64(0)
	err := filepath.WalkDir(s.src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if path == s.src {
				return walkErr
			}
			return nil
		}
		rel, err := filepath.Rel(s.src, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if s.skipDir(rel, d.Name()) {
				s.linkExcluded(rel, path)
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		seen[rel] = true
		if initial {
			files++
			bytes += info.Size()
			if files > shadowMaxFiles || bytes > shadowMaxBytes {
				return errShadowTooLarge
			}
		}
		if s.staged[rel] {
			return nil
		}
		if prev, ok := s.disk[rel]; ok && prev.size == info.Size() && prev.modTime.Equal(info.ModTime()) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		stamp, err := s.write(rel, data, info.Mode().Perm())
		if err != nil {
			return err
		}
		s.disk[rel] = fileStamp{size: info.Size(), modTime: info.ModTime(), hash: stamp.hash}
		return nil
	})
	if err != nil {
		return err
	}
	for rel := range s.disk {
		if !seen[rel] && !s.staged[rel] {
			s.remove(rel)
		}
	}
	return nil
}

// linkExcluded links an excluded directory into the shadow so a command can
// read it (node_modules, a build cache); where links are not available the
// directory is left out.
func (s *shadowWorkspace) linkExcluded(rel, path string) {
	if shadowAlwaysSkipped[filepath.Base(path)] || s.linked[rel] {
		return
	}
	dst := filepath.Join(s.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return
	}
	if err := os.Symlink(path, dst); err == nil {
		s.linked[rel] = true
	}
}

// write puts content at rel in the shadow and records what it holds.
func (s *shadowWorkspace) write(rel string, data []byte, perm os.FileMode) (fileStamp, error) {
	dst := filepath.Join(s.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fileStamp{}, fmt.Errorf("shadow %s: %w", rel, err)
	}
	if perm == 0 {
		perm = 0o644
	}
	if err := os.WriteFile(dst, data, perm|0o200); err != nil {
		return fileStamp{}, fmt.Errorf("shadow %s: %w", rel, err)
	}
	info, err := os.Stat(dst)
	if err != nil {
		return fileStamp{}, fmt.Errorf("shadow %s: %w", rel, err)
	}
	stamp := fileStamp{size: info.Size(), modTime: info.ModTime(), hash: fsutil.ComputeSHA256(data)}
	s.shadow[rel] = stamp
	return stamp, nil
}

func (s *shadowWorkspace) remove(rel string) {
	_ = os.Remove(filepath.Join(s.root, filepath.FromSlash(rel)))
	delete(s.shadow, rel)
	delete(s.disk, rel)
}

// sync writes the overlay's staged edits into the shadow and puts back the
// workspace file under a path that is no longer staged.
func (s *shadowWorkspace) sync(overlay *toolsfs.Overlay) error {
	if err := s.refreshFromDisk(false); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current := overlay.StagedFileContent()
	for rel := range s.staged {
		if _, still := current[rel]; still {
			continue
		}
		delete(s.staged, rel)
		src := filepath.Join(s.src, filepath.FromSlash(rel))
		data, err := os.ReadFile(src)
		if err != nil {
			s.remove(rel)
			continue
		}
		info, _ := os.Stat(src)
		stamp, err := s.write(rel, data, info.Mode().Perm())
		if err != nil {
			return err
		}
		s.disk[rel] = fileStamp{size: info.Size(), modTime: info.ModTime(), hash: stamp.hash}
	}
	for rel, content := range current {
		s.staged[rel] = true
		if prev, ok := s.shadow[rel]; ok && prev.hash == fsutil.ComputeSHA256([]byte(content)) {
			continue
		}
		if _, err := s.write(rel, []byte(content), 0); err != nil {
			return err
		}
	}
	return nil
}

// collect stages into the overlay the files a command changed or created in
// the shadow, and reports what it staged and what it left: a file that is
// binary or larger than shadowMaxStagedBytes (a build output) stays in the
// shadow, and a file the command deleted stays in the workspace.
func (s *shadowWorkspace) collect(overlay *toolsfs.Overlay, client *toolsfs.Client) (staged []string, note string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var skipped, deleted []string
	seen := make(map[string]bool, len(s.shadow))
	_ = filepath.WalkDir(s.root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if path == s.root {
				return walkErr
			}
			return nil
		}
		rel, err := filepath.Rel(s.root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if s.skipDir(rel, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		seen[rel] = true
		if prev, ok := s.shadow[rel]; ok && prev.size == info.Size() && prev.modTime.Equal(info.ModTime()) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		hash := fsutil.ComputeSHA256(data)
		if prev, ok := s.shadow[rel]; ok && prev.hash == hash {
			s.shadow[rel] = fileStamp{size: info.Size(), modTime: info.ModTime(), hash: hash}
			return nil
		}
		if info.Size() > shadowMaxStagedBytes || looksBinary(data) {
			skipped = append(skipped, rel)
			s.shadow[rel] = fileStamp{size: info.Size(), modTime: info.ModTime(), hash: hash}
			return nil
		}
		if err := overlay.StageFromCommand(client, rel, string(data)); err != nil {
			skipped = append(skipped, rel)
			return nil
		}
		s.shadow[rel] = fileStamp{size: info.Size(), modTime: info.ModTime(), hash: hash}
		s.staged[rel] = true
		staged = append(staged, rel)
		return nil
	})
	for rel := range s.shadow {
		if !seen[rel] {
			deleted = append(deleted, rel)
			delete(s.shadow, rel)
			delete(s.disk, rel)
			delete(s.staged, rel)
		}
	}
	sort.Strings(staged)
	sort.Strings(skipped)
	sort.Strings(deleted)
	var parts []string
	if len(staged) > 0 {
		parts = append(parts, "the command's changes to "+strings.Join(staged, ", ")+" are staged with this turn's edits")
	}
	if len(skipped) > 0 {
		parts = append(parts, "not staged (binary or over 1 MiB): "+strings.Join(skipped, ", "))
	}
	if len(deleted) > 0 {
		parts = append(parts, "deleted by the command in the preview only: "+strings.Join(deleted, ", "))
	}
	return staged, strings.Join(parts, "; ")
}

// looksBinary reports a NUL byte in the first 8 KiB.
func looksBinary(data []byte) bool {
	if len(data) > 8192 {
		data = data[:8192]
	}
	return bytes.IndexByte(data, 0) >= 0
}

// shadowFor is the shadow workspace of overlay, made on first use. An error
// is remembered for the runner: a workspace too large to copy stays so.
func (r *Runner) shadowFor(overlay *toolsfs.Overlay) (*shadowWorkspace, error) {
	if r == nil {
		return nil, errors.New("runner is nil")
	}
	r.shadowMu.Lock()
	defer r.shadowMu.Unlock()
	if r.shadowErr != nil {
		return nil, r.shadowErr
	}
	if sh, ok := r.shadows[overlay]; ok {
		return sh, nil
	}
	sh, err := newShadowWorkspace(r.workspaceRoot, r.excludeDirs)
	if err != nil {
		r.shadowErr = err
		return nil, err
	}
	if r.shadows == nil {
		r.shadows = make(map[*toolsfs.Overlay]*shadowWorkspace)
	}
	r.shadows[overlay] = sh
	return sh, nil
}

// closeShadowOf removes the shadow of overlay: a turn that closed, a task
// layer that was dropped or committed.
func (r *Runner) closeShadowOf(overlay *toolsfs.Overlay) {
	if r == nil {
		return
	}
	r.shadowMu.Lock()
	sh := r.shadows[overlay]
	delete(r.shadows, overlay)
	r.shadowMu.Unlock()
	sh.Close()
}

func (r *Runner) closeShadows() {
	if r == nil {
		return
	}
	r.shadowMu.Lock()
	all := r.shadows
	r.shadows = nil
	r.shadowMu.Unlock()
	for _, sh := range all {
		sh.Close()
	}
}

// VerificationRoot is where the runtime verifies the edits of the agent
// behind ctx — builds, tests, acceptance checks, a typecheck. In a preview
// with exec.shadow on it is the agent's shadow workspace, brought up to date
// with its staged edits, and inShadow is true; otherwise the workspace root.
// A preview whose workspace cannot be shadowed gets the error the commands
// get, so the caller can say why it skipped.
func (r *Runner) VerificationRoot(ctx context.Context) (root string, inShadow bool, err error) {
	if r == nil {
		return "", false, errors.New("runner is nil")
	}
	t := r.TurnAt(ctx)
	if !t.DryRun() || !r.shadowExec {
		return r.workspaceRoot, false, nil
	}
	overlay := r.overlayAt(ctx)
	sh, err := r.shadowFor(overlay)
	if err != nil {
		return "", false, err
	}
	if err := sh.sync(overlay); err != nil {
		return "", false, err
	}
	return sh.Root(), true, nil
}
