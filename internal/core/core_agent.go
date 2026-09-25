package core

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/app"
	"github.com/orchestra/orchestra/internal/checkpoint"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/retention"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/internal/usage"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/patch/applier"
	"github.com/orchestra/orchestra/patch/ops"
	"github.com/orchestra/orchestra/patch/patches"
	"github.com/orchestra/orchestra/protocol"
	"github.com/orchestra/orchestra/protocol/wire"
)

type AgentRunParams struct {
	Query string `json:"query"`

	Apply  bool `json:"apply,omitempty"`
	Backup bool `json:"backup,omitempty"`

	MaxSteps          int `json:"max_steps,omitempty"`
	MaxInvalidRetries int `json:"max_invalid_retries,omitempty"`
	MaxPromptBytes    int `json:"max_prompt_bytes,omitempty"`

	AllowExec bool `json:"allow_exec,omitempty"`
	// AllowBrowser gives the turn browser.* tools (ProtocolVersion 19).
	AllowBrowser bool `json:"allow_browser,omitempty"`
	// BrowserPanel says the client has a browser view of its own open and
	// this turn may use it: browser.* then act on the page the person is
	// looking at, through the client, instead of on a browser started here.
	// Ignored without allow_browser, and by a client that never answers
	// "browser/call". (ProtocolVersion 21.)
	BrowserPanel bool `json:"browser_panel,omitempty"`
	// AllowBrowserDrive lets those tools act on the panel rather than only
	// read it: navigate, click, type, fill, select. Off by default, because
	// looking at the page a person has open and pressing things on it are
	// different acts against their session. Panel only — a browser the core
	// starts for itself is empty and ours. (ProtocolVersion 21.)
	AllowBrowserDrive bool `json:"allow_browser_drive,omitempty"`
	// AllowBrowserEval lets browser.eval run the model's own script in the
	// panel. Its own switch, off even when driving: arbitrary code with the
	// person's cookies is stronger than every other op together.
	// (ProtocolVersion 21.)
	AllowBrowserEval bool `json:"allow_browser_eval,omitempty"`
	Debug            bool `json:"debug,omitempty"`

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

	// Attachments are optional files/images for multimodal turns.
	Attachments []MessageAttachment `json:"attachments,omitempty"`

	// Resume continues a run its core did not finish — a crash, a kill — from
	// its checkpoint: a run_id, or "last" for the most recent one that can
	// be resumed. The run's query, mode and apply are the checkpoint's; the
	// request gives only the consent (allow_exec, allow_web, browser).
	// (ProtocolVersion 23.)
	Resume string `json:"resume,omitempty"`

	// The fields below are for a core run in-process (`orchestra apply`) and
	// never cross the wire.

	// AllowWeb is web consent given for this run (--allow-web). Over RPC the
	// only web consent is web.confirm: false in the config.
	AllowWeb bool `json:"-"`
	// OnAgentEvent receives the turn's own agent events as they are, beside
	// the notifications OnEvent gets: the terminal renders these.
	OnAgentEvent func(agent.AgentEvent) `json:"-"`
	// UserImages are images already loaded for the turn (--image).
	UserImages []llm.ContentPart `json:"-"`
}

type AgentRunResult struct {
	// RunID names the run: its log (.orchestra/runs/<run_id>.events.jsonl)
	// and its checkpoint, which agent.run resume takes. (ProtocolVersion 23.)
	RunID string `json:"run_id,omitempty"`

	Steps   int  `json:"steps"`
	Applied bool `json:"applied"`

	Patches []patches.Patch `json:"patches,omitempty"`
	Ops     []ops.AnyOp     `json:"ops,omitempty"`

	ApplyResponse *tools.FSApplyOpsResponse `json:"apply_response,omitempty"`

	// PatchPath is set when apply_output=patch and a unified diff was written.
	PatchPath string `json:"patch_path,omitempty"`

	// SwitchToBuild is true when plan_exit requested a build continuation
	// that was not completed in this response (legacy; normally handled in-core).
	SwitchToBuild bool `json:"switch_to_build,omitempty"`

	Todos []tools.TodoItem `json:"todos,omitempty"`

	// PlanPath is the plan markdown file this run WROTE. Empty when the run
	// produced no plan — including a plan-mode run that explored and stopped
	// without planning, which is a thing local models do.
	//
	// It used to carry the path the run was configured with, which exists for
	// every plan/architecture/orchestra run whether or not a plan was ever
	// written. A caller then had a path to a file that was never created: the
	// eval harness reported "cannot find the path specified" for a run whose
	// real story was that the model never called plan_exit, and a UI offering
	// to open the plan would open nothing.
	PlanPath string `json:"plan_path,omitempty"`

	// Usage summarises token consumption for this run.
	// did not return usage info (some local servers omit it).
	Usage *UsageSnapshot `json:"usage,omitempty"`

	// EffectiveMode is the mode actually used after auto-router (mode=agent).
	EffectiveMode string `json:"effective_mode,omitempty"`
	// RoutedFrom is set when RequestedMode was agent and routing rewrote the mode.
	RoutedFrom string `json:"routed_from,omitempty"`
}

