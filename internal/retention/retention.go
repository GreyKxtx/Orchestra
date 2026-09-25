// Package retention bounds the files Orchestra keeps: the run logs, the
// exported patches, the session snapshots (audit DATA-11). It is a leaf so
// every store can call it.
package retention

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// PruneFiles removes the oldest regular files in dir whose name ends with
// suffix so that at most keep remain. Orchestra names these files by their
// UTC timestamp, so name order is age order; a missing directory is nothing
// to prune. keep < 0 keeps everything. Best-effort: a file that cannot be
// removed now is removed by a later call. Returns how many were removed.
func PruneFiles(dir, suffix string, keep int) int {
	if keep < 0 {
		return 0
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), suffix) {
			names = append(names, e.Name())
		}
	}
	if len(names) <= keep {
		return 0
	}
	sort.Strings(names)
	removed := 0
	for _, n := range names[:len(names)-keep] {
		if err := os.Remove(filepath.Join(dir, n)); err == nil {
			removed++
		}
	}
	return removed
}
