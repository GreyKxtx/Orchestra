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
	promptpkg "github.com/orchestra/orchestra/internal/prompt"
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

// WorkerVerifyOptions widens the deterministic DoD beyond LSP + build
// (spec §5.4, checklist 23): affected Go tests and frontend typecheck.
type WorkerVerifyOptions struct {
	AffectedTests     bool
	FrontendTypecheck bool
}

// VerifyWorkerOutcome runs deterministic checks before Lead sees worker success.
func VerifyWorkerOutcome(ctx context.Context, runner *tools.Runner, paths []string, opts WorkerVerifyOptions) WorkerVerifyReport {
	if len(paths) == 0 {
		return WorkerVerifyReport{Passed: true}
	}
	var checks []WorkerVerifyCheck
	allOK := true
	record := func(c WorkerVerifyCheck) {
		checks = append(checks, c)
		if !c.Skip && !c.OK {
			allOK = false
		}
	}
	for _, p := range paths {
		record(verifyWorkerLSP(ctx, runner, p))
	}
	if runner != nil && !runner.DryRun() {
		root := runner.WorkspaceRoot()
		for _, pkg := range goBuildPackages(paths) {
			record(verifyWorkerGoBuild(ctx, root, pkg))
			if opts.AffectedTests {
				record(verifyWorkerGoTest(ctx, root, pkg))
			}
		}
		if opts.FrontendTypecheck && hasFrontendFile(paths) {
			record(verifyWorkerFrontendTypecheck(ctx, root))
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

// verifyWorkerGoTest runs the tests of one edited package (affected tests,
// checklist 23). Whole-repo test runs stay on the Platform stage; the worker
// gate covers only the packages the worker touched.
func verifyWorkerGoTest(ctx context.Context, root, pkg string) WorkerVerifyCheck {
	check := WorkerVerifyCheck{Name: "go_test", Path: pkg, OK: true}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		check.Skip = true
		check.Detail = "no go.mod"
		return check
	}
	resp, err := exec.Run(ctx, root, workerVerifyBuildTimeout, 32*1024, exec.RunRequest{
		Command:   "go",
		Args:      []string{"test", "-count=1", pkg},
		TimeoutMS: int(workerVerifyBuildTimeout / time.Millisecond),
	})
	if err != nil {
		check.OK = false
		check.Detail = err.Error()
		return check
	}
	if resp != nil && resp.ExitCode != 0 {
		check.OK = false
		check.Detail = compactOutput(resp.Stdout, resp.Stderr)
	}
	return check
}

// verifyWorkerFrontendTypecheck runs `tsc --noEmit` when the worker touched
// frontend sources and the project has a tsconfig.json. A missing toolchain
// downgrades to skip — the gate must not fail on environments without node.
func verifyWorkerFrontendTypecheck(ctx context.Context, root string) WorkerVerifyCheck {
	check := WorkerVerifyCheck{Name: "frontend_typecheck", OK: true}
	if _, err := os.Stat(filepath.Join(root, "tsconfig.json")); err != nil {
		check.Skip = true
		check.Detail = "no tsconfig.json"
		return check
	}
	resp, err := exec.Run(ctx, root, workerVerifyBuildTimeout, 32*1024, exec.RunRequest{
		Command:   "npx",
		Args:      []string{"--no-install", "tsc", "--noEmit"},
		TimeoutMS: int(workerVerifyBuildTimeout / time.Millisecond),
	})
	if err != nil {
		check.Skip = true
		check.Detail = "typecheck toolchain unavailable: " + err.Error()
		return check
	}
	if resp != nil && resp.ExitCode != 0 {
		check.OK = false
		check.Detail = compactOutput(resp.Stdout, resp.Stderr)
	}
	return check
}

func hasFrontendFile(paths []string) bool {
	for _, p := range paths {
		switch strings.ToLower(filepath.Ext(p)) {
		case ".ts", ".tsx", ".mts", ".cts", ".vue", ".svelte":
			return true
		}
	}
	return false
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
		"status":        "verified_success",
		"worker_result": parseJSONOrString(workerResult),
		"verification":  report,
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
		"status":              "verified_success",
		"worker_result":       parseJSONOrString(workerResult),
		"verification":        detReport,
		"llm_verified":        true,
		"llm_verifier_result": parseJSONOrString(llmResult),
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return workerResult
	}
	return string(b)
}

// wrapWorkerNeedsReview is spec §5.4 arbitration rule 2: executable
// acceptance checks are green but the LLM verifier is red — a criteria
// dispute, not a proven defect. The Lead arbitrates.
func wrapWorkerNeedsReview(workerResult string, detReport WorkerVerifyReport, llmResult string) string {
	payload := map[string]any{
		"status":              "needs_review",
		"worker_result":       parseJSONOrString(workerResult),
		"verification":        detReport,
		"llm_verifier_result": parseJSONOrString(llmResult),
		"suggestion_for_lead": "acceptance_checks are green but the LLM verifier disagrees: decide whether to fix the code or fix the acceptance criterion.",
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return workerResult + "\n\nneeds_review: checks green, LLM verifier red"
	}
	return string(b)
}

func wrapWorkerLLMVerifyFailure(workerResult string, detReport WorkerVerifyReport, llmResult string) string {
	payload := map[string]any{
		"status":              "llm_verification_failed",
		"worker_result":       parseJSONOrString(workerResult),
		"verification":        detReport,
		"llm_verifier_result": parseJSONOrString(llmResult),
		"suggestion_for_lead": "Replan, spawn debug child, or issue a narrower WorkOrder with fix instructions.",
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
		PromptFamily:           promptpkg.ResolvePromptFamily("", workerOpts.ModelLabel),
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

// resolvedWorkerVerifyOptions: affected tests default to true (spec §5.4),
// frontend typecheck too — both self-gate on project shape (go.mod /
// tsconfig.json) and dry-run.
func (r *TaskRunner) resolvedWorkerVerifyOptions() WorkerVerifyOptions {
	opts := WorkerVerifyOptions{AffectedTests: true, FrontendTypecheck: true}
	if r.child.WorkerVerifyAffectedTests != nil {
		opts.AffectedTests = *r.child.WorkerVerifyAffectedTests
	}
	if r.child.WorkerVerifyFrontendTypecheck != nil {
		opts.FrontendTypecheck = *r.child.WorkerVerifyFrontendTypecheck
	}
	return opts
}

func (r *TaskRunner) resolvedMaxWorkerVerifyRetries() int {
	if r.child.MaxWorkerVerifyRetries <= 0 {
		return 1
	}
	return r.child.MaxWorkerVerifyRetries
}

// runWorkerWithVerification executes worker rounds on the assigned tier and,
// when tier escalation is enabled (spec §5.5) and verification keeps failing,
// re-runs the same WorkOrder on the escalation tier. Escalation triggers only
// on deterministic verification_failed — never on malformed task_result JSON
// (schema repair-retry handles format issues at the tool-call layer).
func (r *TaskRunner) runWorkerWithVerification(
	ctx context.Context,
	client llm.Client,
	opts agent.Options,
	childGoal string,
) ([]llm.Message, *agent.Result, error) {
	esc := r.child.TierEscalation
	maxRounds := 1
	if r.resolvedWorkerVerifyEnabled() {
		if esc.Enabled {
			maxRounds = esc.baseRounds()
		} else {
			maxRounds = 1 + r.resolvedMaxWorkerVerifyRetries()
		}
	}
	acChecks := acceptanceChecksFromGoal(childGoal)
	hist, res, err := r.runWorkerRounds(ctx, client, opts, childGoal, maxRounds, acChecks)
	if err != nil || res == nil || !esc.Enabled || !r.resolvedWorkerVerifyEnabled() {
		return hist, res, err
	}
	failSummary, failed := workerVerificationFailure(res.SubtaskResult)
	if !failed {
		return hist, res, nil
	}
	escClient, providerLabel, modelLabel, ok := r.escalationClient()
	if !ok {
		// No senior tier configured/resolvable — keep the base failure for replan.
		return hist, res, nil
	}
	escOpts := opts
	escOpts.ProviderLabel = providerLabel
	escOpts.ModelLabel = modelLabel
	escGoal := childGoal + formatTierEscalationPrompt(esc.tierName(), failSummary)
	escHist, escRes, escErr := r.runWorkerRounds(ctx, escClient, escOpts, escGoal, esc.escalatedRounds(), acChecks)
	if escErr != nil || escRes == nil {
		// Escalation infrastructure failure must not mask the original outcome.
		return hist, res, nil
	}
	escRes.SubtaskResult = annotateEscalatedResult(escRes.SubtaskResult, esc.tierName(), modelLabel)
	return escHist, escRes, nil
}

// runWorkerRounds executes up to maxRounds worker attempts with post-run
// deterministic checks, feeding verification failures back as hints.
// Arbitration order (spec §5.4): deterministic checks → acceptance_checks →
// LLM verifier. Red acceptance_checks block with priority over the verifier;
// green checks + red verifier yield needs_review for the Lead to arbitrate.
func (r *TaskRunner) runWorkerRounds(
	ctx context.Context,
	client llm.Client,
	opts agent.Options,
	childGoal string,
	maxRounds int,
	acChecks []AcceptanceCheck,
) ([]llm.Message, *agent.Result, error) {
	if maxRounds < 1 {
		maxRounds = 1
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
		report := VerifyWorkerOutcome(ctx, r.toolRunner, paths, r.resolvedWorkerVerifyOptions())
		if report.Passed {
			// Acceptance checks run only after green deterministic checks:
			// no point probing behavior of code that does not compile.
			acResults := runAcceptanceChecks(ctx, r.toolRunner.WorkspaceRoot(), acChecks, r.child.Caps.Exec, r.toolRunner.DryRun())
			report.Checks = append(report.Checks, acResults...)
			acRan, acGreen := acceptanceOutcome(acResults)
			if !acGreen {
				// Red acceptance check blocks with priority over the LLM
				// verifier (spec §5.4 arbitration, rule 1) and feeds the
				// same retry/escalation loop as any verification failure.
				report.Passed = false
				verifyFail = report.Summary()
				if round == maxRounds-1 {
					if res != nil {
						res.SubtaskResult = wrapWorkerVerifyFailure(taskResult, report)
					}
					return hist, res, nil
				}
				continue
			}
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
					if acRan {
						// Rule 2: checks green, verifier red → needs_review;
						// the Lead decides whether to fix code or criterion.
						res.SubtaskResult = wrapWorkerNeedsReview(taskResult, report, llmResult)
					} else {
						res.SubtaskResult = wrapWorkerLLMVerifyFailure(taskResult, report, llmResult)
					}
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

// workerVerificationFailure reports whether a wrapped worker result is a
// deterministic verification failure and returns its summary for the
// escalation hint. llm_verification_failed is excluded: a red LLM verifier
// with green deterministic checks is a criteria dispute for the Lead, not a
// code-quality signal (spec §5.4 arbitration).
func workerVerificationFailure(raw string) (summary string, failed bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || !json.Valid([]byte(raw)) {
		return "", false
	}
	var payload struct {
		Status       string             `json:"status"`
		Verification WorkerVerifyReport `json:"verification"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return "", false
	}
	if payload.Status != "verification_failed" {
		return "", false
	}
	return payload.Verification.Summary(), true
}

// escalationClient resolves the senior-tier LLM client via the same
// TierResolver/ChildClientResolver pair the spawn path uses.
func (r *TaskRunner) escalationClient() (client llm.Client, providerLabel, modelLabel string, ok bool) {
	if r.child.ResolveTier == nil || r.child.ResolveClient == nil {
		return nil, "", "", false
	}
	provider, model, found := r.child.ResolveTier(r.child.TierEscalation.tierName())
	if !found || (provider == "" && model == "") {
		return nil, "", "", false
	}
	c, pl, ml, err := r.child.ResolveClient(provider, model)
	if err != nil || c == nil {
		return nil, "", "", false
	}
	if pl == "" {
		pl = provider
	}
	if ml == "" {
		ml = model
	}
	return c, pl, ml, true
}

func formatTierEscalationPrompt(tier, failure string) string {
	return "\n\n--- TIER ESCALATION (system) ---\n" +
		"A lower-tier worker failed deterministic verification on this WorkOrder. Last failure:\n" +
		strings.TrimSpace(failure) +
		"\nYou are running on the senior tier (" + tier + "). Implement the WorkOrder correctly, fix the reported issues, then call task_result."
}

// annotateEscalatedResult marks the result JSON so the Lead sees the outcome
// came from an escalated tier (verified_success and verification_failed alike).
func annotateEscalatedResult(raw, tier, model string) string {
	trimmed := strings.TrimSpace(raw)
	var m map[string]any
	if trimmed == "" || json.Unmarshal([]byte(trimmed), &m) != nil {
		return raw
	}
	m["escalated_to_tier"] = tier
	if model != "" {
		m["escalated_model"] = model
	}
	if b, err := json.Marshal(m); err == nil {
		return string(b)
	}
	return raw
}
