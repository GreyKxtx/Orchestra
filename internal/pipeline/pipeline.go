// Package pipeline implements a deterministic multi-agent orchestration pipeline.
// The pipeline runs three roles sequentially: Investigator → Coder → Critic.
// Go code controls the sequencing; each role is a plain agent.Agent run.
//
// State transfer between stages is via in-memory text injection:
//   - Investigator result is injected into the Coder goal as <investigation>.
//   - Critic feedback is injected into the next Coder attempt as <critique>.
//
// Coder always runs dry-run internally; the pipeline applies ops once at the end
// when the Critic accepts (or max attempts are exhausted).
package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/daemon"
	"github.com/orchestra/orchestra/internal/patches"
	"github.com/orchestra/orchestra/internal/llm"
	"github.com/orchestra/orchestra/internal/ops"
	"github.com/orchestra/orchestra/internal/schema"
	"github.com/orchestra/orchestra/internal/tools"
)

// TraceContext carries runtime trace information used for evidence pre-fetching.
// When set in Options, the pipeline queries the CKG store before the Investigator
// runs and injects the results as <runtime_evidence> into both Investigator and Critic goals.
type TraceContext struct {
	TraceID string
}

// Options configures the pipeline run.
type Options struct {
	// MaxCoderAttempts is the maximum number of Coder→Critic cycles. Default: 2.
	MaxCoderAttempts int

	// Apply writes changes to disk if true. Default: false (dry-run).
	Apply  bool
	Backup bool

	// TraceCtx, if non-nil, enables runtime evidence pre-fetching for the given trace.
	TraceCtx *TraceContext

	// Per-stage step limits (0 = use defaults).
	MaxStepsInvestigator int
	MaxStepsCoder        int
	MaxStepsCritic       int

	PromptFamily   string
	ResponseFormat *llm.ResponseFormat
	Debug          bool

	MaxInvalidRetries    int
	MaxDeniedToolRepeats int
	MaxToolErrorRepeats  int
	MaxFinalFailures     int
	LLMStepTimeout       time.Duration
	MaxPromptBytes       int

	// OnEvent, if non-nil, receives streaming events tagged with the stage name.
	OnEvent func(stage string, ev agent.AgentEvent)

	AgentLogger *llm.Logger
}

// StageResult holds the outcome of a single pipeline stage.
type StageResult struct {
	Stage   string
	Steps   int
	Text    string // investigation text or critic verdict text
	Patches []patches.Patch
	Ops     []ops.AnyOp
}

// Result is the final output of a pipeline run.
type Result struct {
	Investigation string
	Attempts      int
	Accepted      bool
	FinalCritique string

	StageResults []StageResult
	Patches      []patches.Patch
	Ops          []ops.AnyOp
	ApplyResponse *tools.FSApplyOpsResponse
}

