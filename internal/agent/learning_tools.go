package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/orchestra/orchestra/internal/lessons"
	"github.com/orchestra/orchestra/internal/playbooks"
)

func learningToolModeOK(mode Mode) bool {
	return mode == ModeArchitecture || mode == ModeOrchestra
}

func (a *Agent) handleLessonPromote(ctx context.Context, input json.RawMessage) ([]byte, error) {
	_ = ctx
	if !learningToolModeOK(a.opts.Mode) {
		return nil, fmt.Errorf("lesson_promote is available only to Dept Lead (architecture) or Orchestra Lead")
	}
	var req struct {
		Dept   string `json:"dept"`
		Note   string `json:"note"`
		Source string `json:"source"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, fmt.Errorf("lesson_promote: invalid input: %w", err)
	}
	dept := lessons.NormalizeDept(req.Dept)
	if dept == "" {
		return nil, fmt.Errorf("lesson_promote: dept is required")
	}
	root := a.tools.WorkspaceRoot()
	var rel, pendingRef string
	var err error
	if note := strings.TrimSpace(req.Note); note != "" {
		summary := clipPromoteSummary(note)
		rel, err = playbooks.DraftLocalOverlay(root, dept, note, strings.TrimSpace(req.Source), summary)
		pendingRef = "PENDING: " + summary
	} else {
		rel, pendingRef, err = playbooks.DraftFromLastPattern(root, dept)
	}
	if err != nil {
		return nil, fmt.Errorf("lesson_promote: %w", err)
	}
	lessons.ClearAntiPatternSignals(root, dept)
	resp, _ := json.Marshal(map[string]any{
		"path":         rel,
		"status":       "draft",
		"decision_ref": pendingRef,
		"message":      "Draft local overlay written. Ask the User via open_questions; the runtime auto-seals decision_ref. Then call playbook_promote (same approval covers merge).",
	})
	return resp, nil
}

func (a *Agent) handlePlaybookPromote(ctx context.Context, input json.RawMessage) ([]byte, error) {
	_ = ctx
	if !learningToolModeOK(a.opts.Mode) {
		return nil, fmt.Errorf("playbook_promote is available only to Dept Lead (architecture) or Orchestra Lead")
	}
	var req struct {
		Dept          string `json:"dept"`
		PromotionRef  string `json:"promotion_ref"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, fmt.Errorf("playbook_promote: invalid input: %w", err)
	}
	dept := lessons.NormalizeDept(req.Dept)
	ref := strings.TrimSpace(req.PromotionRef)
	root := a.tools.WorkspaceRoot()
	log := readDecisionLogRaw(root)
	if ref == "" {
		body, err := playbooks.ReadLocalOverlayBody(root, dept)
		if err != nil {
			return nil, fmt.Errorf("playbook_promote: %w", err)
		}
		ref = playbooks.ParseDecisionRef(body)
	}
	if dept == "" || ref == "" {
		return nil, fmt.Errorf("playbook_promote: dept is required; promotion_ref defaults to approved overlay decision_ref")
	}
	l2Rel, err := playbooks.MergeApprovedLocalToL2(root, dept, ref, log)
	if err != nil {
		return nil, fmt.Errorf("playbook_promote: %w", err)
	}
	resp, _ := json.Marshal(map[string]any{
		"l2_path":       l2Rel,
		"promotion_ref": ref,
		"status":        "merged",
	})
	return resp, nil
}

func clipPromoteSummary(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= 160 {
		return s
	}
	return s[:159] + "…"
}
