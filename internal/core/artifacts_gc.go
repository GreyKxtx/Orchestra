package core

import (
	"os"
	"path/filepath"
	"time"
)

// artifactTTL is how long staged runtime artifacts survive before startup GC
// removes them. Attachments were already sent to the LLM at message time and
// diff-preview pairs exist only to back an open VS Code diff editor, so a
// week is generous.
const artifactTTL = 7 * 24 * time.Hour

// cleanupWorkspaceArtifacts removes stale files from .orchestra staging
// directories. Best-effort, runs once per core start in a self-terminating
// goroutine; failures are silently ignored (GC must never break startup).
// apply.lock is deliberately left alone: it is flock-managed and deleting it
// under a live holder would be racy.
func cleanupWorkspaceArtifacts(workspaceRoot string) {
	cutoff := time.Now().Add(-artifactTTL)
	for _, sub := range []string{"attachments", "diff-preview"} {
		dir := filepath.Join(workspaceRoot, ".orchestra", sub)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			if info.ModTime().Before(cutoff) {
				_ = os.Remove(filepath.Join(dir, e.Name()))
			}
		}
	}
}
