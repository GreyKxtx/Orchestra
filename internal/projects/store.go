package projects

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/orchestra/orchestra/patch/cache"
	"github.com/orchestra/orchestra/patch/fsutil"
)

// storeFile is the on-disk shape. Paths only: this file must never hold a
// token, so a stray backup of it leaks nothing but directory names.
type storeFile struct {
	Projects []string `json:"projects"`
}

// StorePath is ~/.orchestra/projects.json — the same ~/.orchestra that holds
// the user-level config and skills (os.UserHomeDir, no override).
func StorePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".orchestra", "projects.json"), nil
}

// LoadPaths reads the remembered project paths. An absent or corrupt list is an
// empty list, never an error: a corrupt file must not stop the server from
// starting, because then the user cannot reach the UI to fix it, and the next
// SavePaths overwrites it atomically. Any other read failure (permissions, a
// directory in the way) IS an error, so the caller knows not to overwrite a
// list it never saw.
func LoadPaths(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read project list: %w", err)
	}
	var f storeFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, nil
	}
	return f.Projects, nil
}

// SavePaths writes the list atomically (temp file → fsync → rename) with
// owner-only permissions; the parent directory is created when missing.
func SavePaths(path string, paths []string) error {
	b, err := json.MarshalIndent(storeFile{Projects: paths}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode project list: %w", err)
	}
	b = append(b, '\n')
	if err := fsutil.AtomicWriteFile(path, b, 0600); err != nil {
		return fmt.Errorf("write project list: %w", err)
	}
	return nil
}

// Store is the remembered project list — the projects the interface shows
// whether or not the core currently holds them. Closing a project keeps it
// here; only Forget removes it. That distinction is the whole reason this
// type exists instead of the server saving Registry.Paths() on the way out.
type Store struct {
	path string

	mu    sync.Mutex
	paths []string // cleaned absolute paths, deduped by project id
}

// NewStore loads the list at path. An absent or corrupt file is an empty list
// (LoadPaths' contract); any other read failure is returned, because a list we
// could not read is a list we must not overwrite.
func NewStore(path string) (*Store, error) {
	loaded, err := LoadPaths(path)
	if err != nil {
		return nil, err
	}
	s := &Store{path: path}
	for _, p := range loaded {
		abs, aerr := filepath.Abs(p)
		if aerr != nil {
			continue // a path we cannot resolve is not a project we can open
		}
		s.insert(filepath.Clean(abs))
	}
	return s, nil
}

// identity is the dedupe key: the same project id the registry and the HTTP
// API use, which case-folds on Windows. Falling back to the path keeps a
// project that cannot be hashed visible rather than dropping it.
func identity(abs string) string {
	if id, err := cache.ComputeProjectID(abs); err == nil {
		return id
	}
	return abs
}

// insert adds abs unless an entry with the same identity is already present.
// Caller holds no lock on the first call from NewStore (no other reference
// exists yet); every other caller holds s.mu.
func (s *Store) insert(abs string) bool {
	key := identity(abs)
	for _, p := range s.paths {
		if identity(p) == key {
			return false
		}
	}
	s.paths = append(s.paths, abs)
	sort.Strings(s.paths)
	return true
}

// snapshotLocked copies the list; the caller holds s.mu.
func (s *Store) snapshotLocked() []string {
	out := make([]string, len(s.paths))
	copy(out, s.paths)
	return out
}

// Paths returns the remembered paths in a stable order.
func (s *Store) Paths() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked()
}

// Add remembers a path and writes the list. Adding one that is already
// remembered writes nothing and is not an error.
//
// The write happens while the lock is held, on purpose. Snapshotting under the
// lock and writing outside it lets two concurrent Adds persist in either
// order, leaving the file disagreeing with memory until the next write. This
// path runs when a user opens a project — holding a mutex across one small
// atomic write costs nothing worth measuring, and the alternative is a bug
// that only shows up as a project missing after a restart.
func (s *Store) Add(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve project path %q: %w", path, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.insert(filepath.Clean(abs)) {
		return nil
	}
	return SavePaths(s.path, s.snapshotLocked())
}

// Forget drops a path and writes the list. The bool says whether it was
// remembered, so a caller can answer 404 for an id nobody knows. The write is
// under the lock for the same reason as Add.
func (s *Store) Forget(path string) (bool, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false, fmt.Errorf("resolve project path %q: %w", path, err)
	}
	key := identity(filepath.Clean(abs))

	s.mu.Lock()
	defer s.mu.Unlock()
	kept := make([]string, 0, len(s.paths))
	found := false
	for _, p := range s.paths {
		if identity(p) == key {
			found = true
			continue
		}
		kept = append(kept, p)
	}
	if !found {
		return false, nil
	}
	s.paths = kept
	return true, SavePaths(s.path, s.snapshotLocked())
}

// PathForID resolves a remembered path by project id, so the API can act on a
// closed project the client knows only by id.
func (s *Store) PathForID(id string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.paths {
		if identity(p) == id {
			return p, true
		}
	}
	return "", false
}
