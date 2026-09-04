package working

import (
	"os"
	"path/filepath"
	"strings"
)

// splitPersistedDigests extracts digest bodies from a .turns.md file, oldest first.
func splitPersistedDigests(data string) []string {
	var out []string
	for _, p := range strings.Split(data, "\n---\n") {
		p = strings.TrimSpace(p)
		if strings.Contains(p, "[turn_digest]") {
			out = append(out, p)
		}
	}
	return out
}

func turnDigestPath(workspaceRoot, sessionID string) string {
	return filepath.Join(workspaceRoot, ".orchestra", "memory", "sessions", sessionID+".turns.md")
}

func loadTurnDigests(workspaceRoot, sessionID string) []string {
	if strings.TrimSpace(sessionID) == "" {
		return nil
	}
	data, err := os.ReadFile(turnDigestPath(workspaceRoot, sessionID))
	if err != nil {
		return nil
	}
	return splitPersistedDigests(string(data))
}

// LastTurnDigest returns the most recently persisted digest for a session, or
// "" when the session has none.
func LastTurnDigest(workspaceRoot, sessionID string) string {
	digests := loadTurnDigests(workspaceRoot, sessionID)
	if len(digests) == 0 {
		return ""
	}
	return digests[len(digests)-1]
}

// MemoryNoteFromDigest condenses a turn digest into a durable project-memory
// note: what the turn was for, what it finished, and which files it touched.
//
// This is the rule-based path to cross-session memory. The LLM summary is
// better prose, but it needs a working endpoint — and when the endpoint is
// down is exactly when a run is worth remembering. Turn digests are already
// on disk by then, so a note costs nothing and cannot fail.
//
// Returns "" unless the turn actually changed something. done: is the signal —
// ObserveTool fills it from write/edit/bash only, while files: also collects
// everything read, grepped or explored. Gating on files: would put every
// look-around turn in agent.md, which is the noise this replaced.
func MemoryNoteFromDigest(digest string) string {
	var goal, done, files string
	for _, line := range strings.Split(strings.TrimSpace(digest), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "goal:"):
			goal = strings.TrimSpace(strings.TrimPrefix(line, "goal:"))
		case strings.HasPrefix(line, "done:"):
			done = strings.TrimSpace(strings.TrimPrefix(line, "done:"))
		case strings.HasPrefix(line, "files:"):
			files = strings.TrimSpace(strings.TrimPrefix(line, "files:"))
		}
	}
	if done == "" {
		return ""
	}
	var b strings.Builder
	if goal != "" {
		b.WriteString("goal: " + goal + "\n")
	}
	if done != "" {
		b.WriteString("done: " + done + "\n")
	}
	if files != "" {
		b.WriteString("files: " + files + "\n")
	}
	return strings.TrimSpace(b.String())
}