// Run executes the Investigator → Coder → Critic pipeline for query.
// toolRunner must be pre-configured with the project root.
func Run(
	ctx context.Context,
	llmClient llm.Client,
	validator *schema.Validator,
	toolRunner *tools.Runner,
	query string,
	opts Options,
) (*Result, error) {
	applyDefaults(&opts)
	result := &Result{}

	// Pre-fetch runtime evidence before the Investigator starts.
	// This is more reliable than hoping the model calls runtime.query itself.
	var runtimeEvidence string
	if opts.TraceCtx != nil && opts.TraceCtx.TraceID != "" {
		runtimeEvidence = fetchRuntimeEvidence(ctx, toolRunner, opts.TraceCtx.TraceID)
	}

	// --- Stage 1: Investigator ---
	investigationText, invSteps, err := runInvestigator(ctx, llmClient, validator, toolRunner, query, runtimeEvidence, opts)
	if err != nil {
		return nil, fmt.Errorf("pipeline investigator: %w", err)
	}
	result.Investigation = investigationText
	result.StageResults = append(result.StageResults, StageResult{
		Stage: "investigator", Steps: invSteps, Text: investigationText,
	})
	saveArtifact(toolRunner.WorkspaceRoot(), "investigation.md", investigationText)

	// --- Stage 2–3: Coder + Critic retry loop ---
	var finalPatches []patches.Patch
	var finalOps []ops.AnyOp
	var critique string

	for attempt := 1; attempt <= opts.MaxCoderAttempts; attempt++ {
		result.Attempts = attempt

		// Coder (always dry-run so no disk mutations happen mid-loop)
		coderGoal := buildCoderGoal(query, investigationText, critique, attempt)
		coderRes, coderSteps, err := runCoder(ctx, llmClient, validator, toolRunner, coderGoal, opts)
		if err != nil {
			return nil, fmt.Errorf("pipeline coder attempt %d: %w", attempt, err)
		}
		result.StageResults = append(result.StageResults, StageResult{
			Stage:   fmt.Sprintf("coder_%d", attempt),
			Steps:   coderSteps,
			Patches: coderRes.Patches,
			Ops:     coderRes.Ops,
		})

		// Critic
		criticGoal := buildCriticGoal(query, investigationText, runtimeEvidence, coderRes)
		accept, criticText, criticSteps, err := runCritic(ctx, llmClient, validator, toolRunner, criticGoal, opts)
		if err != nil {
			return nil, fmt.Errorf("pipeline critic attempt %d: %w", attempt, err)
		}
		result.StageResults = append(result.StageResults, StageResult{
			Stage: fmt.Sprintf("critic_%d", attempt),
			Steps: criticSteps,
			Text:  criticText,
		})
		saveArtifact(toolRunner.WorkspaceRoot(), fmt.Sprintf("critique_%d.md", attempt), criticText)

		if accept {
			result.Accepted = true
			finalPatches = coderRes.Patches
			finalOps = coderRes.Ops
			break
		}
		critique = criticText
		result.FinalCritique = criticText
	}

	// If Critic never accepted, use last Coder output anyway.
	if !result.Accepted && result.Attempts > 0 {
		for i := len(result.StageResults) - 1; i >= 0; i-- {
			sr := result.StageResults[i]
			if strings.HasPrefix(sr.Stage, "coder_") {
				finalPatches = sr.Patches
				finalOps = sr.Ops
				break
			}
		}
	}

	result.Patches = finalPatches
	result.Ops = finalOps

	// Apply at the end (single write, no mid-loop mutations).
	if opts.Apply && len(result.Ops) > 0 {
		resp, err := toolRunner.FSApplyOps(ctx, tools.FSApplyOpsRequest{
			Ops:    result.Ops,
			DryRun: false,
			Backup: opts.Backup,
		})
		if err != nil {
			return nil, fmt.Errorf("pipeline apply: %w", err)
		}
		result.ApplyResponse = resp
	}

	return result, nil
}

// --- stage runners ---

func runInvestigator(
	ctx context.Context,
	llmClient llm.Client,
	validator *schema.Validator,
	toolRunner *tools.Runner,
	query string,
	runtimeEvidence string,
	opts Options,
) (text string, steps int, err error) {
	goal := buildInvestigatorGoal(query, runtimeEvidence)
	ag, err := agent.New(llmClient, validator, toolRunner, agent.Options{
		MaxSteps:             opts.MaxStepsInvestigator,
		MaxInvalidRetries:    opts.MaxInvalidRetries,
		MaxDeniedToolRepeats: opts.MaxDeniedToolRepeats,
		MaxToolErrorRepeats:  opts.MaxToolErrorRepeats,
		MaxFinalFailures:     opts.MaxFinalFailures,
		MaxPromptBytes:       opts.MaxPromptBytes,
		LLMStepTimeout:       opts.LLMStepTimeout,
		PromptFamily:         opts.PromptFamily,
		ResponseFormat:       opts.ResponseFormat,
		Debug:                opts.Debug,
		AgentLogger:          opts.AgentLogger,
		// Read-only + task_result + runtime for trace correlation.
		CustomTools: tools.ListToolsForInvestigator(),
		OnEvent:     wrapOnEvent("investigator", opts.OnEvent),
	})
	if err != nil {
		return "", 0, err
	}
	_, res, runErr := ag.Run(ctx, nil, goal)
	if runErr != nil {
		return "", 0, runErr
	}
	return res.SubtaskResult, res.Steps, nil
}

