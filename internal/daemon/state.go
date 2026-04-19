package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/orchestra/orchestra/internal/projectfs"
	"github.com/orchestra/orchestra/internal/store"
)

type FileState struct {
	Path  string
	Size  int64
	MTime int64 // unix nanos
	Hash  string

	content []byte // cached only for small files
}

type State struct {
	projectRootAbs string
	projectID      string
	configHash     string

	excludeDirs       []string
	skipBackups       bool
	maxCacheFileBytes int64

	mu        sync.RWMutex
	files     map[string]*FileState // key: rel path (forward slashes)
	indexedAt time.Time
}

type ScanResult struct {
	Scanned     int
	Changed     int
	FilesSeen   int
	CacheFastOK int // Files validated via mtime+size (no hash needed)
	CacheHashed int // Files that needed hash computation
}

func NewState(projectRoot string, projectID string, configHash string, excludeDirs []string, skipBackups bool, maxCacheFileBytes int64, seed map[string]store.FileRef) (*State, error) {
	rootAbs, err := filepath.Abs(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute project root: %w", err)
	}

	files := make(map[string]*FileState)
	for rel, ref := range seed {
		key := filepath.ToSlash(rel)
		files[key] = &FileState{
			Path:  key,
			Size:  ref.Size,
			MTime: ref.MTime,
			Hash:  ref.Hash,
		}
	}

	return &State{
		projectRootAbs:    rootAbs,
		projectID:         projectID,
		configHash:        configHash,
		excludeDirs:       append([]string(nil), excludeDirs...),
		skipBackups:       skipBackups,
		maxCacheFileBytes: maxCacheFileBytes,
		files:             files,
		indexedAt:         time.Time{},
	}, nil
}

func (s *State) ProjectRoot() string { return s.projectRootAbs }
func (s *State) ProjectID() string   { return s.projectID }
func (s *State) ConfigHash() string  { return s.configHash }

func (s *State) SnapshotFiles() []FileMeta {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]FileMeta, 0, len(s.files))
	for _, f := range s.files {
		out = append(out, FileMeta{Path: f.Path, Size: f.Size, MTime: f.MTime, Hash: f.Hash})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func (s *State) GetFileMeta(path string) (FileMeta, bool) {
	key, err := normalizeRelPath(path)
	if err != nil {
		return FileMeta{}, false
	}

	s.mu.RLock()
	f, ok := s.files[key]
	s.mu.RUnlock()
	if !ok {
		return FileMeta{}, false
	}
	return FileMeta{Path: f.Path, Size: f.Size, MTime: f.MTime, Hash: f.Hash}, true
}

func (s *State) ReadFile(path string) ([]byte, FileMeta, error) {
	key, err := normalizeRelPath(path)
	if err != nil {
		return nil, FileMeta{}, err
	}

	s.mu.RLock()
	f, ok := s.files[key]
	if ok && len(f.content) > 0 {
		content := append([]byte(nil), f.content...)
		meta := FileMeta{Path: f.Path, Size: f.Size, MTime: f.MTime, Hash: f.Hash}
		s.mu.RUnlock()
		return content, meta, nil
	}
	s.mu.RUnlock()

	// Read from disk (with traversal protection).
	info, err := projectfs.ReadFile(s.projectRootAbs, filepath.FromSlash(key))
	if err != nil {
		return nil, FileMeta{}, err
	}

	st, err := os.Stat(info.Path)
	if err != nil {
		return nil, FileMeta{}, err
	}
	meta := FileMeta{Path: key, Size: info.Size, MTime: st.ModTime().UnixNano()}
	data := []byte(info.Content)

	// Cache content for small files.
	if info.Size <= s.maxCacheFileBytes {
		s.mu.Lock()
		if existing, ok := s.files[key]; ok {
			existing.content = append([]byte(nil), data...)
			// Keep existing mtime/hash as-is; periodic scan will update metadata.
		}
		s.mu.Unlock()
	}

	return data, meta, nil
}

func (s *State) ScanOnce(ctx context.Context) (ScanResult, error) {
	excludeMap := make(map[string]bool)
	for _, dir := range s.excludeDirs {
		excludeMap[dir] = true
	}
	// Always exclude orchestra runtime dir
	excludeMap[".orchestra"] = true

	s.mu.RLock()
	old := s.files
	s.mu.RUnlock()

	next := make(map[string]*FileState, len(old))
	seen := make(map[string]struct{}, len(old))

	scanned := 0
	changed := 0
	cacheFastOK := 0
	cacheHashed := 0

	err := filepath.WalkDir(s.projectRootAbs, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if d.IsDir() {
			relPath, _ := filepath.Rel(s.projectRootAbs, path)
			dirName := filepath.Base(path)
			if excludeMap[dirName] || excludeMap[relPath] {
				return filepath.SkipDir
			}
			return nil
		}

		if s.skipBackups && strings.HasSuffix(path, ".orchestra.bak") {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}

		relPath, err := filepath.Rel(s.projectRootAbs, path)
		if err != nil {
			return nil
		}
		key := filepath.ToSlash(relPath)
		seen[key] = struct{}{}
		scanned++

		size := info.Size()
		mtime := info.ModTime().UnixNano()

		if prev, ok := old[key]; ok {
			if prev.Size == size && prev.MTime == mtime {
				next[key] = prev
				cacheFastOK++
				return nil
			}
		}

		// Changed or new file.
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		h := store.ComputeSHA256(data)
		cacheHashed++
		fs := &FileState{Path: key, Size: size, MTime: mtime, Hash: h}
		if size <= s.maxCacheFileBytes {
			fs.content = data
		}
		next[key] = fs
		changed++
		return nil
	})
	if err != nil {
		return ScanResult{}, err
	}

	// Count removals.
	removed := 0
	for k := range old {
		if _, ok := seen[k]; !ok {
			removed++
		}
	}
	changed += removed

	s.mu.Lock()
	s.files = next
	s.indexedAt = time.Now()
	s.mu.Unlock()

	return ScanResult{
		Scanned:     scanned,
		Changed:     changed,
		FilesSeen:   scanned,
		CacheFastOK: cacheFastOK,
		CacheHashed: cacheHashed,
	}, nil
}

func (s *State) ToCache() *store.Cache {
	s.mu.RLock()
	defer s.mu.RUnlock()

	files := make(map[string]store.FileRef, len(s.files))
	for k, f := range s.files {
		files[k] = store.FileRef{Size: f.Size, MTime: f.MTime, Hash: f.Hash}
	}
	return store.NewCache(s.projectRootAbs, s.projectID, s.configHash, files)
}

func normalizeRelPath(path string) (string, error) {
	p := filepath.ToSlash(path)
	p = strings.TrimSpace(p)
	if p == "" {
		return "", fmt.Errorf("path is empty")
	}
	p = filepath.Clean(filepath.FromSlash(p))
	p = filepath.ToSlash(p)
	if p == "." {
		return "", fmt.Errorf("path is invalid")
	}
	if p == ".." || strings.HasPrefix(p, "../") {
		return "", fmt.Errorf("invalid path: %s", path)
	}
	return p, nil
}
