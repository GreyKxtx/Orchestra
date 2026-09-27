package ckg

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"
)

type Scanner struct {
	store   *Store
	root    string
	ignores []string
}

// NewScannerWithIgnores creates a scanner with project-configured exclusions
// in addition to the built-in and ignore-file rules.
func NewScannerWithIgnores(store *Store, root string, ignores []string) *Scanner {
	s := &Scanner{
		store:   store,
		root:    root,
		ignores: []string{".git", "vendor", "node_modules", "dist", "build", ".orchestra"},
	}
	for _, ignore := range ignores {
		s.addIgnore(ignore)
	}
	s.loadIgnoreFile(".gitignore")
	s.loadIgnoreFile(".orchestraignore")
	return s
}

func (s *Scanner) addIgnore(ignore string) {
	ignore = filepath.ToSlash(strings.TrimSpace(ignore))
	ignore = strings.Trim(ignore, "/")
	if ignore != "" && !strings.HasPrefix(ignore, "!") {
		s.ignores = append(s.ignores, ignore)
	}
}

func (s *Scanner) loadIgnoreFile(filename string) {
	path := filepath.Join(s.root, filename)
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		s.addIgnore(line)
	}
}

func (s *Scanner) isIgnored(path string) bool {
	rel, err := filepath.Rel(s.root, path)
	if err != nil {
		return false
	}
	if rel == "." || rel == "" {
		return false
	}

	relSlash := filepath.ToSlash(rel)
	parts := strings.Split(relSlash, "/")
	for _, ignore := range s.ignores {
		if strings.Contains(ignore, "/") {
			if relSlash == ignore || strings.HasPrefix(relSlash, ignore+"/") {
				return true
			}
			if matched, _ := pathpkg.Match(ignore, relSlash); matched {
				return true
			}
			continue
		}
		for _, part := range parts {
			if part == ignore {
				return true
			}
			matched, _ := filepath.Match(ignore, part)
			if matched {
				return true
			}
		}
	}
	return false
}

// ScanResult is what one pass over the workspace found.
type ScanResult struct {
	// ToParse are new or modified files; ToDelete files the graph has that
	// the tree no longer does.
	ToParse  []string
	ToDelete []string
	// Stamps is every indexable file the pass looked at, with the hash the
	// graph should hold for it: the whole tree after a walk, the changed
	// files after a pass the watcher named.
	Stamps map[string]FileStamp
	// Restamp are unchanged files whose stored stamp is stale (a touch, a row
	// from before stamps existed): the pass records the new stamp so the next
	// walk does not hash them again.
	Restamp map[string]FileStamp
	// Hashed is how many files had to be read: their stamp was unknown or had
	// changed. An unchanged tree hashes nothing.
	Hashed int
	// Walked says the pass walked the tree; false when the store's change
	// feed named the changes and nothing else was looked at.
	Walked bool
	// Ack tells the change feed the pass landed what the snapshot named. The
	// orchestrator calls it once the graph is written, never on a failed
	// pass. Nil without a feed.
	Ack func()
}

// ScanChanges finds what the graph has to take in. With a change feed on the
// store (a Watcher) it looks only at the paths the feed names, unless the
// feed owes a walk; without one it walks the workspace. A file whose mtime
// and size match its stored stamp is taken as unchanged without being read;
// every pass used to hash every file (DATA-3).
//
// It reads the store without locking it: the orchestrator holds the graph
// lock it needs around the call (the read lock for a refresh, the write
// lock for the warmup, which reserves it up front).
func (s *Scanner) ScanChanges(ctx context.Context) (*ScanResult, error) {
	feed := s.store.ChangeFeed()
	if feed == nil {
		return s.walkAll(ctx)
	}
	paths, full, gen := feed.Snapshot()
	var res *ScanResult
	var err error
	if full {
		res, err = s.walkAll(ctx)
	} else {
		res, err = s.scanPaths(ctx, paths)
	}
	if err != nil {
		return nil, err
	}
	res.Ack = func() { feed.Ack(gen) }
	return res, nil
}

