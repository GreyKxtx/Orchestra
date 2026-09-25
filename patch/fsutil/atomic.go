package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
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
	// Best-effort: keep Orchestra's own artifact directory private on Unix.
	// Only that directory: this helper writes elsewhere too, and it used to
	// chmod 0700 whatever directory the file happened to be in (DATA-4).
	if filepath.Base(dir) == ".orchestra" {
		_ = os.Chmod(dir, 0700)
	}

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
	// os.Rename replaces an existing target on every platform (MoveFileEx
	// with MOVEFILE_REPLACE_EXISTING on Windows). When it fails there, it is
	// usually a moment's sharing violation — an antivirus scan, a reader
	// holding the file — so it is retried before anything else.
	var renameErr error
	for attempt := 0; attempt < renameAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * renameBackoff)
		}
		if renameErr = os.Rename(tmpName, path); renameErr == nil {
			_ = os.Chmod(path, perm)
			syncDir(dir)
			return nil
		}
	}
	// The old fallback removed the target and renamed again: when that second
	// rename failed too, the target and the temp file were both gone (DATA-4,
	// the bug H9 fixed in the applier). Overwriting in place is not atomic
	// for readers, but the target never goes missing.
	if err := os.WriteFile(path, data, perm); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("failed to replace %s (rename: %v): %w", path, renameErr, err)
	}
	_ = os.Remove(tmpName)
	return nil
}

const (
	renameAttempts = 5
	renameBackoff  = 20 * time.Millisecond
)

// syncDir fsyncs the directory so the rename itself survives a power loss
// (POSIX: directory metadata is not flushed by the file's own fsync).
// Best-effort: on Windows directories cannot be opened for sync this way
// and NTFS journals metadata anyway, so errors are ignored.
func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}
