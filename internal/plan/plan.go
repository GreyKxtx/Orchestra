// Package plan helpers for plan-mode artifact paths and write guards.
package plan

import (
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const legacyRelPath = ".orchestra/plan.md"

var safeID = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

// SessionRelPath returns the per-session plan markdown path (relative to project root).
func SessionRelPath(sessionID string) string {
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return AdHocRelPath()
	}
	safe := safeID.ReplaceAllString(id, "-")
	if safe == "" {
		safe = "session"
	}
	return filepath.ToSlash(filepath.Join(".orchestra", "plans", safe+".md"))
}

// AdHocRelPath returns a timestamped plan path for one-shot runs without a session id.
func AdHocRelPath() string {
	ts := time.Now().Format("20060102-150405")
	return filepath.ToSlash(filepath.Join(".orchestra", "plans", ts+"-plan.md"))
}

// NormalizeRelPath cleans a project-relative path for comparison.
func NormalizeRelPath(path string) string {
	p := strings.TrimSpace(path)
	if p == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(p))
}

// IsWritablePath reports whether path may be written in plan mode.
// assigned is the session's canonical plan file; legacy .orchestra/plan.md is always allowed.
func IsWritablePath(path, assigned string) bool {
	p := NormalizeRelPath(path)
	if p == "" {
		return false
	}
	if assigned != "" && p == NormalizeRelPath(assigned) {
		return true
	}
	if p == legacyRelPath {
		return true
	}
	if strings.HasPrefix(p, ".orchestra/plans/") && strings.HasSuffix(p, ".md") {
		return true
	}
	return false
}
