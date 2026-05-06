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
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/patches"
	"github.com/orchestra/orchestra/internal/llm"
	"github.com/orchestra/orchestra/internal/ops"
	"github.com/orchestra/orchestra/internal/protocol"
	"github.com/orchestra/orchestra/internal/schema"
	"github.com/orchestra/orchestra/internal/cache"
	"github.com/orchestra/orchestra/internal/tools"

	coresession "github.com/orchestra/orchestra/internal/core/session"
	"github.com/orchestra/orchestra/internal/hooks"
	"github.com/orchestra/orchestra/internal/mcp"
	"github.com/orchestra/orchestra/internal/tasks"
)

type Core struct {
	workspaceRoot string
	projectID     string
	debug         bool
	initMu        sync.Mutex
	initialized   bool
	initParams    *InitializeParams

	cfg       *config.ProjectConfig
	llmClient llm.Client

	validator  *schema.Validator
	tools      *tools.Runner
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
		ExcludeDirs:     cfg.ExcludeDirs,
		ExecTimeout:     time.Duration(cfg.Exec.TimeoutS) * time.Second,
		ExecOutputLimit: cfg.Exec.OutputLimitKB * 1024,
		LSP:             cfg.LSP,
	})
	if err != nil {
		return nil, err
	}

	llmClient := opts.LLMClient
	if llmClient == nil {
		switch strings.ToLower(cfg.LLM.Provider) {
		case "anthropic":
			llmClient = llm.NewAnthropicClient(cfg.LLM)
		default:
			oc := llm.NewOpenAIClient(cfg.LLM)
			logger := llm.NewLogger(rootAbs)
			oc.SetLogger(logger)
			llmClient = oc
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

	return &Core{
		workspaceRoot: rootAbs,
		projectID:     projectID,
		debug:         opts.Debug,
		cfg:           cfg,
		llmClient:     llmClient,
		validator:     v,
		tools:         tr,
		sessions:      coresession.NewManager(),
		mcpManager:    mcpMgr,
	}, nil
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

	// OnEvent is called for each agent streaming event (method + params).
	// Not serialized — set programmatically by the RPC handler.
	OnEvent func(method string, params any) `json:"-"`
}

type AgentRunResult struct {
	Steps   int  `json:"steps"`
	Applied bool `json:"applied"`

	Patches []patches.Patch `json:"patches,omitempty"`
	Ops     []ops.AnyOp           `json:"ops,omitempty"`

	ApplyResponse *tools.FSApplyOpsResponse `json:"apply_response,omitempty"`
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
		promptFamily = c.cfg.LLM.PromptFamily
	}

	// Build OnEvent callback: translate agent.AgentEvent to JSON-RPC notifications.
	var onEvent func(agent.AgentEvent)
	if params.OnEvent != nil {
		notify := params.OnEvent
		onEvent = func(ev agent.AgentEvent) {
			if ev.Stream.Kind == llm.StreamEventExecOutput {
				notify("exec/output_chunk", map[string]any{
					"step":  ev.Step,
					"chunk": ev.Stream.Content,
				})
				return
			}
			notify("agent/event", map[string]any{
				"step":            ev.Step,
				"type":            string(ev.Stream.Kind),
				"content":         ev.Stream.Content,
				"tool_call_id":    ev.Stream.ToolCallID,
				"tool_call_name":  ev.Stream.ToolCallName,
				"tool_call_index": ev.Stream.ToolCallIndex,
				"args_delta":      ev.Stream.ArgsDelta,
			})
		}
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

	taskRunner := tasks.New(c.llmClient, c.validator, c.tools)
	var hooksRunner agent.HooksRunner
	if hr := hooks.New(c.cfg.Hooks, c.workspaceRoot); hr != nil {
		hooksRunner = hr
	}

	customOpts := c.resolveCustomAgentOpts(params.Mode, agentLogger)

	ag, err := agent.New(customOpts.llmClient, c.validator, c.tools, agent.Options{
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
		Mode:                 params.Mode,
		SystemPromptOverride: customOpts.systemPromptOverride,
		CustomTools:          customOpts.customTools,
		OnEvent:              onEvent,
		AgentLogger:          agentLogger,
		SubtaskRunner:        taskRunner,
		HooksRunner:          hooksRunner,
		ExtraTools:           c.mcpToolDefs(),
	})
	if err != nil {
		return nil, err
	}

	_, res, err := ag.Run(ctx, nil, params.Query)
	if err != nil {
		return nil, err
	}

	return &AgentRunResult{
		Steps:         res.Steps,
		Applied:       res.Applied,
		Patches:       res.Patches,
		Ops:           res.Ops,
		ApplyResponse: res.ApplyResponse,
	}, nil
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
func (c *Core) resolveCustomAgentOpts(mode string, agentLogger *llm.Logger) customAgentOpts {
	result := customAgentOpts{llmClient: c.llmClient}
	if c.cfg == nil || mode == "" {
		return result
	}
	def := c.cfg.FindAgent(mode)
	if def == nil {
		return result
	}

	result.systemPromptOverride = def.SystemPrompt

	if def.Model != "" {
		overrideCfg := c.cfg.LLM
		overrideCfg.Model = def.Model
		overrideClient := llm.NewOpenAIClient(overrideCfg)
		if agentLogger != nil {
			overrideClient.SetLogger(agentLogger)
		}
		result.llmClient = overrideClient
	}

	if def.Tools != nil {
		defs, err := tools.ResolveToolNames(def.Tools)
		if err == nil {
			// Auto-append MCP tools so custom agents retain MCP access.
			defs = append(defs, c.mcpToolDefs()...)
			result.customTools = defs
		}
	}

	return result
}

// ── Session API ──────────────────────────────────────────────────────────────

type SessionStartParams struct{}

type SessionStartResult struct {
	SessionID string `json:"session_id"`
}

// SessionStart creates a new session and returns its ID.
func (c *Core) SessionStart(_ SessionStartParams) (*SessionStartResult, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	s := c.sessions.Create()
	return &SessionStartResult{SessionID: s.ID}, nil
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

	// OnEvent is set programmatically by the RPC handler for streaming notifications.
	OnEvent func(method string, params any) `json:"-"`
}

type SessionMessageResult struct {
	Steps   int  `json:"steps"`
	Applied bool `json:"applied"`

	Patches       []patches.Patch     `json:"patches,omitempty"`
	Ops           []ops.AnyOp               `json:"ops,omitempty"`
	ApplyResponse *tools.FSApplyOpsResponse `json:"apply_response,omitempty"`
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

	sess, err := c.sessions.Get(params.SessionID)
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

	// Merge params with config defaults.
	agParams := AgentRunParams{
		Query:             params.Content,
		Apply:             params.Apply,
		Backup:            params.Backup,
		AllowExec:         params.AllowExec,
		MaxSteps:          params.MaxSteps,
		MaxInvalidRetries: params.MaxInvalidRetries,
		MaxPromptBytes:    params.MaxPromptBytes,
		OnEvent:           params.OnEvent,
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
		promptFamily = c.cfg.LLM.PromptFamily
	}

	var onEvent func(agent.AgentEvent)
	if agParams.OnEvent != nil {
		notify := agParams.OnEvent
		onEvent = func(ev agent.AgentEvent) {
			if ev.Stream.Kind == llm.StreamEventExecOutput {
				notify("exec/output_chunk", map[string]any{
					"step":  ev.Step,
					"chunk": ev.Stream.Content,
				})
				return
			}
			notify("agent/event", map[string]any{
				"step":            ev.Step,
				"type":            string(ev.Stream.Kind),
				"content":         ev.Stream.Content,
				"tool_call_id":    ev.Stream.ToolCallID,
				"tool_call_name":  ev.Stream.ToolCallName,
				"tool_call_index": ev.Stream.ToolCallIndex,
				"args_delta":      ev.Stream.ArgsDelta,
			})
		}
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

	sessTaskRunner := tasks.New(c.llmClient, c.validator, c.tools)
	var sessHooksRunner agent.HooksRunner
	if hr := hooks.New(c.cfg.Hooks, c.workspaceRoot); hr != nil {
		sessHooksRunner = hr
	}

	sessCustomOpts := c.resolveCustomAgentOpts(params.Mode, sessAgentLogger)

	ag, err := agent.New(sessCustomOpts.llmClient, c.validator, c.tools, agent.Options{
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
		Mode:                 params.Mode,
		SystemPromptOverride: sessCustomOpts.systemPromptOverride,
		CustomTools:          sessCustomOpts.customTools,
		OnEvent:              onEvent,
		AgentLogger:          sessAgentLogger,
		SubtaskRunner:        sessTaskRunner,
		HooksRunner:          sessHooksRunner,
		ExtraTools:           c.mcpToolDefs(),
	})
	if err != nil {
		return nil, err
	}

	outHistory, res, err := ag.Run(turnCtx, inHistory, params.Content)
	if err != nil {
		return nil, err
	}

	// Update session history and todos with the results of this turn.
	newMsgs := outHistory[len(inHistory):]
	sess.Lock()
	if len(newMsgs) > 0 {
		sess.AppendHistory(newMsgs)
	}
	if res != nil {
		sess.SetTodos(res.Todos)
	}
	sess.Unlock()

	// Store ops for potential later apply; clear if already applied or no ops.
	sess.Lock()
	if !params.Apply && len(res.Ops) > 0 {
		sess.SetPending(res.Ops)
	} else {
		sess.SetPending(nil)
	}
	sess.Unlock()

	return &SessionMessageResult{
		Steps:         res.Steps,
		Applied:       res.Applied,
		Patches:       res.Patches,
		Ops:           res.Ops,
		ApplyResponse: res.ApplyResponse,
	}, nil
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
	sess, err := c.sessions.Get(params.SessionID)
	if err != nil {
		return nil, protocol.NewError(protocol.ExecFailed, err.Error(), map[string]any{"session_id": params.SessionID})
	}

	sess.Lock()
	pendingOps := sess.TakePending()
	sess.Unlock()

	if len(pendingOps) == 0 {
		return &SessionApplyPendingResult{Applied: false}, nil
	}

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
	sess, err := c.sessions.Get(params.SessionID)
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
	sess, err := c.sessions.Get(params.SessionID)
	if err != nil {
		// Already gone — not an error.
		return nil
	}
	sess.Cancel()
	c.sessions.Delete(params.SessionID)
	return nil
}

func samePath(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
