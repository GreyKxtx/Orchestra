package ckg

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
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
	s := &Scanner{
		store:   store,
		root:    root,
		ignores: []string{".git", "vendor", "node_modules", "dist", "build", ".orchestra"},
	}
	s.loadIgnoreFile(".gitignore")
	s.loadIgnoreFile(".orchestraignore")
	return s
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
		// Basic cleanup for simple match
		line = strings.TrimPrefix(line, "/")
		line = strings.TrimSuffix(line, "/")
		s.ignores = append(s.ignores, line)
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

	parts := strings.Split(rel, string(filepath.Separator))
	for _, part := range parts {
		for _, ignore := range s.ignores {
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

var supportedExtensions = map[string]bool{
	".go":   true,
	".py":   true,
	".ts":   true,
	".js":   true,
	".java": true,
	".c":    true,
	".cpp":  true,
	".rs":   true,
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
		
		ext := filepath.Ext(info.Name())
		if !supportedExtensions[ext] {
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
