package trajectory

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/orchestra/orchestra/internal/sessionfile"
)

// A one-shot agent.run has no session to keep its log beside, so it gets a
// file of its own under .orchestra/runs, named by its turn id: the same
// events a session records — turn boundaries, every agent/event including
// the child_started / child_done pairs that give each subagent task its
// parent and depth — so a run's delegation tree can be read back after it
// ended. A session's log is its history and lives as long as the session;
// run logs are for looking back, and only the most recent MaxRunLogs are kept.

// MaxRunLogs is how many run logs .orchestra/runs keeps.
const MaxRunLogs = 50

// RunPath returns the log's location for a run (a turn_id).
func RunPath(workspaceRoot, runID string) string {
	return filepath.Join(workspaceRoot, ".orchestra", "runs", runID+".events.jsonl")
}

// NewRunWriter opens a run's log, first removing the oldest logs so that
// MaxRunLogs remain with this one.
func NewRunWriter(workspaceRoot, runID string) (*Writer, error) {
	if workspaceRoot == "" || runID == "" {
		return nil, fmt.Errorf("trajectory: workspace_root and run id required")
	}
	if err := sessionfile.CheckID(runID); err != nil {
		return nil, err
	}
	p := RunPath(workspaceRoot, runID)
	pruneRunLogs(filepath.Dir(p), MaxRunLogs-1)
	return openWriter(p)
}

// ReadRun returns a run's events; recorded is false when it has no log.
func ReadRun(workspaceRoot, runID string) (events []Event, recorded bool, err error) {
	if err := sessionfile.CheckID(runID); err != nil {
		return nil, false, err
	}
	return readFile(RunPath(workspaceRoot, runID))
}

// pruneRunLogs removes the oldest logs in dir so that at most keep remain.
// Turn ids begin with their UTC timestamp, so name order is age order.
// Best-effort: a log that cannot be removed now is removed by a later run.
func pruneRunLogs(dir string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".events.jsonl") {
			names = append(names, e.Name())
		}
	}
	if len(names) <= keep {
		return
	}
	sort.Strings(names)
	for _, n := range names[:len(names)-keep] {
		_ = os.Remove(filepath.Join(dir, n))
	}
}
