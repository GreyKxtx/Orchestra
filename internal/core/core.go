package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/patch/cache"
	"github.com/orchestra/orchestra/patch/ops"
	"github.com/orchestra/orchestra/protocol"
	"github.com/orchestra/orchestra/protocol/schema"

	coresession "github.com/orchestra/orchestra/internal/core/session"
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

	cfg        *config.ProjectConfig
	configPath string
	// cfgMu guards cfgMTime plus reader-visible config state: the c.cfg
	// pointer swap (config_refresh) and the mutable collections read by
	// list RPCs (cfg.Agents, cfg.MCP.Servers, mcpManager). Read-only
	// endpoints (agents.list, mcp.list) take RLock only — they must never
	// queue behind runMu, which a session.message turn holds for minutes.
	// Writers already serialize on runMu and additionally take cfgMu.Lock
	// for the short in-memory mutation window (never across file I/O:
	// saveConfigLocked → noteConfigMTime locks cfgMu itself).
	cfgMu             sync.RWMutex
	cfgMTime          time.Time
	llmClient         llm.Client
	llmClientInjected bool // true when LLMClient was set via Options (test/DI mode)

	validator *schema.Validator
	tools     *tools.Runner
	// runMu serialises every RPC entry point that mutates shared Runner state
	// (SetDryRun, ClearStaged, staged-overlay writes). Without this, two
	// concurrent agent.run / session.message / workflow.run / skill.invoke /
	// ops.apply / session.apply_pending calls race over the dry-run flag and
	// can leak staged ops between requests.
	runMu        sync.Mutex
	sessions     *coresession.Manager
	mcpManager   *mcp.Manager
	mcpStartErrs map[string]string // last ReplaceMCP/New failures by server name
}

type Options struct {
	Debug bool
	// LLMClient overrides the default OpenAI client (used in tests).
	LLMClient llm.Client
	// ToolsOnly skips LLM client construction, the network call to resolve
	// model context-window limits, and starting Orchestra's own configured
	// MCP client servers. Set by `orchestra mcp serve`: an MCP tool server
	// needs none of these, and requiring them would mean it can't start at
	// all without a working, reachable LLM endpoint configured.
	ToolsOnly bool
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
	// stderr only: stdout carries the JSON-RPC framing.
	cfg.FprintWarnings(os.Stderr)

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
		Embed:              cfg.ResolvedEmbed(),
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
	if llmClient == nil && !opts.ToolsOnly {
		// Discover max_model_len from the server so max_tokens / num_ctx stay valid
		// even when .orchestra.yml is stale or missing num_ctx.
		discCtx, discCancel := context.WithTimeout(context.Background(), 8*time.Second)
		llm.ResolveModelLimits(discCtx, &cfg.LLM)
		discCancel()
		logger := llm.NewLogger(rootAbs)
		llmClient = llm.NewClient(cfg.LLM)
		if oc, ok := llm.AsOpenAIClient(llmClient); ok {
			oc.SetLogger(logger)
		}
		llmClient = llm.MaybeWrapFallback(llmClient, cfg.LLMRegistry(), cfg.LLM, logger)
	}

	// Start MCP servers (non-fatal: errors are logged but don't abort Core startup).
	var mcpMgr *mcp.Manager
	mcpErrs := map[string]string{}
	if !opts.ToolsOnly && len(cfg.MCP.Servers) > 0 {
		var startErrs []error
		mcpMgr, startErrs = mcp.NewManager(context.Background(), cfg.MCP)
		for _, err := range startErrs {
			// Log to stderr — not a fatal error.
			fmt.Fprintf(os.Stderr, "orchestra: mcp startup warning: %v\n", err)
			msg := err.Error()
			const prefix = `mcp server "`
			if strings.HasPrefix(msg, prefix) {
				rest := msg[len(prefix):]
				if i := strings.Index(rest, `"`); i > 0 {
					mcpErrs[rest[:i]] = msg
				}
			}
		}
		if !mcpMgr.IsEmpty() {
			tr.SetMCPCaller(mcpMgr)
		}
	}
	tr.SetMemoryContext("", cfg.Memory.Resolve())

	c := &Core{
		workspaceRoot:     rootAbs,
		projectID:         projectID,
		debug:             opts.Debug,
		cfg:               cfg,
		configPath:        cfgPath,
		llmClient:         llmClient,
		llmClientInjected: injected,
		validator:         v,
		tools:             tr,
		sessions:          coresession.NewManager(),
		mcpManager:        mcpMgr,
		mcpStartErrs:      mcpErrs,
	}
	c.noteConfigMTime()
	// Startup GC for staged runtime artifacts (attachments, diff-preview).
	// Self-terminating goroutine — see artifacts_gc.go.
	go cleanupWorkspaceArtifacts(rootAbs)
	return c, nil
}

