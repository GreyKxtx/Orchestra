package core

import (
	"strings"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/plan"
	coresession "github.com/orchestra/orchestra/internal/core/session"
)

// resolvePlanPath returns the plan markdown path for an agent run.
// Session-backed plan mode uses a stable .orchestra/plans/<sessionID>.md path.
func resolvePlanPath(mode, sessionPlanPath, sessionID string) string {
	if p := strings.TrimSpace(sessionPlanPath); p != "" {
		return plan.NormalizeRelPath(p)
	}
	if agent.Mode(mode) != agent.ModePlan {
		return ""
	}
	if strings.TrimSpace(sessionID) != "" {
		return plan.SessionRelPath(sessionID)
	}
	return plan.AdHocRelPath()
}

// sessionPlanPathLocked assigns or returns the session plan path.
// Caller must hold sess.Lock().
func sessionPlanPathLocked(sess *coresession.Session, mode string) string {
	if p := sess.PlanPath(); p != "" {
		return p
	}
	if agent.Mode(mode) != agent.ModePlan {
		return ""
	}
	p := plan.SessionRelPath(sess.ID)
	sess.SetPlanPath(p)
	return p
}