func runCoder(
	ctx context.Context,
	llmClient llm.Client,
	validator *schema.Validator,
	toolRunner *tools.Runner,
	goal string,
	opts Options,
) (res *agent.Result, steps int, err error) {
	ag, err := agent.New(llmClient, validator, toolRunner, agent.Options{
		MaxSteps:             opts.MaxStepsCoder,
		MaxInvalidRetries:    opts.MaxInvalidRetries,
		MaxDeniedToolRepeats: opts.MaxDeniedToolRepeats,
		MaxToolErrorRepeats:  opts.MaxToolErrorRepeats,
		MaxFinalFailures:     opts.MaxFinalFailures,
		MaxPromptBytes:       opts.MaxPromptBytes,
		LLMStepTimeout:       opts.LLMStepTimeout,
		PromptFamily:         opts.PromptFamily,
		ResponseFormat:       opts.ResponseFormat,
		Debug:                opts.Debug,
		AgentLogger:          opts.AgentLogger,
		// Full build mode, always dry-run — pipeline applies at the end.
		Apply:   false,
		Backup:  false,
		OnEvent: wrapOnEvent("coder", opts.OnEvent),
	})
	if err != nil {
		return nil, 0, err
	}
	_, coderRes, runErr := ag.Run(ctx, nil, goal)
	if runErr != nil {
		return nil, 0, runErr
	}
	return coderRes, coderRes.Steps, nil
}

func runCritic(
	ctx context.Context,
	llmClient llm.Client,
	validator *schema.Validator,
	toolRunner *tools.Runner,
	goal string,
	opts Options,
) (accept bool, text string, steps int, err error) {
	ag, err := agent.New(llmClient, validator, toolRunner, agent.Options{
		MaxSteps:             opts.MaxStepsCritic,
		MaxInvalidRetries:    opts.MaxInvalidRetries,
		MaxDeniedToolRepeats: opts.MaxDeniedToolRepeats,
		MaxToolErrorRepeats:  opts.MaxToolErrorRepeats,
		MaxFinalFailures:     opts.MaxFinalFailures,
		MaxPromptBytes:       opts.MaxPromptBytes,
		LLMStepTimeout:       opts.LLMStepTimeout,
		PromptFamily:         opts.PromptFamily,
		ResponseFormat:       opts.ResponseFormat,
		Debug:                opts.Debug,
		AgentLogger:          opts.AgentLogger,
		// Read-only + task_result; no write tools.
		CustomTools: tools.ListToolsForChild(),
		OnEvent:     wrapOnEvent("critic", opts.OnEvent),
	})
	if err != nil {
		return false, "", 0, err
	}
	_, res, runErr := ag.Run(ctx, nil, goal)
	if runErr != nil {
		return false, "", 0, runErr
	}
	accepted, verdict := parseVerdict(res.SubtaskResult)
	return accepted, verdict, res.Steps, nil
}

// --- goal builders ---

func buildInvestigatorGoal(query, runtimeEvidence string) string {
	var b strings.Builder
	b.WriteString(`Ты — Investigator. Твоя задача: исследовать кодовую базу и собрать всю информацию, необходимую для выполнения задачи ниже.

Используй read, ls, glob, grep, symbols для анализа.`)

	if runtimeEvidence != "" {
		b.WriteString("\n\n")
		b.WriteString(runtimeEvidence)
		b.WriteString("\n\nПрежде всего разбери <runtime_evidence>: найди в кодовой базе файлы и функции, " +
			"соответствующие CKG-нодам из спанов с ошибками. " +
			"Используй runtime для уточняющих запросов если нужно.")
	}

	b.WriteString(`

Когда закончишь — вызови task_result с подробным структурированным отчётом:
- Какие файлы затронуты
- Какие функции/типы нужно изменить (с полными именами)
- Зависимости между файлами
- Риски и нетривиальные места

Задача:
`)
	b.WriteString(strings.TrimSpace(query))
	return b.String()
}

