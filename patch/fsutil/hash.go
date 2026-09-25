package fsutil

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

// ComputeSHA256 returns "sha256:<hex>" of data: the file_hash every external
// patch and internal op carries.
func ComputeSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ComputeProjectID derives a stable id from the project's absolute path:
// "sha256:<hex>" of the cleaned path, lower-cased on Windows where paths are
// usually case-insensitive.
func ComputeProjectID(projectRoot string) (string, error) {
	abs, err := filepath.Abs(projectRoot)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute project root: %w", err)
	}
	abs = filepath.Clean(abs)
	if runtime.GOOS == "windows" {
		abs = strings.ToLower(abs)
	}
	sum := sha256.Sum256([]byte(abs))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
