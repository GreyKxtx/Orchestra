package core

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/applier"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/patches"
	"github.com/orchestra/orchestra/internal/llm"
	"github.com/orchestra/orchestra/internal/ops"
	"github.com/orchestra/orchestra/internal/protocol"
	"github.com/orchestra/orchestra/internal/schema"
	"github.com/orchestra/orchestra/internal/cache"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/internal/usage"
	promptpkg "github.com/orchestra/orchestra/internal/prompt"

	coresession "github.com/orchestra/orchestra/internal/core/session"
	"github.com/orchestra/orchestra/internal/hooks"
	"github.com/orchestra/orchestra/internal/mcp"
	"github.com/orchestra/orchestra/internal/sessionfile"
	"github.com/orchestra/orchestra/internal/tasks"
)

type Core struct {
	workspaceRoot string
	projectID     string
	debug         bool
	initMu        sync.Mutex
	initialized   bool
	initParams    *InitializeParams

	cfg               *config.ProjectConfig
	llmClient         llm.Client
	llmClientInjected bool // true when LLMClient was set via Options (test/DI mode)

	validator *schema.Validator
	tools     *tools.Runner
	// runMu serialises every RPC entry point that mutates shared Runner state
	// (SetDryRun, ClearStaged, staged-overlay writes). Without this, two
	// concurrent agent.run / session.message / workflow.run / skill.invoke /
	// ops.apply / session.apply_pending calls race over the dry-run flag and
	// can leak staged ops between requests.
	runMu      sync.Mutex
	sessions   *coresession.Manager
	mcpManager *mcp.Manager
}

type Options struct {
	Debug bool
	// LLMClient overrides the default OpenAI client (used in tests).
	LLMClient llm.Client
}

func New(workspaceRoot string, opts Options) (*Core, error) {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	if workspaceRoot == "" {
		return nil, fmt.Errorf("workspaceRoot is empty")
	}
	rootAbs, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("abs workspaceRoot: %w", err)
	}

	// Load project config from workspace root.
	cfgPath := filepath.Join(rootAbs, ".orchestra.yml")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	projectID, err := cache.ComputeProjectID(cfg.ProjectRoot)
	if err != nil {
		return nil, err
	}

	v, err := schema.NewValidator()
	if err != nil {
		return nil, err
	}

	tr, err := tools.NewRunner(cfg.ProjectRoot, tools.RunnerOptions{
		ExcludeDirs:        cfg.ExcludeDirs,
		ExecTimeout:        time.Duration(cfg.Exec.TimeoutS) * time.Second,
		ExecOutputLimit:    cfg.Exec.OutputLimitKB * 1024,
		WebFetchTimeout:    time.Duration(cfg.Web.FetchTimeoutS) * time.Second,
		WebMaxContentBytes: cfg.Web.MaxContentBytes,
		WebSearch:          cfg.Web.Search,
		LSP:                cfg.LSP,
		Embed:              cfg.Embed,
		// JSON-RPC core makes a hard "no side effects in dry-run" promise to
		// remote clients (TUI / IDE / web). Block bash bypassing the staging
		// overlay. CLI's plan-mode uses its own Runner without this flag so
		// bash inspection (git status, go test) keeps working in plan mode.
		BlockExecInDryRun: true,
	})
	if err != nil {
		return nil, err
	}

	injected := opts.LLMClient != nil
	llmClient := opts.LLMClient
	if llmClient == nil {
		llmClient = llm.NewClient(cfg.LLM)
		if oc, ok := llmClient.(*llm.OpenAIClient); ok {
			oc.SetLogger(llm.NewLogger(rootAbs))
		}
	}

	// Start MCP servers (non-fatal: errors are logged but don't abort Core startup).
	var mcpMgr *mcp.Manager
	if len(cfg.MCP.Servers) > 0 {
		var startErrs []error
		mcpMgr, startErrs = mcp.NewManager(context.Background(), cfg.MCP)
		for _, err := range startErrs {
			// Log to stderr — not a fatal error.
			fmt.Fprintf(os.Stderr, "orchestra: mcp startup warning: %v\n", err)
		}
		if !mcpMgr.IsEmpty() {
			tr.SetMCPCaller(mcpMgr)
		}
	}
	tr.SetMemoryContext("", cfg.Memory.Resolve())

	return &Core{
		workspaceRoot:     rootAbs,
		projectID:         projectID,
		debug:             opts.Debug,
		cfg:               cfg,
		llmClient:         llmClient,
		llmClientInjected: injected,
		validator:         v,
		tools:             tr,
		sessions:          coresession.NewManager(),
		mcpManager:        mcpMgr,
	}, nil
}

// WarmupCKG starts a background CKG scan bound to ctx. Call once after New
// so the graph is populated before the first agent run or explore call.
func (c *Core) WarmupCKG(ctx context.Context) {
	c.tools.WarmupCKG(ctx)
}

func (c *Core) Health() protocol.Health {
	return protocol.Health{
		Status:          "ok",
		CoreVersion:     protocol.CoreVersion,
		ProtocolVersion: protocol.ProtocolVersion,
		OpsVersion:      protocol.OpsVersion,
		ToolsVersion:    protocol.ToolsVersion,
		WorkspaceRoot:   c.workspaceRoot,
		ProjectID:       c.projectID,
	}
}

type InitializeParams struct {
	ProjectRoot     string `json:"project_root"`
	ProjectID       string `json:"project_id"`
	ProtocolVersion int    `json:"protocol_version"`
	OpsVersion      int    `json:"ops_version,omitempty"`
	ToolsVersion    int    `json:"tools_version,omitempty"`
}

type InitializeResult struct {
	Status string          `json:"status"`
	Health protocol.Health `json:"health"`
}