// walkAll is the pass over the whole tree.
func (s *Scanner) walkAll(ctx context.Context) (*ScanResult, error) {
	known, err := s.store.fileStampsUnlocked(ctx)
	if err != nil {
		return nil, err
	}
	res := &ScanResult{Stamps: make(map[string]FileStamp, len(known)), Restamp: map[string]FileStamp{}, Walked: true}
	if err := s.walk(s.root, known, res); err != nil {
		return nil, err
	}
	res.finish(known, func(path string) bool {
		_, exists := res.Stamps[path]
		return !exists
	})
	return res, nil
}

// scanPaths is the pass a change feed makes possible: it looks at the named
// paths and at nothing else. A path that is gone takes with it whatever the
// graph had at or under it, so a directory removed or renamed away needs no
// notification per file; a directory that appeared is walked.
func (s *Scanner) scanPaths(ctx context.Context, paths []string) (*ScanResult, error) {
	res := &ScanResult{Stamps: map[string]FileStamp{}, Restamp: map[string]FileStamp{}}
	if len(paths) == 0 {
		return res, nil
	}
	known, err := s.store.fileStampsUnlocked(ctx)
	if err != nil {
		return nil, err
	}
	gone := map[string]bool{}
	for _, rel := range paths {
		abs := filepath.Join(s.root, filepath.FromSlash(rel))
		info, err := os.Lstat(abs)
		switch {
		case err != nil:
			for k := range known {
				if k == rel || strings.HasPrefix(k, rel+"/") {
					gone[k] = true
				}
			}
		case info.IsDir():
			if err := s.walk(abs, known, res); err != nil {
				return nil, err
			}
		default:
			s.stamp(abs, info, known, res)
		}
	}
	res.finish(known, func(path string) bool { return gone[path] })
	return res, nil
}

// walk stamps every indexable file under dir that is not ignored.
func (s *Scanner) walk(dir string, known map[string]FileStamp, res *ScanResult) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if s.isIgnored(path) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			return nil
		}
		s.stamp(path, info, known, res)
		return nil
	})
}

// stamp records what the graph should hold for one file: the stored hash
// when the stamp matches, a fresh one otherwise. A file no parser handles,
// or one the scanner ignores, is not a file of the graph.
func (s *Scanner) stamp(path string, info os.FileInfo, known map[string]FileStamp, res *ScanResult) {
	if s.isIgnored(path) {
		return
	}
	// Keep discovery aligned with the actual parser registry. The previous
	// hand-maintained allowlist omitted React sources (.jsx/.tsx) and most
	// languages already supported by tree-sitter.
	ext := strings.ToLower(filepath.Ext(info.Name()))
	if SitterLanguageFor(ext) == nil {
		return
	}
	rel, relErr := filepath.Rel(s.root, path)
	if relErr != nil {
		return
	}
	normalizedPath := filepath.ToSlash(rel)

	stamp := FileStamp{MTimeNS: info.ModTime().UnixNano(), Size: info.Size()}
	old, seen := known[normalizedPath]
	if seen && old.MTimeNS != 0 && old.MTimeNS == stamp.MTimeNS && old.Size == stamp.Size {
		stamp.Hash = old.Hash
	} else {
		hash, hashErr := hashFile(path)
		if hashErr != nil {
			return
		}
		res.Hashed++
		stamp.Hash = hash
		if seen && old.Hash == hash {
			res.Restamp[normalizedPath] = stamp
		}
	}
	res.Stamps[normalizedPath] = stamp
}

// ack tells the change feed, if any, that the pass landed.
func (res *ScanResult) ack() {
	if res != nil && res.Ack != nil {
		res.Ack()
	}
}

// finish derives ToParse from the stamps taken and ToDelete from the known
// files that gone says are no longer on the tree.
func (res *ScanResult) finish(known map[string]FileStamp, gone func(path string) bool) {
	for path, stamp := range res.Stamps {
		old, exists := known[path]
		if !exists || old.Hash != stamp.Hash {
			res.ToParse = append(res.ToParse, path)
		}
	}
	for path := range known {
		if gone(path) {
			res.ToDelete = append(res.ToDelete, path)
		}
	}
	sort.Strings(res.ToParse)
	sort.Strings(res.ToDelete)
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