func buildCoderGoal(query, investigation, critique string, attempt int) string {
	var b strings.Builder

	if attempt > 1 {
		b.WriteString("Это попытка №")
		b.WriteString(fmt.Sprintf("%d", attempt))
		b.WriteString(". Предыдущая реализация получила замечания — исправь их.\n\n")
	}

	b.WriteString("Задача:\n")
	b.WriteString(strings.TrimSpace(query))

	if investigation != "" {
		b.WriteString("\n\n<investigation>\n")
		b.WriteString(strings.TrimSpace(investigation))
		b.WriteString("\n</investigation>")
	}

	if critique != "" {
		b.WriteString("\n\n<critique>\n")
		b.WriteString(strings.TrimSpace(critique))
		b.WriteString("\n</critique>")
		b.WriteString("\n\nИсправь все замечания из <critique>. Не повторяй прежних ошибок.")
	}

	b.WriteString("\n\nРеализуй задачу. Используй file.search_replace для точечных правок, file.write_atomic для новых файлов.")
	return b.String()
}

func buildCriticGoal(query, investigation, runtimeEvidence string, coderRes *agent.Result) string {
	var b strings.Builder

	b.WriteString("Ты — Critic. Проверь реализацию задачи на корректность и полноту.\n\n")
	b.WriteString("Задача:\n")
	b.WriteString(strings.TrimSpace(query))

	if investigation != "" {
		b.WriteString("\n\n<investigation>\n")
		b.WriteString(truncate(investigation, 2000))
		b.WriteString("\n</investigation>")
	}

	if runtimeEvidence != "" {
		b.WriteString("\n\n")
		b.WriteString(runtimeEvidence)
	}

	// Inject patch summary so Critic can review without applying.
	patchSummary := summarizePatches(coderRes.Patches)
	if patchSummary != "" {
		b.WriteString("\n\n<proposed_changes>\n")
		b.WriteString(patchSummary)
		b.WriteString("\n</proposed_changes>")
	}

	b.WriteString(`

Прочитай изменённые файлы через read и оцени:
1. Решает ли реализация поставленную задачу?
2. Есть ли логические ошибки или упущенные граничные случаи?
3. Соответствует ли код конвенциям кодовой базы?
4. Есть ли проблемы с производительностью или безопасностью?

Когда закончишь анализ — вызови task_result с JSON-вердиктом:
{"status":"accept","reason":"<краткое обоснование>"}
  — если реализация корректна
{"status":"reject","reason":"<главная проблема>","issues":["<issue1>","<issue2>"]}
  — если нужны исправления`)

	return b.String()
}

// --- helpers ---

// parseVerdict interprets a Critic's task_result content.
// Returns (accept, text).
func parseVerdict(content string) (bool, string) {
	content = strings.TrimSpace(content)
	if content == "" {
		// Empty verdict = accept (Critic had nothing to say).
		return true, ""
	}

	// Try structured JSON first.
	var v struct {
		Status string   `json:"status"`
		Reason string   `json:"reason"`
		Issues []string `json:"issues"`
	}
	if json.Unmarshal([]byte(content), &v) == nil && v.Status != "" {
		accept := strings.EqualFold(v.Status, "accept")
		if accept {
			return true, v.Reason
		}
		text := v.Reason
		if len(v.Issues) > 0 {
			text += "\n" + strings.Join(v.Issues, "\n")
		}
		return false, text
	}

	// Lenient fallback: look for the word "reject" first (takes precedence).
	lower := strings.ToLower(content)
	if strings.Contains(lower, "reject") {
		return false, content
	}
	if strings.Contains(lower, "accept") {
		return true, content
	}
	// Default: reject (safe side — better to retry than ship bad code).
	return false, content
}