func (c *Core) Initialize(params InitializeParams) (*InitializeResult, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}

	root := strings.TrimSpace(params.ProjectRoot)
	if root == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "project_root is empty", nil)
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "invalid project_root", map[string]any{
			"project_root": root,
			"error":        err.Error(),
		})
	}

	// Canonicalize optional version fields so initialize stays idempotent even if the
	// client omits ops/tools versions on subsequent calls.
	canonical := InitializeParams{
		ProjectRoot:     rootAbs,
		ProjectID:       strings.TrimSpace(params.ProjectID),
		ProtocolVersion: params.ProtocolVersion,
		OpsVersion:      params.OpsVersion,
		ToolsVersion:    params.ToolsVersion,
	}
	if canonical.OpsVersion == 0 {
		canonical.OpsVersion = protocol.OpsVersion
	}
	if canonical.ToolsVersion == 0 {
		canonical.ToolsVersion = protocol.ToolsVersion
	}

	c.initMu.Lock()
	defer c.initMu.Unlock()

	// initialize is idempotent:
	// - same params => OK
	// - different params => AlreadyInitialized (or ProtocolMismatch per spec)
	if c.initialized {
		if c.initParams != nil && sameInitializeParams(*c.initParams, canonical) {
			return &InitializeResult{Status: "ok", Health: c.Health()}, nil
		}
		return nil, protocol.NewError(protocol.AlreadyInitialized, "core already initialized with different parameters", map[string]any{
			"expected": c.initParams,
			"got":      canonical,
		})
	}

	// First-time initialize: enforce handshake constraints.
	if canonical.ProtocolVersion != protocol.ProtocolVersion {
		return nil, protocol.NewError(protocol.ProtocolMismatch, "protocol_version mismatch", map[string]any{
			"client": canonical.ProtocolVersion,
			"core":   protocol.ProtocolVersion,
		})
	}
	if canonical.OpsVersion != protocol.OpsVersion {
		return nil, protocol.NewError(protocol.ProtocolMismatch, "ops_version mismatch", map[string]any{
			"client": canonical.OpsVersion,
			"core":   protocol.OpsVersion,
		})
	}
	if canonical.ToolsVersion != protocol.ToolsVersion {
		return nil, protocol.NewError(protocol.ProtocolMismatch, "tools_version mismatch", map[string]any{
			"client": canonical.ToolsVersion,
			"core":   protocol.ToolsVersion,
		})
	}
	if !samePath(rootAbs, c.workspaceRoot) {
		return nil, protocol.NewError(protocol.ProtocolMismatch, "project_root mismatch", map[string]any{
			"client": rootAbs,
			"core":   c.workspaceRoot,
		})
	}
	if strings.TrimSpace(canonical.ProjectID) == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "project_id is empty", nil)
	}
	if strings.TrimSpace(canonical.ProjectID) != c.projectID {
		return nil, protocol.NewError(protocol.ProtocolMismatch, "project_id mismatch", map[string]any{
			"client": canonical.ProjectID,
			"core":   c.projectID,
		})
	}

	c.initialized = true
	c.initParams = &canonical

	return &InitializeResult{
		Status: "ok",
		Health: c.Health(),
	}, nil
}

func sameInitializeParams(a, b InitializeParams) bool {
	if !samePath(a.ProjectRoot, b.ProjectRoot) {
		return false
	}
	if strings.TrimSpace(a.ProjectID) != strings.TrimSpace(b.ProjectID) {
		return false
	}
	if a.ProtocolVersion != b.ProtocolVersion {
		return false
	}
	if a.OpsVersion != b.OpsVersion {
		return false
	}
	if a.ToolsVersion != b.ToolsVersion {
		return false
	}
	return true
}

func (c *Core) IsInitialized() bool {
	if c == nil {
		return false
	}
	c.initMu.Lock()
	defer c.initMu.Unlock()
	return c.initialized
}

type AgentRunParams struct {
	Query string `json:"query"`

	Apply  bool `json:"apply,omitempty"`
	Backup bool `json:"backup,omitempty"`

	MaxSteps          int `json:"max_steps,omitempty"`
	MaxInvalidRetries int `json:"max_invalid_retries,omitempty"`
	MaxPromptBytes    int `json:"max_prompt_bytes,omitempty"`

	AllowExec bool `json:"allow_exec,omitempty"`
	Debug     bool `json:"debug,omitempty"`

	// Mode selects the agent mode or custom agent name (from agents: in .orchestra.yml).
	Mode string `json:"mode,omitempty"`

	// ApplyOutput selects how changes are materialised: "disk" (default) or "patch".
	// When "patch", Apply is forced false and a unified .patch is written to PatchPath
	// (or apply.patch_dir default).
	ApplyOutput string `json:"apply_output,omitempty"`
	// PatchPath is an optional absolute or project-relative path for apply_output=patch.
	PatchPath string `json:"patch_path,omitempty"`
	// Profile is an adaptive execution preset: "fast" | "precision".
	Profile string `json:"profile,omitempty"`

	// OnEvent is called for each agent streaming event (method + params).
	// Not serialized — set programmatically by the RPC handler.
	OnEvent func(method string, params any) `json:"-"`

	// PermissionRequester, if non-nil, is consulted before exec.run/bash runs
	// instead of (or before) the static AllowExec gate. Set programmatically by the RPC handler.
	PermissionRequester PermissionRequester `json:"-"`

	// QuestionAsker, if non-nil, enables the question tool and plan_exit approval.
	// Set programmatically by the RPC handler.
	QuestionAsker tools.QuestionAsker `json:"-"`
}

type AgentRunResult struct {
	Steps   int  `json:"steps"`
	Applied bool `json:"applied"`

	Patches []patches.Patch `json:"patches,omitempty"`
	Ops     []ops.AnyOp           `json:"ops,omitempty"`

	ApplyResponse *tools.FSApplyOpsResponse `json:"apply_response,omitempty"`

	// PatchPath is set when apply_output=patch and a unified diff was written.
	PatchPath string `json:"patch_path,omitempty"`

	// SwitchToBuild is true when plan_exit requested a build continuation
	// that was not completed in this response (legacy; normally handled in-core).
	SwitchToBuild bool `json:"switch_to_build,omitempty"`

	Todos []tools.TodoItem `json:"todos,omitempty"`

	// PlanPath is the session plan markdown file when plan mode was used.
	PlanPath string `json:"plan_path,omitempty"`

	// Usage summarises token consumption for this run.
	// did not return usage info (some local servers omit it).
	Usage *UsageSnapshot `json:"usage,omitempty"`
}

