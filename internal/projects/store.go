package projects

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

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

// LoadPaths reads the remembered project paths. An absent or unreadable list is
// an empty list, never an error: a corrupt file must not stop the server from
// starting, because then the user cannot reach the UI to fix it. The next
// SavePaths overwrites it atomically.
func LoadPaths(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
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
