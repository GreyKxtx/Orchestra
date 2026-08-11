package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/agent/guard"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/internal/tools/exec"
	"github.com/orchestra/orchestra/llm"
)

const workerVerifyBuildTimeout = 90 * time.Second

// WorkerVerifyCheck is one deterministic post-worker check.
type WorkerVerifyCheck struct {
	Name   string `json:"name"`
	Path   string `json:"path,omitempty"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
	Skip   bool   `json:"skip,omitempty"`
}

// WorkerVerifyReport aggregates verifier output for the Lead.
type WorkerVerifyReport struct {
	Passed bool                `json:"passed"`
	Checks []WorkerVerifyCheck `json:"checks"`
}

func (r WorkerVerifyReport) Summary() string {
	if r.Passed {
		return "verification passed"
	}
	var b strings.Builder
	b.WriteString("verification failed:")
	for _, c := range r.Checks {
		if c.Skip || c.OK {
			continue
		}
		fmt.Fprintf(&b, "\n- %s", c.Name)
		if c.Path != "" {
			fmt.Fprintf(&b, " (%s)", c.Path)
		}
		if c.Detail != "" {
			fmt.Fprintf(&b, ": %s", strings.TrimSpace(c.Detail))
		}
	}
	return strings.TrimSpace(b.String())
}

// ParseWorkerTaskResult extracts status and primary path from task_result content.
func ParseWorkerTaskResult(raw string) (status, path string) {
	raw = strings.TrimSpace(raw)
	if raw == "" || !json.Valid([]byte(raw)) {
		return "", ""
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return "", ""
	}
	if s, ok := m["status"].(string); ok {
		status = strings.ToLower(strings.TrimSpace(s))
	}
	if p, ok := m["path"].(string); ok {
		path = strings.TrimSpace(p)
	}
	return status, path
}

func workerTaskResultSuccess(raw string) bool {
	st, _ := ParseWorkerTaskResult(raw)
	if st == "" || st == "success" || st == "ok" || st == "done" {
		return true
	}
	return false
}

// CollectEditedPaths returns unique relative paths touched by edit/write in history.
func CollectEditedPaths(hist []llm.Message, primaryPath string) []string {
	seen := make(map[string]struct{})
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		p = filepath.ToSlash(p)
		p = strings.TrimPrefix(p, "./")
		if _, ok := seen[p]; !ok {
			seen[p] = struct{}{}
		}
	}
	for _, m := range hist {
		if m.Role != llm.RoleAssistant {
			continue
		}
		for _, tc := range m.ToolCalls {
			name := strings.ToLower(strings.TrimSpace(tc.Function.Name))
			if name != "edit" && name != "write" {
				continue
			}
			add(guard.ExtractWriteOrEditPath(json.RawMessage(tc.Function.Arguments)))
		}
	}
	add(primaryPath)
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	return out
}

// VerifyWorkerOutcome runs deterministic checks before Lead sees worker success.
func VerifyWorkerOutcome(ctx context.Context, runner *tools.Runner, paths []string) WorkerVerifyReport {
	if len(paths) == 0 {
		return WorkerVerifyReport{Passed: true}
	}
	var checks []WorkerVerifyCheck
	allOK := true
	for _, p := range paths {
		c := verifyWorkerLSP(ctx, runner, p)
		checks = append(checks, c)
		if !c.Skip && !c.OK {
			allOK = false
		}
	}
	if runner != nil && !runner.DryRun() {
		for _, pkg := range goBuildPackages(paths) {
			c := verifyWorkerGoBuild(ctx, runner.WorkspaceRoot(), pkg)
			checks = append(checks, c)
			if !c.Skip && !c.OK {
				allOK = false
			}
		}
	} else if runner != nil && hasGoFile(paths) {
		checks = append(checks, WorkerVerifyCheck{
			Name:   "go_build",
			OK:     true,
			Skip:   true,
			Detail: "skipped in dry-run (staging overlay; LSP gate covers compile errors)",
		})
	}
	return WorkerVerifyReport{Passed: allOK, Checks: checks}
}

func verifyWorkerLSP(ctx context.Context, runner *tools.Runner, relPath string) WorkerVerifyCheck {
	check := WorkerVerifyCheck{Name: "lsp", Path: relPath, OK: true}
	if runner == nil {
		check.Skip = true
		check.Detail = "no tool runner"
		return check
	}
	resp, err := runner.LSPDiagnostics(ctx, tools.LSPDiagnosticsRequest{Path: relPath})
	if err != nil {
		check.Skip = true
		check.Detail = err.Error()
		return check
	}
	var errs []string
	for _, d := range resp.Diagnostics {
		if d.Severity == "error" {
			errs = append(errs, fmt.Sprintf("line %d:%d: %s", d.StartLine, d.StartCol, d.Message))
		}
	}
	if len(errs) > 0 {
		check.OK = false
		check.Detail = strings.Join(errs, "; ")
		if len(check.Detail) > 400 {
			check.Detail = check.Detail[:400] + "..."
		}
	}
	return check
}

func verifyWorkerGoBuild(ctx context.Context, root, pkg string) WorkerVerifyCheck {
	check := WorkerVerifyCheck{Name: "go_build", Path: pkg, OK: true}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		check.Skip = true
		check.Detail = "no go.mod"
		return check
	}
	resp, err := exec.Run(ctx, root, workerVerifyBuildTimeout, 32*1024, exec.RunRequest{
		Command:   "go",
		Args:      []string{"build", "-o", goBuildDevNull(), pkg},
		TimeoutMS: int(workerVerifyBuildTimeout / time.Millisecond),
	})
	if err != nil {
		check.OK = false
		check.Detail = err.Error()
		return check
	}
	if resp != nil && resp.ExitCode != 0 {
		check.OK = false
		out := strings.TrimSpace(resp.Stderr)
		if out == "" {
			out = strings.TrimSpace(resp.Stdout)
		}
		check.Detail = out
		if len(check.Detail) > 400 {
			check.Detail = check.Detail[:400] + "..."
		}
	}
	return check
}

func goBuildDevNull() string {
	if filepath.Separator == '\\' {
		return "NUL"
	}
	return os.DevNull
}

func hasGoFile(paths []string) bool {
	for _, p := range paths {
		if strings.HasSuffix(strings.ToLower(p), ".go") {
			return true
		}
	}
	return false
}

func goBuildPackages(paths []string) []string {
	seen := make(map[string]struct{})
	for _, p := range paths {
		if !strings.HasSuffix(strings.ToLower(p), ".go") {
			continue
		}
		dir := filepath.ToSlash(filepath.Dir(p))
		dir = strings.TrimPrefix(dir, "./")
		pkg := "."
		if dir != "" && dir != "." {
			pkg = "./" + dir
		}
		seen[pkg] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for pkg := range seen {
		out = append(out, pkg)
	}
	return out
}

func formatWorkerVerifyRetryPrompt(failure string) string {
	return "\n\n--- VERIFICATION FAILED (system) ---\n" +
		strings.TrimSpace(failure) +
		"\nFix the issues above, then call task_result again with status=success."
}

// wrapWorkerVerifyFailure formats a compact Lead-facing payload.
func wrapWorkerVerifyFailure(workerResult string, report WorkerVerifyReport) string {
	payload := map[string]any{
		"status":              "verification_failed",
		"worker_result":       parseJSONOrString(workerResult),
		"verification":        report,
		"suggestion_for_lead": "Replan, spawn debug child, or issue a narrower WorkOrder with fix instructions.",
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return workerResult + "\n\n" + report.Summary()
	}
	return string(b)
}

func appendWorkerVerifyPassed(workerResult string, report WorkerVerifyReport) string {
	payload := map[string]any{
		"status":         "verified_success",
		"worker_result":  parseJSONOrString(workerResult),
		"verification":   report,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return workerResult
	}
	return string(b)
}

func parseJSONOrString(raw string) any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if json.Valid([]byte(raw)) {
		var v any
		if json.Unmarshal([]byte(raw), &v) == nil {
			return v
		}
	}
	return raw
}

func (r *TaskRunner) resolvedWorkerLLMVerifyEnabled() bool {
	if r.child.WorkerLLMVerifyEnabled == nil {
		return false
	}
	return *r.child.WorkerLLMVerifyEnabled
}

// VerifierResultPassed reports whether task_result content contains a PASSED marker
// without a FAILED marker (goal-backward verifier contract).
func VerifierResultPassed(content string) bool {
	upper := strings.ToUpper(content)
	if strings.Contains(upper, "## VERIFICATION FAILED") {
		return false
	}
	return strings.Contains(upper, "## VERIFICATION PASSED")
}

func formatLLMVerifierPrompt(childGoal, workerResult string, paths []string, detReport WorkerVerifyReport) string {
	var b strings.Builder
	b.WriteString("Verify the worker outcome against acceptance criteria (goal-backward, independent checks).\n\n")
	b.WriteString("## WorkOrder / task\n")
	b.WriteString(strings.TrimSpace(childGoal))
	b.WriteString("\n\n## Worker task_result\n")
	b.WriteString(strings.TrimSpace(workerResult))
	if len(paths) > 0 {
		b.WriteString("\n\n## Edited paths\n")
		for _, p := range paths {
			fmt.Fprintf(&b, "- %s\n", p)
		}
	}
	if len(detReport.Checks) > 0 {
		b.WriteString("\n\n## Deterministic checks (already passed)\n")
		if raw, err := json.MarshalIndent(detReport, "", "  "); err == nil {
			b.Write(raw)
		}
	}
	b.WriteString("\n\nFinish with ## VERIFICATION PASSED or ## VERIFICATION FAILED in task_result.")
	return b.String()
}

func appendWorkerLLMVerifyPassed(workerResult string, detReport WorkerVerifyReport, llmResult string) string {
	payload := map[string]any{
		"status":                     "verified_success",
		"worker_result":              parseJSONOrString(workerResult),
		"verification":               detReport,
		"llm_verified":               true,
		"llm_verifier_result":        parseJSONOrString(llmResult),
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return workerResult
	}
	return string(b)
}

func wrapWorkerLLMVerifyFailure(workerResult string, detReport WorkerVerifyReport, llmResult string) string {
	payload := map[string]any{
		"status":                     "llm_verification_failed",
		"worker_result":              parseJSONOrString(workerResult),
		"verification":               detReport,
		"llm_verifier_result":        parseJSONOrString(llmResult),
		"suggestion_for_lead":        "Replan, spawn debug child, or issue a narrower WorkOrder with fix instructions.",
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return workerResult + "\n\nLLM verification failed"
	}
	return string(b)
}

func (r *TaskRunner) runInlineLLMVerifier(
	ctx context.Context,
	client llm.Client,
	workerOpts agent.Options,
	prompt string,
) (string, error) {
	maxPrompt := r.child.MaxPromptBytes
	if maxPrompt <= 0 {
		maxPrompt = 64 * 1024
	}
	maxSteps := r.child.MaxStepsCap
	if maxSteps <= 0 {
		maxSteps = DefaultChildMaxSteps
	}
	opts := agent.Options{
		MaxSteps:               maxSteps,
		MaxPromptBytes:         maxPrompt,
		CompactThresholdPct:    r.child.CompactThresholdPct,
		ModelContextTokens:     r.child.ModelContextTokens,
		CompletionMaxTokens:    r.child.CompletionMaxTokens,
		ToolDigestBytes:        r.child.ToolDigestBytes,
		HistoryPruneKeepRecent: r.child.HistoryPruneKeepRecent,
		LLMStepTimeout:         r.child.LLMStepTimeout,
		CustomTools:            childToolsForSubagent("verifier", r.child.Caps),
		Mode:                   agent.ModeVerifier,
		IsChild:                true,
		UsageTracker:           r.child.UsageTracker,
		ProviderLabel:          workerOpts.ProviderLabel,
		ModelLabel:             workerOpts.ModelLabel,
		AllowExec:              r.child.Caps.Exec,
		SkipMemoryInject:       true,
	}
	if r.child.OnChildEvent != nil {
		opts.OnEvent = r.child.OnChildEvent
	}
	ag, err := agent.New(client, r.validator, r.toolRunner, opts)
	if err != nil {
		return "", err
	}
	_, res, runErr := ag.Run(ctx, nil, prompt)
	if runErr != nil {
		return "", runErr
	}
	if res == nil {
		return "", fmt.Errorf("verifier returned no result")
	}
	return res.SubtaskResult, nil
}

func (r *TaskRunner) resolvedWorkerVerifyEnabled() bool {
	if r.child.WorkerVerifyEnabled == nil {
		return true
	}
	return *r.child.WorkerVerifyEnabled
}

func (r *TaskRunner) resolvedMaxWorkerVerifyRetries() int {
	if r.child.MaxWorkerVerifyRetries <= 0 {
		return 1
	}
	return r.child.MaxWorkerVerifyRetries
}

// runWorkerWithVerification executes worker agent rounds with post-run deterministic checks.
func (r *TaskRunner) runWorkerWithVerification(
	ctx context.Context,
	client llm.Client,
	opts agent.Options,
	childGoal string,
) ([]llm.Message, *agent.Result, error) {
	maxRounds := 1
	if r.resolvedWorkerVerifyEnabled() {
		maxRounds = 1 + r.resolvedMaxWorkerVerifyRetries()
	}
	var (
		hist       []llm.Message
		res        *agent.Result
		runErr     error
		verifyFail string
	)
	for round := 0; round < maxRounds; round++ {
		runGoal := childGoal
		if round > 0 && verifyFail != "" {
			runGoal = childGoal + formatWorkerVerifyRetryPrompt(verifyFail)
		}
		ag, err := agent.New(client, r.validator, r.toolRunner, opts)
		if err != nil {
			return nil, nil, err
		}
		hist, res, runErr = ag.Run(ctx, nil, runGoal)
		if runErr != nil {
			return hist, res, runErr
		}
		taskResult := ""
		if res != nil {
			taskResult = res.SubtaskResult
		}
		if !r.resolvedWorkerVerifyEnabled() || !workerTaskResultSuccess(taskResult) {
			return hist, res, nil
		}
		_, primaryPath := ParseWorkerTaskResult(taskResult)
		paths := CollectEditedPaths(hist, primaryPath)
		report := VerifyWorkerOutcome(ctx, r.toolRunner, paths)
		if report.Passed {
			if r.resolvedWorkerLLMVerifyEnabled() {
				verifyPrompt := formatLLMVerifierPrompt(childGoal, taskResult, paths, report)
				llmResult, verifyErr := r.runInlineLLMVerifier(ctx, client, opts, verifyPrompt)
				if verifyErr != nil {
					if res != nil {
						res.SubtaskResult = wrapWorkerLLMVerifyFailure(taskResult, report, verifyErr.Error())
					}
					return hist, res, nil
				}
				if VerifierResultPassed(llmResult) {
					if res != nil {
						res.SubtaskResult = appendWorkerLLMVerifyPassed(taskResult, report, llmResult)
					}
					return hist, res, nil
				}
				if res != nil {
					res.SubtaskResult = wrapWorkerLLMVerifyFailure(taskResult, report, llmResult)
				}
				return hist, res, nil
			}
			if res != nil {
				res.SubtaskResult = appendWorkerVerifyPassed(taskResult, report)
			}
			return hist, res, nil
		}
		verifyFail = report.Summary()
		if round == maxRounds-1 {
			if res != nil {
				res.SubtaskResult = wrapWorkerVerifyFailure(taskResult, report)
			}
			return hist, res, nil
		}
	}
	return hist, res, runErr
}
