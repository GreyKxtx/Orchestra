package core

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/orchestra/orchestra/internal/agent"
	coresession "github.com/orchestra/orchestra/internal/core/session"
	"github.com/orchestra/orchestra/internal/plan"
)

// resolvePlanPath returns the plan markdown path for an agent run.
// Session-backed plan mode uses a stable .orchestra/plans/<sessionID>.md path.
func resolvePlanPath(mode, sessionPlanPath, sessionID string) string {
	if p := strings.TrimSpace(sessionPlanPath); p != "" {
		return plan.NormalizeRelPath(p)
	}
	if !needsPlanPath(agent.Mode(mode)) {
		return ""
	}
	if strings.TrimSpace(sessionID) != "" {
		return plan.SessionRelPath(sessionID)
	}
	return plan.AdHocRelPath()
}

func needsPlanPath(m agent.Mode) bool {
	switch m {
	case agent.ModePlan, agent.ModeArchitecture, agent.ModeOrchestra:
		return true
	default:
		return false
	}
}

// sessionPlanPathLocked assigns or returns the session plan path.
// Caller must hold sess.Lock().
func sessionPlanPathLocked(sess *coresession.Session, mode string) string {
	if p := sess.PlanPath(); p != "" {
		return p
	}
	if !needsPlanPath(agent.Mode(mode)) {
		return ""
	}
	p := plan.SessionRelPath(sess.ID)
	sess.SetPlanPath(p)
	return p
}

// writtenPlanPath returns relPath only when a plan actually exists there.
//
// resolvePlanPath answers "where would a plan go", which every plan-mode run
// has. Whether a plan was WRITTEN is a different question, and the run result
// is the place that has to answer the second one: its consumers open the file
// or check it, and a path to a file that was never created makes a run that
// did not plan look like a run whose plan went missing.
func writtenPlanPath(workspaceRoot, relPath string) string {
	rel := strings.TrimSpace(relPath)
	if rel == "" {
		return ""
	}
	abs := filepath.Join(workspaceRoot, filepath.FromSlash(rel))
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() || info.Size() == 0 {
		return ""
	}
	return rel
}