// WarmupCKG starts a background CKG scan bound to ctx. Call once after New
// so the graph is populated before the first agent run or explore call.
//
// The semantic_search index is chained onto the same warmup rather than left
// to a CLI command nobody runs — embeddings are only ever written explicitly,
// so an index that is not built here is an index that stays empty. The pass is
// incremental and skipped entirely unless embed.model is set.
func (c *Core) WarmupCKG(ctx context.Context) {
	graph := c.tools.WarmupCKG(ctx)
	c.tools.WarmupEmbeddings(ctx, graph)
}

// WarmupLSP detects workspace languages and auto-ensures missing servers
// (policy from lsp.auto_install, default true).
func (c *Core) WarmupLSP(ctx context.Context) {
	if c == nil || c.tools == nil {
		return
	}
	go c.tools.WarmupLSP(ctx)
}

func (c *Core) Health() protocol.Health {
	h := protocol.Health{
		Status:          "ok",
		CoreVersion:     protocol.CoreVersion,
		ProtocolVersion: protocol.ProtocolVersion,
		OpsVersion:      protocol.OpsVersion,
		ToolsVersion:    protocol.ToolsVersion,
		WorkspaceRoot:   c.workspaceRoot,
		ProjectID:       c.projectID,
	}
	if c != nil && c.cfg != nil {
		h.Model = c.cfg.LLM.Model
		h.Provider = c.cfg.LLM.Provider
	}
	if c != nil && c.tools != nil {
		h.LSPStatus = c.tools.LSPStatus()
		if p := c.tools.LSPInstallProgress(); p != nil {
			h.LSPInstallProgress = &protocol.LSPInstallProgress{
				ID:      p.ID,
				Phase:   p.Phase,
				Percent: p.Percent,
				Message: p.Message,
			}
		}
	}
	return h
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
	// Deprecated wrapper — prefer Core.buildChildAgentConfig for resolvers.
	out := tasks.ChildAgentConfig{
		MaxPromptBytes: maxPromptBytes,
		UsageTracker:   usage,
	}
	if cfg == nil {
		return out
	}
	out.CompactThresholdPct = cfg.EffectiveCompactThresholdPct()
	out.ModelContextTokens = int(cfg.EffectiveNumCtx())
	out.CompletionMaxTokens = cfg.LLM.MaxTokens
	out.ToolDigestBytes = cfg.Agent.ResolvedToolDigestBytes()
	out.HistoryPruneKeepRecent = cfg.Agent.ResolvedHistoryPruneKeepRecent()
	out.ProviderLabel = providerLabelOf(cfg)
	out.ModelLabel = cfg.LLM.Model
	out.MaxWorkerRetries = cfg.Orchestra.ResolvedMaxWorkerRetries()
	enabled := cfg.Orchestra.ResolvedWorkerVerifyEnabled()
	out.WorkerVerifyEnabled = &enabled
	out.MaxWorkerVerifyRetries = cfg.Orchestra.ResolvedMaxWorkerVerifyRetries()
	llmVerify := cfg.Orchestra.ResolvedWorkerLLMVerifyEnabled()
	out.WorkerLLMVerifyEnabled = &llmVerify
	out.LLMStepTimeout = time.Duration(cfg.LLM.TimeoutS) * time.Second
	out.MaxStepsCap = cfg.Agent.ResolvedChildMaxSteps()
	return out
}
