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
	"github.com/orchestra/orchestra/internal/memory"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/patch/fsutil"
	"github.com/orchestra/orchestra/patch/ops"
	"github.com/orchestra/orchestra/protocol"
	"github.com/orchestra/orchestra/protocol/schema"
	"github.com/orchestra/orchestra/protocol/wire"

	coresession "github.com/orchestra/orchestra/internal/core/session"
	"github.com/orchestra/orchestra/internal/mcp"
)

type Core struct {
	workspaceRoot string
	projectID     string
	debug         bool
	initMu        sync.Mutex
	initialized   bool
	initParams    *InitializeParams
	// protocolVersion is the one initialize negotiated: the newest both
	// sides speak.
	protocolVersion int

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
	// warm tracks the background LSP warmups: Close cancels them and waits,
	// so none is still reading the runner — or os.Stderr — after it.
	warm      warmups
	closeOnce sync.Once
	closeErr  error
	// runMu serialises every RPC entry point that mutates shared Runner state
	// (SetDryRun, ClearStaged, staged-overlay writes). Without this, two
	// concurrent agent.run / session.message / workflow.run / skill.invoke /
	// ops.apply / session.apply_pending calls race over the dry-run flag and
	// can leak staged ops between requests.
	runMu    sync.Mutex
	sessions *coresession.Manager
	// housekeeping runs at most every housekeepEvery, on session.start.
	houseMu      sync.Mutex
	lastHouse    time.Time
	mcpManager   *mcp.Manager
	mcpStartErrs map[string]string // last ReplaceMCP/New failures by server name
	// mcpHost answers the requests MCP servers make of us (sampling,
	// elicitation). Built before the servers start so they get real hooks;
	// bound to the client when the RPC handler attaches its requester.
	mcpHost *mcpHost
	// sampling is the (client, model) an MCP server samples with. It is a
	// published snapshot, not a read of c.llmClient / c.cfg.LLM: the reader is
	// an MCP goroutine outside runMu, while every model writer mutates those
	// fields in place under runMu alone. Writers call publishSamplingTarget.
	sampling samplingTarget

	// What the LLM server reported as the model's context window, found in
	// the background after New — see model_limits.go. limitsMu guards it;
	// the writer is the discovery goroutine, the reader the RPC pre-dispatch
	// hook, which applies it to cfg.LLM under the usual locks.
	limitsMu         sync.Mutex
	discoveredLimits *llm.ModelLimits
	limitsCancel     context.CancelFunc
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
	// Config, when set, is the project config to run with instead of loading
	// .orchestra.yml. `orchestra apply` runs the core in its own process and
	// has already loaded the config and applied its flags (--provider,
	// --skill) to it.
	Config *config.ProjectConfig
	// ExecInDryRun lets bash run in a turn whose edits are staged. A remote
	// client's preview turn promises no side effects and keeps it off; at the
	// terminal, --allow-exec is consent for this run's commands, previews
	// included — `orchestra apply --allow-exec` runs the tests it is asked to.
	ExecInDryRun bool
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

	// Load project config from workspace root, unless the caller already has.
	cfgPath := filepath.Join(rootAbs, ".orchestra.yml")
	cfg := opts.Config
	if cfg == nil {
		loaded, err := config.Load(cfgPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load config: %w", err)
		}
		cfg = loaded
		// stderr only: stdout carries the JSON-RPC framing.
		cfg.FprintWarnings(os.Stderr)
		if st := cfg.Trust(); len(st.Ignored) > 0 {
			fmt.Fprintf(os.Stderr, "orchestra: this workspace is not trusted — ignoring %s. "+
				"Trust it with `orchestra trust` or the workspace.trust method.\n", strings.Join(st.Ignored, ", "))
		}
	}

	projectID, err := fsutil.ComputeProjectID(cfg.ProjectRoot)
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
		ExecEnvPassthrough: cfg.Exec.EnvPassthrough,
		WebFetchTimeout:    time.Duration(cfg.Web.FetchTimeoutS) * time.Second,
		WebMaxContentBytes: cfg.Web.MaxContentBytes,
		WebSearch:          cfg.Web.Search,
		LSP:                cfg.LSP,
		Embed:              cfg.ResolvedEmbed(),
		// One Runner serves every session, so the browser client is here for
		// the runs given allow_browser (skill.invoke, workflow.run) and each
		// agent refuses browser tools in a run without it. The server starts
		// on the first browser call, not here; tool.call refuses browser tools.
		Browser:      cfg.Browser,
		AllowBrowser: true,
		// JSON-RPC core makes a hard "no side effects in dry-run" promise to
		// remote clients (TUI / IDE / web). Block bash bypassing the staging
		// overlay. `orchestra apply` sets ExecInDryRun so bash inspection
		// (git status, go test) keeps working in a preview at the terminal.
		BlockExecInDryRun: !opts.ExecInDryRun,
	})
	if err != nil {
		return nil, err
	}

	injected := opts.LLMClient != nil
	llmClient := opts.LLMClient
	if llmClient == nil && !opts.ToolsOnly {
		// The model's window from the static catalogue, now, with no network:
		// enough for history budgeting to start from something sane. What the
		// server actually reports is asked for in the background below and
		// applied before the next RPC — asking here, on the constructor's
		// critical path, cost an 8-second timeout every time a project whose
		// endpoint was down or on a VPN was opened, and the window on that
		// wait showed nothing but a dimmed start screen.
		if lim, ok := llm.CatalogModelLimits(cfg.LLM); ok {
			llm.ApplyDiscoveredLimits(&cfg.LLM, lim)
		}
		llmClient = llm.BuildClient(cfg.LLM, cfg.LLMRegistry(), llm.NewLogger(rootAbs))
	}

	tr.SetMemoryContext("", memory.ConfigFrom(cfg.Memory))

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
		mcpStartErrs:      map[string]string{},
	}
	// The host resolves the model at call time: runtime.set_model swaps
	// c.llmClient under running servers, and a server that samples an hour
	// from now should get the model configured then, not the one at startup.
	// It reads the published snapshot, not the fields — see Core.sampling.
	c.publishSamplingTarget()
	c.mcpHost = newMCPHost(c.samplingModel)

	// Start MCP servers (non-fatal: errors are logged but don't abort Core startup).
	if !opts.ToolsOnly && len(cfg.MCP.Servers) > 0 {
		mcpMgr, startErrs := mcp.NewManager(context.Background(), cfg.MCP, rootAbs, c.mcpHost.hooks())
		for _, err := range startErrs {
			// Log to stderr — not a fatal error.
			fmt.Fprintf(os.Stderr, "orchestra: mcp startup warning: %v\n", err)
			msg := err.Error()
			const prefix = `mcp server "`
			if strings.HasPrefix(msg, prefix) {
				rest := msg[len(prefix):]
				if i := strings.Index(rest, `"`); i > 0 {
					c.mcpStartErrs[rest[:i]] = msg
				}
			}
		}
		c.mcpManager = mcpMgr
		if !mcpMgr.IsEmpty() {
			tr.SetMCPCaller(mcpMgr)
		}
	}
	c.noteConfigMTime()
	if !injected && !opts.ToolsOnly {
		c.startModelLimitDiscovery()
	}
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
	tr := c.tools
	c.warm.start(ctx, tr.WarmupLSP)
}

