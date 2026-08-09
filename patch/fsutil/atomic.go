package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
)

// AtomicWriteFile writes data to path atomically: temp file in the same
// directory, fsync, then rename over the target. Used for .orchestra/
// artifacts (plan.json, diff.txt, last_run.jsonl, discovery files)
// where a half-written file would corrupt a subsequent --from-plan
// replay or confuse a watching client.
//
// H4 in architecture audit: this helper lived in internal/daemon for
// historical reasons even though every non-daemon caller (apply.go,
// core.go) imported the daemon package just to reach it. Moved here
// so daemon can shrink to its actual scope.
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	// Best-effort: lock down .orchestra/ directory on Unix. No-op when
	// dir is something other than .orchestra/ but cheap enough to keep
	// in the generic helper.
	_ = os.Chmod(dir, 0700)

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
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
	if err := os.Rename(tmpName, path); err == nil {
		_ = os.Chmod(path, perm)
		return nil
	}
	// Windows: os.Rename fails if destination exists. Try remove + rename.
	_ = os.Remove(path)
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}
	_ = os.Chmod(path, perm)
	return nil
}
