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

// ScanResult is what one walk of the workspace found.
type ScanResult struct {
	// ToParse are new or modified files; ToDelete files the graph has that
	// the tree no longer does.
	ToParse  []string
	ToDelete []string
	// Stamps is every indexable file on disk with the hash the graph should
	// hold for it.
	Stamps map[string]FileStamp
	// Restamp are unchanged files whose stored stamp is stale (a touch, a row
	// from before stamps existed): the pass records the new stamp so the next
	// walk does not hash them again.
	Restamp map[string]FileStamp
	// Hashed is how many files had to be read: their stamp was unknown or had
	// changed. An unchanged tree hashes nothing.
	Hashed int
}

// ScanChanges walks the workspace and compares it with the graph. A file
// whose mtime and size match its stored stamp is taken as unchanged without
// being read; every pass used to hash every file (DATA-3).
//
// It reads the store without locking it: the orchestrator holds the graph
// lock it needs around the call (the read lock for a refresh, the write
// lock for the warmup, which reserves it up front).
func (s *Scanner) ScanChanges(ctx context.Context) (*ScanResult, error) {
	known, err := s.store.fileStampsUnlocked(ctx)
	if err != nil {
		return nil, err
	}
	res := &ScanResult{Stamps: make(map[string]FileStamp, len(known)), Restamp: map[string]FileStamp{}}

	err = filepath.Walk(s.root, func(path string, info os.FileInfo, walkErr error) error {
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

		// Keep discovery aligned with the actual parser registry. The previous
		// hand-maintained allowlist omitted React sources (.jsx/.tsx) and most
		// languages already supported by tree-sitter.
		ext := strings.ToLower(filepath.Ext(info.Name()))
		if SitterLanguageFor(ext) == nil {
			return nil
		}

		rel, relErr := filepath.Rel(s.root, path)
		if relErr != nil {
			return nil
		}
		normalizedPath := filepath.ToSlash(rel)

		stamp := FileStamp{MTimeNS: info.ModTime().UnixNano(), Size: info.Size()}
		old, seen := known[normalizedPath]
		if seen && old.MTimeNS != 0 && old.MTimeNS == stamp.MTimeNS && old.Size == stamp.Size {
			stamp.Hash = old.Hash
		} else {
			hash, hashErr := hashFile(path)
			if hashErr != nil {
				return nil
			}
			res.Hashed++
			stamp.Hash = hash
			if seen && old.Hash == hash {
				res.Restamp[normalizedPath] = stamp
			}
		}
		res.Stamps[normalizedPath] = stamp
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Find new and modified files
	for path, stamp := range res.Stamps {
		old, exists := known[path]
		if !exists || old.Hash != stamp.Hash {
			res.ToParse = append(res.ToParse, path)
		}
	}

	// Find deleted files
	for path := range known {
		if _, exists := res.Stamps[path]; !exists {
			res.ToDelete = append(res.ToDelete, path)
		}
	}

	return res, nil
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