// warmups are background jobs a core started and must stop before it
// closes. They were bare goroutines: Close returned with a warmup still
// running against the runner it had just closed, still writing to the
// os.Stderr its caller was about to restore (a data race under -race).
type warmups struct {
	mu      sync.Mutex
	wg      sync.WaitGroup
	seq     int
	running map[int]context.CancelFunc
	closed  bool
}

func (w *warmups) start(ctx context.Context, job func(context.Context)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	if w.running == nil {
		w.running = map[int]context.CancelFunc{}
	}
	ctx, cancel := context.WithCancel(ctx)
	w.seq++
	id := w.seq
	w.running[id] = cancel
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		defer func() {
			cancel()
			w.mu.Lock()
			delete(w.running, id)
			w.mu.Unlock()
		}()
		job(ctx)
	}()
}

// warmupStopTimeout bounds how long Close waits for a warmup that does not
// heed its cancelled context (an installer blind to ctx).
const warmupStopTimeout = 10 * time.Second

// stop cancels every warmup and waits for them, up to warmupStopTimeout.
func (w *warmups) stop() {
	w.mu.Lock()
	w.closed = true
	for _, cancel := range w.running {
		cancel()
	}
	w.mu.Unlock()
	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(warmupStopTimeout):
		fmt.Fprintf(os.Stderr, "core: lsp warmup did not stop within %s; closing anyway\n", warmupStopTimeout)
	}
}

