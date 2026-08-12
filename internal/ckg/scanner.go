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

// NewScanner creates a new scanner and loads ignore files.
func NewScanner(store *Store, root string) *Scanner {
	return NewScannerWithIgnores(store, root, nil)
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

// Scan performs an incremental scan of the workspace.
// Returns a list of file paths that need parsing (new or modified)
// and a list of file paths that should be deleted from the DB.
func (s *Scanner) Scan(ctx context.Context) (toParse []string, toDelete []string, err error) {
	currentFiles := make(map[string]string) // normalized rel path -> hash

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

		hash, hashErr := hashFile(path)
		if hashErr != nil {
			return nil
		}

		rel, relErr := filepath.Rel(s.root, path)
		if relErr != nil {
			return nil
		}

		normalizedPath := filepath.ToSlash(rel)
		currentFiles[normalizedPath] = hash
		return nil
	})

	if err != nil {
		return nil, nil, err
	}

	// Compare with DB state
	dbFiles, err := s.store.GetAllFiles(ctx)
	if err != nil {
		return nil, nil, err
	}

	// Find new and modified files
	for path, hash := range currentFiles {
		dbHash, exists := dbFiles[path]
		if !exists || dbHash != hash {
			toParse = append(toParse, path)
		}
	}

	// Find deleted files
	for path := range dbFiles {
		if _, exists := currentFiles[path]; !exists {
			toDelete = append(toDelete, path)
		}
	}

	return toParse, toDelete, nil
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