// UsageSnapshot is the totals view of one run's token consumption, returned
// over JSON-RPC so the caller (CLI) can display a summary without re-reading
// usage.jsonl.
type UsageSnapshot struct {
	Calls            int     `json:"calls"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	CostUSD          float64 `json:"cost_usd,omitempty"`
}

func (c *Core) AgentRun(ctx context.Context, params AgentRunParams) (*AgentRunResult, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	if strings.TrimSpace(params.Query) == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "query is empty", nil)
	}
	if params.Mode != "" && !config.IsBuiltInMode(params.Mode) && c.cfg != nil && c.cfg.FindAgent(params.Mode) == nil {
		return nil, protocol.NewError(protocol.InvalidLLMOutput,
			fmt.Sprintf("unknown agent mode %q: not a built-in mode and not defined in agents: in .orchestra.yml", params.Mode), nil)
	}

	applyOutput := strings.ToLower(strings.TrimSpace(params.ApplyOutput))
	if applyOutput == "" && c.cfg != nil {
		applyOutput = strings.ToLower(strings.TrimSpace(c.cfg.Apply.Output))
	}
	if applyOutput == "" {
		applyOutput = config.ApplyOutputDisk
	}
	if applyOutput != config.ApplyOutputDisk && applyOutput != config.ApplyOutputPatch {
		return nil, protocol.NewError(protocol.InvalidParams,
			fmt.Sprintf("apply_output must be %q or %q", config.ApplyOutputDisk, config.ApplyOutputPatch), nil)
	}
	if applyOutput == config.ApplyOutputPatch {
		if params.Apply {
			return nil, protocol.NewError(protocol.InvalidParams,
				"apply_output=patch is mutually exclusive with apply=true", nil)
		}
		params.Apply = false
		params.Backup = false
	}

	profileName := strings.TrimSpace(params.Profile)
	if profileName == "" && c.cfg != nil {
		profileName = strings.TrimSpace(c.cfg.Agent.Profile)
	}
	if !agent.IsKnownProfile(profileName) {
		return nil, protocol.NewError(protocol.InvalidParams,
			fmt.Sprintf("unknown profile %q (want fast|precision)", profileName), nil)
	}

	// Build ResponseFormat from config (grammar-constrained sampling for local models).
	var respFmt *llm.ResponseFormat
	if c.cfg != nil && c.cfg.LLM.ResponseFormatType != "" {
		respFmt = &llm.ResponseFormat{Type: c.cfg.LLM.ResponseFormatType}
		if c.cfg.LLM.ResponseFormatType == "json_schema" {
			respFmt.Schema = schema.AgentStepSchemaRaw()
			respFmt.SchemaName = "agent_step"
		}
	}

	// Merge params with config defaults (params take precedence when non-zero).
	maxSteps := params.MaxSteps
	if maxSteps <= 0 && c.cfg != nil {
		maxSteps = c.cfg.Agent.MaxSteps
	}
	maxRetries := params.MaxInvalidRetries
	if maxRetries <= 0 && c.cfg != nil {
		maxRetries = c.cfg.Agent.MaxInvalidRetries
	}
	maxPromptBytes := params.MaxPromptBytes
	if maxPromptBytes <= 0 && c.cfg != nil {
		maxPromptBytes = c.cfg.Limits.ContextKB * 1024
	}

	promptFamily := ""
	if c.cfg != nil {
		promptFamily = promptpkg.ResolvePromptFamily(c.cfg.LLM.PromptFamily, c.cfg.LLM.Model)
	}

	// Build OnEvent callback: translate agent.AgentEvent to JSON-RPC notifications.
	var onEvent func(agent.AgentEvent)
	if params.OnEvent != nil {
		onEvent = buildAgentOnEvent(params.OnEvent, EventEnvelope{TurnID: NewTurnID()})
	}

	allowExec := params.AllowExec
	if c.cfg != nil && c.cfg.Exec.Confirm != nil && !*c.cfg.Exec.Confirm {
		allowExec = true // Confirm: false in config = allow all (backward compat)
	}
	var execAllow, execDeny []string
	if c.cfg != nil {
		execAllow = c.cfg.Exec.Allow
		execDeny = c.cfg.Exec.Deny
	}

	var agentLogger *llm.Logger
	if c.llmClient != nil {
		if oc, ok := c.llmClient.(*llm.OpenAIClient); ok {
			agentLogger = oc.GetLogger()
		}
	}

	var hooksRunner agent.HooksRunner
	if hr := hooks.New(c.cfg.Hooks, c.workspaceRoot); hr != nil {
		hooksRunner = hr
	}

	customOpts, err := c.resolveCustomAgentOpts(params.Mode, agentLogger)
	if err != nil {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, err.Error(), nil)
	}

	// Configure staging mode: dry-run when not applying; clear any stale state from prior runs.
	// runMu serialises this whole call so a concurrent agent.run/workflow.run/
	// skill.invoke/ops.apply cannot flip dry-run mid-flight on our shared Runner.
	c.runMu.Lock()
	defer c.runMu.Unlock()
	c.tools.SetDryRun(!params.Apply)
	c.tools.ClearStaged()

	usageTracker := newAgentUsageTracker(c.cfg, "agent.run")
	taskRunner := tasks.New(c.llmClient, c.validator, c.tools, childAgentConfig(c.cfg, maxPromptBytes, usageTracker))

	agOpts := agent.Options{
		MaxSteps:             maxSteps,
		MaxInvalidRetries:    maxRetries,
		MaxDeniedToolRepeats: c.cfg.Agent.MaxDeniedRepeats,
		MaxToolErrorRepeats:  c.cfg.Agent.MaxToolErrors,
		MaxFinalFailures:     c.cfg.Agent.MaxFinalFailures,
		MaxPromptBytes:       maxPromptBytes,
		CompactThresholdPct:  c.cfg.Agent.CompactThresholdPct,
		LLMStepTimeout:       time.Duration(c.cfg.LLM.TimeoutS) * time.Second,
		Apply:                params.Apply,
		Backup:               params.Backup,
		AllowExec:            allowExec,
		ExecAllow:            execAllow,
		ExecDeny:             execDeny,
		PermissionRules:      c.cfg.Permissions.Rules,
		Debug:                params.Debug || c.debug,
		ResponseFormat:       respFmt,
		PromptFamily:         promptFamily,
		Mode:                 agent.Mode(params.Mode),
		SystemPromptOverride: customOpts.systemPromptOverride,
		CustomTools:          customOpts.customTools,
		OnEvent:              onEvent,
		AgentLogger:          agentLogger,
		SubtaskRunner:        taskRunner,
		HooksRunner:          hooksRunner,
		ExtraTools:           c.mcpToolDefs(),
		PermissionRequester:  convertPermissionRequester(params.PermissionRequester),
		QuestionAsker:        params.QuestionAsker,
		UsageTracker:         usageTracker,
		ProviderLabel:        providerLabelOf(c.cfg),
		ModelLabel:           c.cfg.LLM.Model,
		PlanPath:             resolvePlanPath(params.Mode, "", ""),
		Memory:               c.cfg.Memory.Resolve(),
		ToolDigestBytes:        c.cfg.Agent.ResolvedToolDigestBytes(),
		HistoryPruneKeepRecent: c.cfg.Agent.ResolvedHistoryPruneKeepRecent(),
		AutoSessionMemory:    false,
	}
	// Profile overlays config defaults. When a profile is selected it wins over
	// agent.max_steps / limits.context_kb for the knobs it owns; omit profile
	// to drive those via RPC max_steps / max_prompt_bytes alone.
	if err := agent.ApplyProfile(&agOpts, profileName, false); err != nil {
		return nil, protocol.NewError(protocol.InvalidParams, err.Error(), nil)
	}
	if customOpts.systemPromptOverride != "" {
		agOpts.SystemPromptOverride = customOpts.systemPromptOverride
	}
	if customOpts.customTools != nil {
		agOpts.CustomTools = customOpts.customTools
	}

	ag, err := agent.New(customOpts.llmClient, c.validator, c.tools, agOpts)
	if err != nil {
		return nil, err
	}

	var outHistory []llm.Message
	var res *agent.Result
	outHistory, res, err = ag.Run(ctx, nil, params.Query)
	if err != nil {
		return nil, err
	}
	outHistory, res, err = maybeContinueBuildAfterPlan(ctx, customOpts.llmClient, c.validator, c.tools, agOpts, outHistory, res)
	if err != nil {
		return nil, err
	}
	finalizeAgentUsage(usageTracker, c.workspaceRoot)

	result := &AgentRunResult{
		Steps:         res.Steps,
		Applied:       res.Applied,
		Patches:       res.Patches,
		Ops:           res.Ops,
		ApplyResponse: res.ApplyResponse,
		SwitchToBuild: res.SwitchToBuild,
		Todos:         res.Todos,
		PlanPath:      agOpts.PlanPath,
		Usage:         usageSnapshotFrom(usageTracker),
	}
	if applyOutput == config.ApplyOutputPatch {
		path, werr := c.writeAgentPatch(params.PatchPath, res)
		if werr != nil {
			return nil, protocol.NewError(protocol.ExecFailed, werr.Error(), nil)
		}
		result.PatchPath = path
		result.Applied = false
	}
	return result, nil
}

func (c *Core) writeAgentPatch(explicit string, res *agent.Result) (string, error) {
	dir := ".orchestra/patches"
	if c.cfg != nil && c.cfg.Apply.PatchDir != "" {
		dir = c.cfg.Apply.PatchDir
	}
	path := strings.TrimSpace(explicit)
	if path == "" {
		path = applier.DefaultPatchPath(dir)
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(c.workspaceRoot, path)
	}
	var diffs []applier.FileDiff
	if res != nil && res.ApplyResponse != nil {
		diffs = res.ApplyResponse.Diffs
	}
	if err := applier.WriteUnifiedPatch(path, diffs); err != nil {
		return "", err
	}
	return path, nil
}

// providerLabelOf falls back to "openai" when the config doesn't name a provider.
func providerLabelOf(cfg *config.ProjectConfig) string {
	if cfg == nil {
		return "openai"
	}
	if cfg.LLM.Provider != "" {
		return cfg.LLM.Provider
	}
	return "openai"
}

// newAgentUsageTracker wires a Tracker pre-loaded with the project's pricing.
func newAgentUsageTracker(cfg *config.ProjectConfig, label string) *usage.Tracker {
	pricing := usage.Pricing(nil)
	if cfg != nil && len(cfg.Pricing) > 0 {
		pricing = make(usage.Pricing, len(cfg.Pricing))
		for prov, models := range cfg.Pricing {
			bucket := make(map[string]usage.ModelPricing, len(models))
			for m, mp := range models {
				bucket[m] = usage.ModelPricing{InputPer1M: mp.InputPer1M, OutputPer1M: mp.OutputPer1M}
			}
			pricing[prov] = bucket
		}
	}
	runID := time.Now().UTC().Format("20060102T150405.000Z")
	return usage.NewTracker(runID, label, pricing)
}

// finalizeAgentUsage best-effort persists usage. Errors are swallowed; the RPC
// caller still gets the in-memory totals via UsageSnapshot in the response.
func finalizeAgentUsage(t *usage.Tracker, workspaceRoot string) {
	if t == nil || t.Empty() {
		return
	}
	_, _, _ = t.Finalize(workspaceRoot)
}

// usageSnapshotFrom flattens tracker totals into a wire-friendly struct for
// JSON-RPC responses. Returns nil when no calls were recorded.
func usageSnapshotFrom(t *usage.Tracker) *UsageSnapshot {
	if t == nil || t.Empty() {
		return nil
	}
	calls, prompt, completion, total, cost := t.Total()
	return &UsageSnapshot{
		Calls:            calls,
		PromptTokens:     prompt,
		CompletionTokens: completion,
		TotalTokens:      total,
		CostUSD:          cost,
	}
}

type ToolCallParams struct {
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

func (c *Core) ToolCall(ctx context.Context, params ToolCallParams) (json.RawMessage, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	if strings.TrimSpace(params.Name) == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "tool name is empty", nil)
	}

	// Consent policy: bash is blocked by default (Confirm=true).
	// Allowed when: Confirm=false (allow all), OR command is in exec.allow list.
	canonicalName := strings.TrimSpace(params.Name)
	if canonicalName == "bash" && c.cfg != nil {
		confirm := c.cfg.Exec.Confirm == nil || *c.cfg.Exec.Confirm
		if confirm {
			var execReq struct {
				Command string `json:"command"`
			}
			_ = json.Unmarshal(params.Input, &execReq)
			if !c.cfg.Exec.IsCommandAllowed(execReq.Command) {
				msg := "bash requires user consent (configure exec.allow or use --allow-exec)"
				if len(c.cfg.Exec.Allow) > 0 {
					msg = fmt.Sprintf("bash: command %q is not in the allowlist", execReq.Command)
				}
				return nil, protocol.NewError(protocol.ExecDenied, msg, map[string]any{
					"tool":    params.Name,
					"command": execReq.Command,
				})
			}
		}
	}

	return c.tools.Call(ctx, params.Name, params.Input)
}

// Close releases resources held by Core (tools.Runner, MCP manager).
// Safe to call multiple times.
func (c *Core) Close() error {
	if c.mcpManager != nil {
		c.mcpManager.Close()
	}
	if c.tools != nil {
		if err := c.tools.Close(); err != nil {
			return fmt.Errorf("close tools runner: %w", err)
		}
		c.tools = nil
	}
	return nil
}

// mcpToolDefs returns MCP tool definitions if a manager is active.
func (c *Core) mcpToolDefs() []llm.ToolDef {
	if c.mcpManager == nil || c.mcpManager.IsEmpty() {
		return nil
	}
	return c.mcpManager.ListToolDefs()
}

// customAgentOpts holds resolved overrides for a custom agent.
type customAgentOpts struct {
	llmClient            llm.Client
	systemPromptOverride string
	customTools          []llm.ToolDef // nil = use mode-based selection
}

// resolveCustomAgentOpts looks up mode in agents: and builds the per-agent
// overrides (model, system prompt, tool list). Falls back to c.llmClient and
// no overrides when mode is empty or doesn't match a custom agent.
//
// MCP tools are appended to customTools automatically so custom agents get the
// same MCP access as standard modes.
func (c *Core) resolveCustomAgentOpts(mode string, agentLogger *llm.Logger) (customAgentOpts, error) {
	result := customAgentOpts{llmClient: c.llmClient}
	if c.cfg == nil || mode == "" {
		return result, nil
	}
	def := c.cfg.FindAgent(mode)
	if def == nil {
		return result, nil
	}

	result.systemPromptOverride = def.SystemPrompt

	// In test/DI mode (injected client), skip provider/model overrides so the
	// test client is preserved across custom agent runs.
	if !c.llmClientInjected {
		if def.Provider != "" {
			if provCfg, ok := c.cfg.FindProvider(def.Provider); ok {
				if def.Model != "" {
					provCfg.Model = def.Model
				}
				newClient := llm.NewClient(provCfg)
				if oc, ok2 := newClient.(*llm.OpenAIClient); ok2 && agentLogger != nil {
					oc.SetLogger(agentLogger)
				}
				result.llmClient = newClient
			} else {
				return result, fmt.Errorf("agent %q: provider %q not found in providers: section", def.Name, def.Provider)
			}
		} else if def.Model != "" {
			overrideCfg := c.cfg.LLM
			overrideCfg.Model = def.Model
			newClient := llm.NewClient(overrideCfg)
			if oc, ok := newClient.(*llm.OpenAIClient); ok && agentLogger != nil {
				oc.SetLogger(agentLogger)
			}
			result.llmClient = newClient
		}
	}

	if def.Tools != nil {
		defs, err := tools.ResolveToolNames(def.Tools)
		if err == nil {
			// C7 in audit ledger: only inject MCP tools when the custom agent
			// explicitly opts in via the `mcp:*` wildcard in its tools list.
			// Previously every MCP tool was appended unconditionally, so a
			// restricted "reviewer" agent declared as `[read, grep]` got the
			// whole MCP surface regardless. Opt-in semantics make the tool
			// list an actual allowlist.
			for _, name := range def.Tools {
				if name == "mcp:*" || name == "*" {
					defs = append(defs, c.mcpToolDefs()...)
					break
				}
			}
			result.customTools = defs
		}
	}

	return result, nil
}

// ── Session API ──────────────────────────────────────────────────────────────

type SessionStartParams struct {
	// SessionID optionally reopens an existing on-disk session (v2 snapshot).
	// When empty, core allocates a new sortable id.
	SessionID string `json:"session_id,omitempty"`
}

type SessionStartResult struct {
	SessionID string `json:"session_id"`
	Restored  bool   `json:"restored,omitempty"`
}

// SessionStart creates or reopens a session and returns its canonical id.
func (c *Core) SessionStart(params SessionStartParams) (*SessionStartResult, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	id := strings.TrimSpace(params.SessionID)
	var s *coresession.Session
	var restored bool
	if id != "" {
		var err error
		s, err = c.sessions.LoadOrCreate(c.workspaceRoot, id)
		if err != nil {
			return nil, protocol.NewError(protocol.ExecFailed, err.Error(), map[string]any{"session_id": id})
		}
		s.Lock()
		restored = len(s.History) > 0 || len(s.UIMessages()) > 0
		s.Unlock()
	} else {
		s = c.sessions.Create()
	}
	return &SessionStartResult{SessionID: s.ID, Restored: restored}, nil
}

type SessionGetParams struct {
	SessionID string `json:"session_id"`
}

type SessionGetResult struct {
	SessionID   string                   `json:"session_id"`
	Title       string                   `json:"title,omitempty"`
	Model       string                   `json:"model,omitempty"`
	UIMessages  []sessionfile.UIMessage  `json:"ui_messages"`
	HistoryLen  int                      `json:"history_len"`
	HasPending  bool                     `json:"has_pending,omitempty"`
	Restored    bool                     `json:"restored,omitempty"`
}

// SessionGet returns the unified v2 session view for TUI reopen.
func (c *Core) SessionGet(params SessionGetParams) (*SessionGetResult, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	if strings.TrimSpace(params.SessionID) == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "session_id is empty", nil)
	}
	sess, err := c.sessions.GetOrLoad(c.workspaceRoot, params.SessionID)
	if err != nil {
		return nil, protocol.NewError(protocol.ExecFailed, err.Error(), map[string]any{"session_id": params.SessionID})
	}
	sess.Lock()
	defer sess.Unlock()
	ui := sess.UIMessages()
	return &SessionGetResult{
		SessionID:  sess.ID,
		Title:      sess.Title(),
		Model:      sess.Model(),
		UIMessages: ui,
		HistoryLen: len(sess.History),
		HasPending: sess.HasPending(),
		Restored:   len(sess.History) > 0 || len(ui) > 0,
	}, nil
}

type SessionListParams struct{}

type SessionListResult struct {
	Sessions []sessionfile.Meta `json:"sessions"`
}

// SessionList returns session picker metadata from on-disk v2 snapshots.
func (c *Core) SessionList(_ SessionListParams) (*SessionListResult, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	metas, err := sessionfile.ListMeta(c.workspaceRoot)
	if err != nil {
		return nil, protocol.NewError(protocol.ExecFailed, err.Error(), nil)
	}
	if metas == nil {
		metas = []sessionfile.Meta{}
	}
	return &SessionListResult{Sessions: metas}, nil
}

type SessionUISyncParams struct {
	SessionID  string                  `json:"session_id"`
	Title      string                  `json:"title,omitempty"`
	Model      string                  `json:"model,omitempty"`
	UIMessages []sessionfile.UIMessage `json:"ui_messages"`
}

type SessionUISyncResult struct {
	SessionID string `json:"session_id"`
	Saved     bool   `json:"saved"`
}

// SessionUISync persists the TUI chat projection into the unified v2 snapshot.
func (c *Core) SessionUISync(params SessionUISyncParams) (*SessionUISyncResult, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	if strings.TrimSpace(params.SessionID) == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "session_id is empty", nil)
	}
	sess, err := c.sessions.GetOrLoad(c.workspaceRoot, params.SessionID)
	if err != nil {
		return nil, protocol.NewError(protocol.ExecFailed, err.Error(), map[string]any{"session_id": params.SessionID})
	}
	sess.Lock()
	sess.SetTitle(params.Title)
	if strings.TrimSpace(params.Model) != "" {
		sess.SetModel(params.Model)
	}
	sess.SetUIMessages(params.UIMessages)
	sess.LastActivity = time.Now()
	snapErr := sess.Snapshot(c.workspaceRoot)
	sess.Unlock()
	if snapErr != nil {
		return nil, protocol.NewError(protocol.ExecFailed, snapErr.Error(), map[string]any{"session_id": params.SessionID})
	}
	return &SessionUISyncResult{SessionID: params.SessionID, Saved: true}, nil
}

type SessionMessageParams struct {
	SessionID string `json:"session_id"`
	Content   string `json:"content"`

	Apply     bool `json:"apply,omitempty"`
	Backup    bool `json:"backup,omitempty"`
	AllowExec bool `json:"allow_exec,omitempty"`

	MaxSteps          int `json:"max_steps,omitempty"`
	MaxInvalidRetries int `json:"max_invalid_retries,omitempty"`
	MaxPromptBytes    int `json:"max_prompt_bytes,omitempty"`

	// Mode selects the agent mode or custom agent name (from agents: in .orchestra.yml).
	Mode string `json:"mode,omitempty"`

	// ApplyOutput / PatchPath / Profile mirror agent.run (see AgentRunParams).
	ApplyOutput string `json:"apply_output,omitempty"`
	PatchPath   string `json:"patch_path,omitempty"`
	Profile     string `json:"profile,omitempty"`

	// OnEvent is set programmatically by the RPC handler for streaming notifications.
	OnEvent func(method string, params any) `json:"-"`

	// PermissionRequester, if non-nil, is consulted before exec.run/bash runs.
	// Set programmatically by the RPC handler.
	PermissionRequester PermissionRequester `json:"-"`

	// QuestionAsker enables question tool and plan_exit approval in sessions.
	QuestionAsker tools.QuestionAsker `json:"-"`
}

type SessionMessageResult struct {
	Steps   int  `json:"steps"`
	Applied bool `json:"applied"`

	Patches       []patches.Patch     `json:"patches,omitempty"`
	Ops           []ops.AnyOp               `json:"ops,omitempty"`
	ApplyResponse *tools.FSApplyOpsResponse `json:"apply_response,omitempty"`

	SwitchToBuild bool             `json:"switch_to_build,omitempty"`
	Todos         []tools.TodoItem `json:"todos,omitempty"`
	PlanPath      string           `json:"plan_path,omitempty"`
	PatchPath     string           `json:"patch_path,omitempty"`

	// Usage summarises token consumption for this turn.
	Usage *UsageSnapshot `json:"usage,omitempty"`
}

// SessionMessage runs one agent turn in the named session, streaming events via OnEvent.
func (c *Core) SessionMessage(ctx context.Context, params SessionMessageParams) (*SessionMessageResult, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	if strings.TrimSpace(params.SessionID) == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "session_id is empty", nil)
	}
	if strings.TrimSpace(params.Content) == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "content is empty", nil)
	}

	applyOutput := strings.ToLower(strings.TrimSpace(params.ApplyOutput))
	if applyOutput == "" && c.cfg != nil {
		applyOutput = strings.ToLower(strings.TrimSpace(c.cfg.Apply.Output))
	}
	if applyOutput == "" {
		applyOutput = config.ApplyOutputDisk
	}
	if applyOutput != config.ApplyOutputDisk && applyOutput != config.ApplyOutputPatch {
		return nil, protocol.NewError(protocol.InvalidParams,
			fmt.Sprintf("apply_output must be %q or %q", config.ApplyOutputDisk, config.ApplyOutputPatch), nil)
	}
	if applyOutput == config.ApplyOutputPatch {
		if params.Apply {
			return nil, protocol.NewError(protocol.InvalidParams,
				"apply_output=patch is mutually exclusive with apply=true", nil)
		}
		params.Apply = false
		params.Backup = false
	}
	profileName := strings.TrimSpace(params.Profile)
	if profileName == "" && c.cfg != nil {
		profileName = strings.TrimSpace(c.cfg.Agent.Profile)
	}
	if !agent.IsKnownProfile(profileName) {
		return nil, protocol.NewError(protocol.InvalidParams,
			fmt.Sprintf("unknown profile %q (want fast|precision)", profileName), nil)
	}

	sess, err := c.sessions.GetOrLoad(c.workspaceRoot, params.SessionID)
	if err != nil {
		return nil, protocol.NewError(protocol.ExecFailed, err.Error(), map[string]any{"session_id": params.SessionID})
	}

	// Prevent concurrent turns on the same session.
	sess.Lock()
	if sess.IsBusy() {
		sess.Unlock()
		return nil, protocol.NewError(protocol.ExecFailed, "session is busy", map[string]any{"session_id": params.SessionID})
	}
	// Snapshot history and todos for this turn (under lock).
	inHistory := sess.CopyHistory()
	inTodos := sess.CopyTodos()
	planPath := sessionPlanPathLocked(sess, params.Mode)
	// Create a cancellable context for this turn and store its cancel in the session.
	turnCtx, cancel := context.WithCancel(ctx)
	sess.SetCancel(cancel)
	sess.Unlock()

	// Ensure cancel and session state are cleaned up on exit.
	defer func() {
		sess.Lock()
		sess.ClearCancel()
		sess.Unlock()
		cancel()
	}()

	// Same staging contract as AgentRun: serialise shared Runner mutations
	// (SetDryRun, ClearStaged, staged overlay writes) for the whole turn.
	c.runMu.Lock()
	defer c.runMu.Unlock()
	c.tools.SetDryRun(!params.Apply)
	c.tools.ClearStaged()

	// Merge params with config defaults.
	agParams := AgentRunParams{
		Query:               params.Content,
		Apply:               params.Apply,
		Backup:              params.Backup,
		AllowExec:           params.AllowExec,
		MaxSteps:            params.MaxSteps,
		MaxInvalidRetries:   params.MaxInvalidRetries,
		MaxPromptBytes:      params.MaxPromptBytes,
		OnEvent:             params.OnEvent,
		PermissionRequester: params.PermissionRequester,
		QuestionAsker:       params.QuestionAsker,
	}

	// Build and run the agent (same setup as AgentRun).
	var respFmt *llm.ResponseFormat
	if c.cfg != nil && c.cfg.LLM.ResponseFormatType != "" {
		respFmt = &llm.ResponseFormat{Type: c.cfg.LLM.ResponseFormatType}
		if c.cfg.LLM.ResponseFormatType == "json_schema" {
			respFmt.Schema = schema.AgentStepSchemaRaw()
			respFmt.SchemaName = "agent_step"
		}
	}
	maxSteps := agParams.MaxSteps
	if maxSteps <= 0 && c.cfg != nil {
		maxSteps = c.cfg.Agent.MaxSteps
	}
	maxRetries := agParams.MaxInvalidRetries
	if maxRetries <= 0 && c.cfg != nil {
		maxRetries = c.cfg.Agent.MaxInvalidRetries
	}
	maxPromptBytes := agParams.MaxPromptBytes
	if maxPromptBytes <= 0 && c.cfg != nil {
		maxPromptBytes = c.cfg.Limits.ContextKB * 1024
	}
	promptFamily := ""
	if c.cfg != nil {
		promptFamily = promptpkg.ResolvePromptFamily(c.cfg.LLM.PromptFamily, c.cfg.LLM.Model)
	}

	var onEvent func(agent.AgentEvent)
	if agParams.OnEvent != nil {
		onEvent = buildAgentOnEvent(agParams.OnEvent, EventEnvelope{
			SessionID: params.SessionID,
			TurnID:    NewTurnID(),
		})
	}

	sessAllowExec := agParams.AllowExec
	if c.cfg != nil && c.cfg.Exec.Confirm != nil && !*c.cfg.Exec.Confirm {
		sessAllowExec = true
	}
	var sessExecAllow, sessExecDeny []string
	if c.cfg != nil {
		sessExecAllow = c.cfg.Exec.Allow
		sessExecDeny = c.cfg.Exec.Deny
	}

	var sessAgentLogger *llm.Logger
	if c.llmClient != nil {
		if oc, ok := c.llmClient.(*llm.OpenAIClient); ok {
			sessAgentLogger = oc.GetLogger()
		}
	}

	var sessHooksRunner agent.HooksRunner
	if hr := hooks.New(c.cfg.Hooks, c.workspaceRoot); hr != nil {
		sessHooksRunner = hr
	}

	sessCustomOpts, err := c.resolveCustomAgentOpts(params.Mode, sessAgentLogger)
	if err != nil {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, err.Error(), nil)
	}

	sessUsageTracker := newAgentUsageTracker(c.cfg, "session.turn")
	sessTaskRunner := tasks.New(c.llmClient, c.validator, c.tools, childAgentConfig(c.cfg, maxPromptBytes, sessUsageTracker))

	sessAgOpts := agent.Options{
		UsageTracker:         sessUsageTracker,
		ProviderLabel:        providerLabelOf(c.cfg),
		ModelLabel:           c.cfg.LLM.Model,
		MaxSteps:             maxSteps,
		MaxInvalidRetries:    maxRetries,
		MaxDeniedToolRepeats: c.cfg.Agent.MaxDeniedRepeats,
		MaxToolErrorRepeats:  c.cfg.Agent.MaxToolErrors,
		MaxFinalFailures:     c.cfg.Agent.MaxFinalFailures,
		MaxPromptBytes:       maxPromptBytes,
		CompactThresholdPct:  c.cfg.Agent.CompactThresholdPct,
		LLMStepTimeout:       time.Duration(c.cfg.LLM.TimeoutS) * time.Second,
		Apply:                agParams.Apply,
		Backup:               agParams.Backup,
		AllowExec:            sessAllowExec,
		ExecAllow:            sessExecAllow,
		ExecDeny:             sessExecDeny,
		PermissionRules:      c.cfg.Permissions.Rules,
		InitialTodos:         inTodos,
		Debug:                c.debug,
		ResponseFormat:       respFmt,
		PromptFamily:         promptFamily,
		Mode:                 agent.Mode(params.Mode),
		SystemPromptOverride: sessCustomOpts.systemPromptOverride,
		CustomTools:          sessCustomOpts.customTools,
		OnEvent:              onEvent,
		AgentLogger:          sessAgentLogger,
		SubtaskRunner:        sessTaskRunner,
		HooksRunner:          sessHooksRunner,
		ExtraTools:           c.mcpToolDefs(),
		PermissionRequester:  convertPermissionRequester(agParams.PermissionRequester),
		QuestionAsker:        agParams.QuestionAsker,
		PlanPath:             planPath,
		Memory:               c.cfg.Memory.Resolve(),
		SessionID:            params.SessionID,
		ToolDigestBytes:        c.cfg.Agent.ResolvedToolDigestBytes(),
		HistoryPruneKeepRecent: c.cfg.Agent.ResolvedHistoryPruneKeepRecent(),
		AutoSessionMemory:    c.cfg.Agent.ResolvedAutoSessionMemory(),
	}
	c.tools.SetMemoryContext(params.SessionID, c.cfg.Memory.Resolve())
	if err := agent.ApplyProfile(&sessAgOpts, profileName, false); err != nil {
		return nil, protocol.NewError(protocol.InvalidParams, err.Error(), nil)
	}
	if sessCustomOpts.systemPromptOverride != "" {
		sessAgOpts.SystemPromptOverride = sessCustomOpts.systemPromptOverride
	}
	if sessCustomOpts.customTools != nil {
		sessAgOpts.CustomTools = sessCustomOpts.customTools
	}
	ag, err := agent.New(sessCustomOpts.llmClient, c.validator, c.tools, sessAgOpts)
	if err != nil {
		return nil, err
	}

	outHistory, res, err := ag.Run(turnCtx, inHistory, params.Content)
	if err != nil {
		return nil, err
	}
	outHistory, res, err = maybeContinueBuildAfterPlan(turnCtx, sessCustomOpts.llmClient, c.validator, c.tools, sessAgOpts, outHistory, res)
	if err != nil {
		return nil, err
	}
	finalizeAgentUsage(sessUsageTracker, c.workspaceRoot)

	// Update session history, todos, and pending-ops with this turn's
	// results, then snapshot to disk so a core restart can pick up where
	// we left off. C1+C4 in architecture audit consolidated this into
	// one critical section.
	newMsgs := outHistory[len(inHistory):]
	sess.Lock()
	if len(newMsgs) > 0 {
		sess.AppendHistory(newMsgs)
	}
	if res != nil {
		sess.SetTodos(res.Todos)
	}
	if planPath != "" {
		sess.SetPlanPath(planPath)
	}
	if profileName != "" {
		sess.SetProfile(profileName)
	}
	if applyOutput != "" {
		sess.SetApplyOutput(applyOutput)
	}
	if !params.Apply && len(res.Ops) > 0 {
		sess.SetPending(res.Ops)
	} else {
		sess.SetPending(nil)
	}
	if snapErr := sess.Snapshot(c.workspaceRoot); snapErr != nil {
		// Best effort — log but don't fail the request. A failed snapshot
		// only costs the in-progress crash-recovery story for this turn;
		// the result itself is still valid.
		fmt.Fprintf(os.Stderr, "core: session %s snapshot failed: %v\n", sess.ID, snapErr)
	}
	sess.Unlock()

	out := &SessionMessageResult{
		Steps:         res.Steps,
		Applied:       res.Applied,
		Patches:       res.Patches,
		Ops:           res.Ops,
		ApplyResponse: res.ApplyResponse,
		SwitchToBuild: res.SwitchToBuild,
		Todos:         res.Todos,
		PlanPath:      planPath,
		Usage:         usageSnapshotFrom(sessUsageTracker),
	}
	if applyOutput == config.ApplyOutputPatch {
		path, werr := c.writeAgentPatch(params.PatchPath, res)
		if werr != nil {
			return nil, protocol.NewError(protocol.ExecFailed, werr.Error(), nil)
		}
		out.PatchPath = path
		out.Applied = false
	}
	return out, nil
}

type SessionApplyPendingParams struct {
	SessionID string `json:"session_id"`
	Backup    bool   `json:"backup,omitempty"`
}

type SessionApplyPendingResult struct {
	Applied       bool                      `json:"applied"`
	ApplyResponse *tools.FSApplyOpsResponse `json:"apply_response,omitempty"`
}

// SessionApplyPending applies ops stored from the last dry-run turn of the session.
func (c *Core) SessionApplyPending(ctx context.Context, params SessionApplyPendingParams) (*SessionApplyPendingResult, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	if strings.TrimSpace(params.SessionID) == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "session_id is empty", nil)
	}
	sess, err := c.sessions.GetOrLoad(c.workspaceRoot, params.SessionID)
	if err != nil {
		return nil, protocol.NewError(protocol.ExecFailed, err.Error(), map[string]any{"session_id": params.SessionID})
	}

	sess.Lock()
	pendingOps := sess.TakePending()
	sess.Unlock()

	if len(pendingOps) == 0 {
		return &SessionApplyPendingResult{Applied: false}, nil
	}

	c.runMu.Lock()
	defer c.runMu.Unlock()
	resp, err := c.tools.FSApplyOps(ctx, tools.FSApplyOpsRequest{
		Ops:    pendingOps,
		Backup: params.Backup,
	})
	if err != nil {
		// Restore pending so the user can retry. Prepend original ops to any
		// newer ops that a concurrent turn may have added while we were applying.
		sess.Lock()
		newer := sess.TakePending()
		sess.SetPending(append(pendingOps, newer...))
		sess.Unlock()
		return nil, protocol.NewError(protocol.ExecFailed, err.Error(), nil)
	}
	return &SessionApplyPendingResult{Applied: true, ApplyResponse: resp}, nil
}

type SessionHistoryParams struct {
	SessionID string `json:"session_id"`
}

type SessionHistoryResult struct {
	SessionID string        `json:"session_id"`
	Messages  []llm.Message `json:"messages"`
}

// SessionHistory returns the accumulated history for a session.
func (c *Core) SessionHistory(params SessionHistoryParams) (*SessionHistoryResult, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	sess, err := c.sessions.GetOrLoad(c.workspaceRoot, params.SessionID)
	if err != nil {
		return nil, protocol.NewError(protocol.ExecFailed, err.Error(), map[string]any{"session_id": params.SessionID})
	}
	sess.Lock()
	msgs := sess.CopyHistory()
	sess.Unlock()
	return &SessionHistoryResult{SessionID: params.SessionID, Messages: msgs}, nil
}

type SessionCancelParams struct {
	SessionID string `json:"session_id"`
}

// SessionCancel cancels the currently running turn in a session (no-op if idle).
func (c *Core) SessionCancel(params SessionCancelParams) error {
	if c == nil {
		return protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	sess, err := c.sessions.Get(params.SessionID)
	if err != nil {
		return protocol.NewError(protocol.ExecFailed, err.Error(), map[string]any{"session_id": params.SessionID})
	}
	sess.Cancel()
	return nil
}

type SessionCloseParams struct {
	SessionID string `json:"session_id"`
}

// SessionClose cancels any running turn and removes the session.
func (c *Core) SessionClose(params SessionCloseParams) error {
	if c == nil {
		return protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	if sess, err := c.sessions.Get(params.SessionID); err == nil {
		sess.Cancel()
		c.sessions.Delete(params.SessionID)
	}
	// Remove any on-disk snapshot too — close is idempotent across
	// memory and disk, otherwise "closed" sessions would resurrect on
	// the next core restart.
	if err := coresession.DeleteSnapshot(c.workspaceRoot, params.SessionID); err != nil {
		fmt.Fprintf(os.Stderr, "core: session %s snapshot delete failed: %v\n", params.SessionID, err)
	}
	return nil
}

// OpsApplyParams holds the ops to apply.
type OpsApplyParams struct {
	Ops    []ops.AnyOp `json:"ops"`
	Backup bool        `json:"backup"`
}

// OpsApplyResult reports the result of applying pending ops.
type OpsApplyResult struct {
	Applied      bool     `json:"applied"`
	ChangedFiles []string `json:"changed_files"`
}

// OpsApply applies a list of internal ops against the workspace.
// It is intended for TUI "confirm apply" flows: the client received the ops
// via a pending_ops event, user confirmed, and now sends them back to apply.
func (c *Core) OpsApply(ctx context.Context, p OpsApplyParams) (*OpsApplyResult, error) {
	if !c.IsInitialized() {
		return nil, protocol.NewError(protocol.NotInitialized, "initialize required", nil)
	}
	req := tools.FSApplyOpsRequest{
		Ops:    p.Ops,
		DryRun: false,
		Backup: p.Backup,
	}
	c.runMu.Lock()
	defer c.runMu.Unlock()
	resp, err := c.tools.FSApplyOps(ctx, req)
	if err != nil {
		return nil, err
	}
	return &OpsApplyResult{
		Applied:      resp.Applied,
		ChangedFiles: resp.ChangedFiles,
	}, nil
}

// convertPermissionRequester is a no-op identity now that both core
// and agent share the same Requester type (internal/permission). Kept
// as a thin wrapper so existing call sites don't all change at once.
// H6 in architecture audit eliminated the previous adapter pattern.
func convertPermissionRequester(r PermissionRequester) agent.PermissionRequester {
	return r
}

func samePath(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func childAgentConfig(cfg *config.ProjectConfig, maxPromptBytes int, usage agent.UsageRecorder) tasks.ChildAgentConfig {
	out := tasks.ChildAgentConfig{
		MaxPromptBytes: maxPromptBytes,
		UsageTracker:   usage,
	}
	if cfg == nil {
		return out
	}
	out.CompactThresholdPct = cfg.Agent.CompactThresholdPct
	out.ToolDigestBytes = cfg.Agent.ResolvedToolDigestBytes()
	out.HistoryPruneKeepRecent = cfg.Agent.ResolvedHistoryPruneKeepRecent()
	out.ProviderLabel = providerLabelOf(cfg)
	out.ModelLabel = cfg.LLM.Model
	return out
}