// summarizePatches produces a human-readable patch summary for the Critic prompt.
// Truncated to avoid overwhelming the context.
func summarizePatches(patchList []patches.Patch) string {
	if len(patchList) == 0 {
		return ""
	}
	var b strings.Builder
	for i, p := range patchList {
		if i >= 10 {
			fmt.Fprintf(&b, "... and %d more patches\n", len(patchList)-10)
			break
		}
		switch p.Type {
		case patches.TypeFileSearchReplace:
			fmt.Fprintf(&b, "[search_replace] %s\n", p.Path)
		case patches.TypeFileUnifiedDiff:
			fmt.Fprintf(&b, "[unified_diff] %s\n", p.Path)
		case patches.TypeFileWriteAtomic:
			fmt.Fprintf(&b, "[write_atomic] %s (%d bytes)\n", p.Path, len(p.Content))
		default:
			fmt.Fprintf(&b, "[%s] %s\n", p.Type, p.Path)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n...(truncated)"
}

func wrapOnEvent(stage string, fn func(string, agent.AgentEvent)) func(agent.AgentEvent) {
	if fn == nil {
		return nil
	}
	return func(ev agent.AgentEvent) { fn(stage, ev) }
}

func saveArtifact(workspaceRoot, name, content string) {
	dir := filepath.Join(workspaceRoot, ".orchestra")
	_ = os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, name)
	_ = daemon.AtomicWriteFile(path, []byte(content), 0o644)
}

// formatRuntimeEvidence converts a RuntimeQueryResponse into a <runtime_evidence> prompt block.
// Returns "" if resp is nil or has no spans.
func formatRuntimeEvidence(resp *tools.RuntimeQueryResponse) string {
	if resp == nil || len(resp.Spans) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<runtime_evidence>\n")
	if resp.Service != "" {
		fmt.Fprintf(&b, "Service: %s | Trace: %s\n\n", resp.Service, resp.TraceID)
	}
	for _, s := range resp.Spans {
		b.WriteString(fmt.Sprintf("[%s] %s", s.Status, s.Name))
		if s.NodeFQN != "" {
			b.WriteString(fmt.Sprintf(" → CKG:%s", s.NodeFQN))
		}
		if s.ErrorMsg != "" {
			b.WriteString(fmt.Sprintf(" | err: %s", s.ErrorMsg))
		}
		if s.CodeFile != "" {
			if s.CodeLineno > 0 {
				b.WriteString(fmt.Sprintf(" | %s:%d", s.CodeFile, s.CodeLineno))
			} else {
				b.WriteString(fmt.Sprintf(" | %s", s.CodeFile))
			}
		}
		b.WriteString("\n")
	}
	b.WriteString("</runtime_evidence>")
	return b.String()
}

// fetchRuntimeEvidence queries the CKG store and returns a formatted <runtime_evidence> block.
// Returns "" if the store is not configured, the trace is not found, or no spans exist.
func fetchRuntimeEvidence(ctx context.Context, toolRunner *tools.Runner, traceID string) string {
	resp, err := toolRunner.RuntimeQuery(ctx, tools.RuntimeQueryRequest{TraceID: traceID})
	if err != nil {
		return ""
	}
	return formatRuntimeEvidence(resp)
}

func applyDefaults(opts *Options) {
	if opts.MaxCoderAttempts <= 0 {
		opts.MaxCoderAttempts = 2
	}
	if opts.MaxStepsInvestigator <= 0 {
		opts.MaxStepsInvestigator = 10
	}
	if opts.MaxStepsCoder <= 0 {
		opts.MaxStepsCoder = 24
	}
	if opts.MaxStepsCritic <= 0 {
		opts.MaxStepsCritic = 8
	}
	if opts.MaxInvalidRetries <= 0 {
		opts.MaxInvalidRetries = 3
	}
	if opts.MaxDeniedToolRepeats <= 0 {
		opts.MaxDeniedToolRepeats = 2
	}
	if opts.MaxToolErrorRepeats <= 0 {
		opts.MaxToolErrorRepeats = 6
	}
	if opts.MaxFinalFailures <= 0 {
		opts.MaxFinalFailures = 6
	}
	if opts.LLMStepTimeout <= 0 {
		opts.LLMStepTimeout = 25 * time.Second
	}
	if opts.MaxPromptBytes <= 0 {
		opts.MaxPromptBytes = 64 * 1024
	}
}