// UsageSnapshot is the turn's token spend as agent.run and session.message
// answer it: the wire's Usage (protocol/wire).
type UsageSnapshot = wire.Usage

func (c *Core) AgentRun(ctx context.Context, params AgentRunParams) (*AgentRunResult, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	// A resumed run is the run its checkpoint recorded: its query, mode and
	// apply. The request brings only the consent (checkpoint.go).
	var resumed *checkpoint.Checkpoint
	if ref := strings.TrimSpace(params.Resume); ref != "" {
		cp, err := c.loadResumable(ref)
		if err != nil {
			return nil, err
		}
		var rp resumableParams
		if err := json.Unmarshal(cp.Params, &rp); err != nil {
			return nil, protocol.NewError(protocol.InvalidParams, fmt.Sprintf("resume %q: %v", cp.RunID, err), nil)
		}
		params = rp.applyTo(params)
		resumed = cp
	}
	if strings.TrimSpace(params.Query) == "" && len(params.Attachments) == 0 {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "query is empty", nil)
	}
	if err := validateTurnInput(params.Query, params.Attachments); err != nil {
		return nil, err
	}

	imageParts, err := loadAttachmentImages(c.cfg, c.workspaceRoot, params.Attachments)
	if err != nil {
		return nil, err
	}
	imageParts = append(imageParts, params.UserImages...)
	agentQuery := resolveTurnQuery(params.Query, params.Attachments, c.cfg != nil && c.cfg.LLM.Multimodal)
	agentQuery = enrichQueryWithImageHints(agentQuery, params.Attachments)
	if err := c.validateTurnMode(params.Mode); err != nil {
		return nil, err
	}

	applyOutput, err := resolveApplyOutput(c.cfg, params.ApplyOutput, &params.Apply, &params.Backup)
	if err != nil {
		return nil, err
	}

	runID := NewTurnID()
	if resumed != nil {
		runID = resumed.RunID
	} else {
		checkpoint.Prune(c.workspaceRoot, checkpoint.MaxKeep-1)
	}
	ck := newTurnCheckpoint(c.workspaceRoot, runID, resumableParams{
		Query: agentQuery, Mode: params.Mode, Profile: params.Profile,
		Apply: params.Apply, Backup: params.Backup, ApplyOutput: params.ApplyOutput, PatchPath: params.PatchPath,
		MaxSteps: params.MaxSteps, MaxInvalidRetries: params.MaxInvalidRetries, MaxPromptBytes: params.MaxPromptBytes,
	}, c.tools)
	if resumed != nil {
		ck.resumeFrom(resumed)
	}

	launch, err := c.prepareAgentLaunch(ctx, agentLaunchSpec{
		Mode:                params.Mode,
		Profile:             params.Profile,
		Query:               agentQuery,
		Apply:               params.Apply,
		Backup:              params.Backup,
		AllowExec:           params.AllowExec,
		AllowWeb:            params.AllowWeb,
		AllowBrowser:        params.AllowBrowser,
		Debug:               params.Debug || c.debug,
		MaxSteps:            params.MaxSteps,
		MaxInvalidRetries:   params.MaxInvalidRetries,
		MaxPromptBytes:      params.MaxPromptBytes,
		AutoSessionMemory:   false,
		UsageLabel:          "agent.run",
		RecordRun:           true,
		OnEvent:             params.OnEvent,
		OnAgentEvent:        params.OnAgentEvent,
		EventEnvelope:       EventEnvelope{TurnID: runID},
		PermissionRequester: params.PermissionRequester,
		QuestionAsker:       params.QuestionAsker,
		Attachments:         params.Attachments,
		UserImages:          imageParts,
		Multimodal:          len(imageParts) > 0,
		OnGraphChange:       ck.graphChanged,
	})
	if err != nil {
		return nil, err
	}
	// The mode the router chose is the run's: a resumed run does not route
	// again.
	ck.setMode(launch.EffectiveMode)
	ck.setTasks(launch.TaskRunner)
	launch.Opts.OnStepHistory = ck.stepHistory

	// Semantic dry-run pipeline: edit/write always go through staging + LSP
	// during the turn. params.Apply controls commit-to-disk at end of turn,
	// not per-tool live writes. apply:true also unlocks bash (staging would
	// otherwise keep dryRun=true and BlockExecInDryRun would deny shell).
	c.runMu.Lock()
	defer c.runMu.Unlock()
	c.tools.SetDryRun(true)
	c.tools.ClearStaged()
	c.tools.SetAllowExecDespiteDryRun(params.Apply)
	defer c.tools.SetAllowExecDespiteDryRun(false)
	c.tools.SetCommitsToDisk(params.Apply)
	defer c.tools.SetCommitsToDisk(false)
	// Registered after the lock and the runner flags so it runs before them
	// (defers are LIFO): children still running stop while this turn owns the
	// runner, not after the next turn has taken it.
	defer launch.Close()

	ag, err := agent.New(launch.Custom.llmClient, c.validator, c.tools, launch.Opts)
	if err != nil {
		return nil, err
	}

	ctx = launch.RunContext(ctx)
	var history []llm.Message
	if resumed != nil {
		history = restoreRun(ctx, resumed, c.tools, launch.TaskRunner)
	}
	ck.save()
	outHistory, res, err := ag.Run(ctx, history, agentQuery)
	if err == nil {
		_, res, err = maybeContinueBuildAfterPlan(ctx, launch.Custom.llmClient, c.validator, c.tools, launch.Opts, outHistory, res)
	}
	ck.finish(err)
	if err != nil {
		return nil, err
	}
	finalizeAgentUsage(launch.Usage, c.workspaceRoot)

	result := &AgentRunResult{
		RunID:         runID,
		Steps:         res.Steps,
		Applied:       res.Applied,
		Patches:       res.Patches,
		Ops:           res.Ops,
		ApplyResponse: res.ApplyResponse,
		SwitchToBuild: res.SwitchToBuild,
		Todos:         res.Todos,
		PlanPath:      writtenPlanPath(c.workspaceRoot, launch.Opts.PlanPath),
		Usage:         usageSnapshotFrom(launch.Usage),
		EffectiveMode: launch.EffectiveMode,
	}
	if strings.EqualFold(launch.RequestedMode, string(agent.ModeAgent)) && launch.EffectiveMode != "" {
		result.RoutedFrom = string(agent.ModeAgent)
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
	// The export directory keeps the newest cfg.retention.patches files; an
	// explicit path is the caller's and its directory is left alone.
	if strings.TrimSpace(explicit) == "" {
		keep := config.DefaultRetentionPatches
		if c.cfg != nil {
			keep = c.cfg.Retention.Patches
		}
		retention.PruneFiles(filepath.Dir(path), ".patch", keep)
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
	t := usage.NewTracker(runID, label, pricing)
	if cfg != nil {
		t.UseCatalogPrices(cfg.LLM.APIBase)
	}
	return t
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
	sum := t.Totals()
	return &UsageSnapshot{
		Calls:              sum.Calls,
		PromptTokens:       sum.PromptTokens,
		CompletionTokens:   sum.CompletionTokens,
		TotalTokens:        sum.TotalTokens,
		CostUSD:            sum.CostUSD,
		CachedPromptTokens: sum.CachedPromptTokens,
		CacheWriteTokens:   sum.CacheWriteTokens,
		Entries:            usageEntries(t.Snapshot()),
	}
}

// usageEntries puts the tracker's per-(provider, model) rows on the wire.
func usageEntries(in []usage.Entry) []wire.UsageEntry {
	if len(in) == 0 {
		return nil
	}
	out := make([]wire.UsageEntry, len(in))
	for i, e := range in {
		out[i] = wire.UsageEntry{
			Provider:           e.Provider,
			Model:              e.Model,
			Calls:              e.Calls,
			PromptTokens:       e.PromptTokens,
			CompletionTokens:   e.CompletionTokens,
			TotalTokens:        e.TotalTokens,
			CostUSD:            e.CostUSD,
			CachedPromptTokens: e.CachedPromptTokens,
			CacheWriteTokens:   e.CacheWriteTokens,
		}
	}
	return out
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
				Command string   `json:"command"`
				Args    []string `json:"args"`
			}
			_ = json.Unmarshal(params.Input, &execReq)
			if !c.cfg.Exec.AllowsCommand(execReq.Command, execReq.Args) {
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

	// Web tools reach the network on the caller's behalf, held to web.confirm
	// the way bash is held to exec.confirm.
	if (canonicalName == "webfetch" || canonicalName == "websearch") && c.cfg != nil &&
		(c.cfg.Web.Confirm == nil || *c.cfg.Web.Confirm) {
		return nil, protocol.NewError(protocol.ExecDenied,
			canonicalName+" requires user consent (set web.confirm: false in .orchestra.yml)",
			map[string]any{"tool": params.Name})
	}

	// tool.call carries no allow_browser and belongs to no run whose consent
	// could be checked, so the browser stays with agent runs given it.
	if strings.HasPrefix(canonicalName, "browser.") {
		return nil, protocol.NewError(protocol.ExecDenied,
			"browser tools are not available through tool.call: they run inside agent.run, session.message, skill.invoke or workflow.run with allow_browser: true",
			map[string]any{"tool": params.Name})
	}

	return c.tools.Call(ctx, params.Name, params.Input)
}

// Close releases resources held by Core (tools.Runner, MCP manager).
// Safe to call multiple times.
//
// The runner is closed, not nil-ed: an RPC already past its checks (an
// ops.apply waiting on runMu) reads c.tools without a lock, and Close
// setting it to nil under it was both a data race and a nil dereference
// that took the process down. A closed runner is inert.
func (c *Core) Close() error {
	c.closeOnce.Do(func() {
		c.warm.stop()
		if c.limitsCancel != nil {
			c.limitsCancel()
		}
		if c.mcpManager != nil {
			c.mcpManager.Close()
		}
		if c.tools != nil {
			if err := c.tools.Close(); err != nil {
				c.closeErr = fmt.Errorf("close tools runner: %w", err)
			}
		}
	})
	return c.closeErr
}

// mcpToolDefs returns MCP tool definitions if a manager is active.
func (c *Core) mcpToolDefs() []llm.ToolDef {
	if c.mcpManager == nil || c.mcpManager.IsEmpty() {
		return nil
	}
	return c.mcpManager.ListToolDefs()
}

// extraToolDefs is the ExtraTools slice for agent runs: MCP tools plus
// optional feature tools gated by config (parity with `orchestra apply`).
func (c *Core) extraToolDefs() []llm.ToolDef {
	out := c.mcpToolDefs()
	if c == nil || c.cfg == nil {
		return out
	}
	// semantic_search needs an embedding model; the Runner already has
	// embedCfg + ckgStore from New. Empty index returns zero hits, not a crash.
	if strings.TrimSpace(c.cfg.ResolvedEmbed().Model) != "" {
		out = append(out, tools.ToolSemanticSearch())
	}
	// repo_map is always safe (tree-sitter outline, no network).
	out = append(out, tools.ToolRepoMap())
	return out
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
// resolveCustomAgentOpts applies an agents: entry named by mode: its prompt,
// its model, and its tools — those the turn's consent (caps) allows; a tool it
// names and may not use is left off the list rather than offered and refused.
func (c *Core) resolveCustomAgentOpts(mode string, caps tools.Capabilities, agentLogger *llm.Logger) (customAgentOpts, error) {
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
	if !c.llmClientInjected && (def.Provider != "" || def.Model != "") {
		client, _, err := app.ClientFor(c.cfg, def.Provider, def.Model, agentLogger)
		if err != nil {
			return result, fmt.Errorf("agent %q: %w", def.Name, err)
		}
		result.llmClient = client
	}

	if def.Tools != nil {
		defs, err := tools.ResolveToolNamesWithPolicy(def.Tools, caps)
		if err != nil {
			return result, fmt.Errorf("agent %q: %w", def.Name, err)
		}
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

	return result, nil
}

// validateTurnMode refuses a mode a turn cannot run in: a subagent-only mode
// (worker, verifier, scout, compaction…), or a name that is neither built in
// nor an agents: entry. Empty is the default mode.
//
// agent.run checked this and session.message did not (LLM-10): from the TUI
// or VS Code a worker or compaction ran as a whole turn, and a typo silently
// became build.
func (c *Core) validateTurnMode(mode string) error {
	if mode == "" {
		return nil
	}
	if kind, builtIn := config.BuiltInModeKind(mode); builtIn {
		if kind != config.ModeKindTopLevel {
			return protocol.NewError(protocol.InvalidLLMOutput,
				fmt.Sprintf("agent mode %q runs only as a subagent (spawned via task / task_spawn); available modes: %s",
					mode, strings.Join(config.UserSelectableModeNames(), ", ")), nil)
		}
		return nil
	}
	if c.cfg != nil && c.cfg.FindAgent(mode) == nil {
		return protocol.NewError(protocol.InvalidLLMOutput,
			fmt.Sprintf("unknown agent mode %q: not a built-in mode and not defined in agents: in .orchestra.yml", mode), nil)
	}
	return nil
}

// The wire types of this file live in protocol/wire (ARCH-4); the aliases
// keep the package's names.
type (
	ToolCallParams = wire.ToolCallParams
)
