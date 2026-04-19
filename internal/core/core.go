package core

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/externalpatch"
	"github.com/orchestra/orchestra/internal/llm"
	"github.com/orchestra/orchestra/internal/ops"
	"github.com/orchestra/orchestra/internal/protocol"
	"github.com/orchestra/orchestra/internal/schema"
	"github.com/orchestra/orchestra/internal/store"
	"github.com/orchestra/orchestra/internal/tools"

	coresession "github.com/orchestra/orchestra/internal/core/session"
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

	validator *schema.Validator
	tools     *tools.Runner
	sessions  *coresession.Manager
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

	projectID, err := store.ComputeProjectID(cfg.ProjectRoot)
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
	})
	if err != nil {
		return nil, err
	}

	llmClient := opts.LLMClient
	if llmClient == nil {
		llmClient = llm.NewOpenAIClient(cfg.LLM)
		// Set logger for LLM requests (only for real client)
		logger := llm.NewLogger(rootAbs)
		llmClient.(*llm.OpenAIClient).SetLogger(logger)
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

	// OnEvent is called for each agent streaming event (method + params).
	// Not serialized — set programmatically by the RPC handler.
	OnEvent func(method string, params any) `json:"-"`
}

type AgentRunResult struct {
	Steps   int  `json:"steps"`
	Applied bool `json:"applied"`

	Patches []externalpatch.Patch `json:"patches,omitempty"`
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
			notify("agent/event", map[string]any{
				"step": ev.Step,
				"type": string(ev.Stream.Kind),
				"content":        ev.Stream.Content,
				"tool_call_id":   ev.Stream.ToolCallID,
				"tool_call_name": ev.Stream.ToolCallName,
				"tool_call_index": ev.Stream.ToolCallIndex,
				"args_delta":     ev.Stream.ArgsDelta,
			})
		}
	}

	ag, err := agent.New(c.llmClient, c.validator, c.tools, agent.Options{
		MaxSteps:             maxSteps,
		MaxInvalidRetries:    maxRetries,
		MaxDeniedToolRepeats: c.cfg.Agent.MaxDeniedRepeats,
		MaxToolErrorRepeats:  c.cfg.Agent.MaxToolErrors,
		MaxFinalFailures:     c.cfg.Agent.MaxFinalFailures,
		MaxPromptBytes:       maxPromptBytes,
		LLMStepTimeout:       time.Duration(c.cfg.LLM.TimeoutS) * time.Second,
		Apply:                params.Apply,
		Backup:               params.Backup,
		AllowExec:            params.AllowExec,
		Debug:                params.Debug || c.debug,
		ResponseFormat:       respFmt,
		PromptFamily:         promptFamily,
		OnEvent:              onEvent,
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

	// Consent policy: exec.run is forbidden unless explicitly allowed by config.
	if strings.TrimSpace(params.Name) == "exec.run" && c.cfg != nil && c.cfg.Exec.Confirm != nil && *c.cfg.Exec.Confirm {
		return nil, protocol.NewError(protocol.ExecDenied, "exec.run requires user consent", map[string]any{
			"tool": params.Name,
		})
	}

	return c.tools.Call(ctx, params.Name, params.Input)
}

func (c *Core) Close() error {
	// Reserved for future (daemon state, caches, etc).
	return nil
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

	// OnEvent is set programmatically by the RPC handler for streaming notifications.
	OnEvent func(method string, params any) `json:"-"`
}

type SessionMessageResult struct {
	Steps   int  `json:"steps"`
	Applied bool `json:"applied"`

	Patches       []externalpatch.Patch     `json:"patches,omitempty"`
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
	// Snapshot history for this turn (under lock).
	inHistory := sess.CopyHistory()
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

	ag, err := agent.New(c.llmClient, c.validator, c.tools, agent.Options{
		MaxSteps:             maxSteps,
		MaxInvalidRetries:    maxRetries,
		MaxDeniedToolRepeats: c.cfg.Agent.MaxDeniedRepeats,
		MaxToolErrorRepeats:  c.cfg.Agent.MaxToolErrors,
		MaxFinalFailures:     c.cfg.Agent.MaxFinalFailures,
		MaxPromptBytes:       maxPromptBytes,
		LLMStepTimeout:       time.Duration(c.cfg.LLM.TimeoutS) * time.Second,
		Apply:                agParams.Apply,
		Backup:               agParams.Backup,
		AllowExec:            agParams.AllowExec,
		Debug:                c.debug,
		ResponseFormat:       respFmt,
		PromptFamily:         promptFamily,
		OnEvent:              onEvent,
	})
	if err != nil {
		return nil, err
	}

	outHistory, res, err := ag.Run(turnCtx, inHistory, params.Content)
	if err != nil {
		return nil, err
	}

	// Update session history with the new messages from this turn.
	newMsgs := outHistory[len(inHistory):]
	if len(newMsgs) > 0 {
		sess.Lock()
		sess.AppendHistory(newMsgs)
		sess.Unlock()
	}

	return &SessionMessageResult{
		Steps:         res.Steps,
		Applied:       res.Applied,
		Patches:       res.Patches,
		Ops:           res.Ops,
		ApplyResponse: res.ApplyResponse,
	}, nil
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