func (c *Core) Health() protocol.Health {
	h := protocol.Health{
		Status:          "ok",
		CoreVersion:     protocol.CoreVersion,
		ProtocolVersion: protocol.ProtocolVersion,
		// MinProtocolVersion lets a client see before initialize whether
		// the two ranges meet, and which version to ask for.
		MinProtocolVersion: protocol.MinProtocolVersion,
		OpsVersion:         protocol.OpsVersion,
		ToolsVersion:       protocol.ToolsVersion,
	}
	if c == nil {
		return h
	}
	h.WorkspaceRoot = c.workspaceRoot
	h.ProjectID = c.projectID
	if c.cfg != nil {
		h.Model = c.cfg.LLM.Model
		h.Provider = c.cfg.LLM.Provider
	}
	if c.tools != nil {
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

// InitializeParams and InitializeResult are the wire's (protocol/wire):
// the handshake is part of the contract, not of this package.
type InitializeParams = wire.InitializeParams
type InitializeResult = wire.InitializeResult

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

	// Canonicalize optional fields so initialize stays idempotent even if the
	// client omits them on subsequent calls: the range a client before v24
	// names is its one version, and ops/tools default to the core's.
	canonical := InitializeParams{
		ProjectRoot:        rootAbs,
		ProjectID:          strings.TrimSpace(params.ProjectID),
		ProtocolVersion:    params.ProtocolVersion,
		MinProtocolVersion: params.ClientRange().Min,
		OpsVersion:         params.OpsVersion,
		ToolsVersion:       params.ToolsVersion,
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
			return c.initializeResult(), nil
		}
		return nil, protocol.NewError(protocol.AlreadyInitialized, "core already initialized with different parameters", map[string]any{
			"expected": c.initParams,
			"got":      canonical,
		})
	}

	// First-time initialize: the handshake. The protocol version is the
	// newest both ranges contain (ProtocolVersion 24); before that the number
	// had to match exactly, so a client and a core from different releases
	// could not connect at all.
	negotiated, ok := wire.Negotiate(canonical.ClientRange(), wire.CoreRange())
	if !ok {
		return nil, protocol.NewError(protocol.ProtocolMismatch, "protocol_version mismatch", map[string]any{
			"client":     canonical.ProtocolVersion,
			"core":       protocol.ProtocolVersion,
			"client_min": canonical.MinProtocolVersion,
			"client_max": canonical.ProtocolVersion,
			"core_min":   protocol.MinProtocolVersion,
			"core_max":   protocol.ProtocolVersion,
		})
	}
	if canonical.OpsVersion != protocol.OpsVersion {
		return nil, protocol.NewError(protocol.ProtocolMismatch, "ops_version mismatch", map[string]any{
			"client": canonical.OpsVersion,
			"core":   protocol.OpsVersion,
		})
	}
	// tools_version is informational: it moves with tools the client never
	// calls, and it used to keep an extension one commit behind the core
	// from connecting. The answer carries the core's, for a client that
	// needs a particular tool.
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
	c.protocolVersion = negotiated

	return c.initializeResult(), nil
}

// initializeResult is the handshake's answer: the negotiated version, the
// core's tools version and what it serves. Called with initMu held.
func (c *Core) initializeResult() *InitializeResult {
	return &InitializeResult{
		Status:          "ok",
		ProtocolVersion: c.protocolVersion,
		ToolsVersion:    protocol.ToolsVersion,
		Capabilities:    wire.CoreCapabilities(),
		Health:          c.Health(),
	}
}

// ProtocolVersion is the version initialize negotiated, 0 before it.
func (c *Core) ProtocolVersion() int {
	if c == nil {
		return 0
	}
	c.initMu.Lock()
	defer c.initMu.Unlock()
	return c.protocolVersion
}

// sameInitializeParams says whether b asks for what a already got. The
// tools version is not compared: it is informational.
func sameInitializeParams(a, b InitializeParams) bool {
	if !samePath(a.ProjectRoot, b.ProjectRoot) {
		return false
	}
	if strings.TrimSpace(a.ProjectID) != strings.TrimSpace(b.ProjectID) {
		return false
	}
	if a.ProtocolVersion != b.ProtocolVersion || a.MinProtocolVersion != b.MinProtocolVersion {
		return false
	}
	if a.OpsVersion != b.OpsVersion {
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

// The wire types of this file live in protocol/wire (ARCH-4); the aliases
// keep the package's names.
type (
	OpsApplyResult = wire.OpsApplyResult
)
