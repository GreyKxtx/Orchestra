//go:build !windows

package applier

import "os"

// syncDir fsyncs the directory containing `path` so the metadata change
// from a preceding atomic rename is durable across a power loss. POSIX
// requires fsync on the parent directory for rename durability; without
// it, a crash between rename and dir-fsync can leave the new name absent
// on next boot. No-op on Windows where the FS journal handles this.
// M10 in audit ledger.
func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
