package tasks

import (
	"context"
	"encoding/json"
	"sort"
)

// IntegrationReport is the verdict on several workers' edits taken together.
type IntegrationReport struct {
	Status  string              `json:"status"` // passed | failed
	Workers int                 `json:"workers"`
	Files   []string            `json:"files"`
	Summary string              `json:"summary"`
	Failed  []WorkerVerifyCheck `json:"failed_checks,omitempty"`
	Skipped []string            `json:"skipped,omitempty"`
}

// integrationVerify runs the worker DoD — LSP on every file, build and tests
// of every touched package, frontend typecheck — over the union of what the
// successful workers among entries changed. Each worker already passed it
// alone; this is the check that the pieces fit (orchestra-vs-subagents.md,
// "Интеграция после fan-out": twenty green workers that do not build
// together). Nil when fewer than two workers changed files, or when worker
// verification is switched off.
func (r *TaskRunner) integrationVerify(ctx context.Context, entries []*taskEntry) json.RawMessage {
	seen := map[string]bool{}
	var files []string
	workers := 0
	r.mu.Lock()
	for _, e := range entries {
		if e == nil || !e.worker || e.status != "done" || len(e.edited) == 0 {
			continue
		}
		workers++
		for _, p := range e.edited {
			if !seen[p] {
				seen[p] = true
				files = append(files, p)
			}
		}
	}
	r.mu.Unlock()
	if workers < 2 || !r.resolvedWorkerVerifyEnabled() || ctx.Err() != nil {
		return nil
	}
	sort.Strings(files)
	report := VerifyWorkerOutcome(ctx, r.toolRunner, files, r.resolvedWorkerVerifyOptions())
	out := IntegrationReport{Status: "passed", Workers: workers, Files: files, Summary: report.Summary()}
	if !report.Passed {
		out.Status = "failed"
	}
	for _, c := range report.Checks {
		switch {
		case c.Skip:
			out.Skipped = append(out.Skipped, c.Name+": "+c.Detail)
		case !c.OK:
			out.Failed = append(out.Failed, c)
		}
	}
	if r.child.NotifyAgentEvent != nil {
		r.child.NotifyAgentEvent(map[string]any{
			"type":    "integration_verify",
			"status":  out.Status,
			"workers": workers,
			"files":   len(files),
			"summary": out.Summary,
		})
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil
	}
	return raw
}
