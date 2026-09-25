package applier

import (
	"fmt"
	"path/filepath"

	"github.com/orchestra/orchestra/patch/fsutil"
)

// acquireProjectLock takes the exclusive project lock
// `<project>/.orchestra/apply.lock` (flock on POSIX, LockFileEx on Windows)
// and returns its release. It blocks until the lock is free. H9 in audit
// ledger.
//
// Cross-process safety: a second Orchestra apply against the same project
// waits here until the first finishes, so concurrent .orchestra.bak writes
// can't clobber each other.
func acquireProjectLock(projectRoot string) (release func(), err error) {
	release, err = fsutil.LockFile(filepath.Join(projectRoot, ".orchestra", "apply.lock"))
	if err != nil {
		return nil, fmt.Errorf("apply lock: %w", err)
	}
	return release, nil
}
