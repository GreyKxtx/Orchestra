package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/orchestra/orchestra/internal/browser"
	"github.com/orchestra/orchestra/internal/ckg"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/lsp"
	"github.com/orchestra/orchestra/internal/lsp/provision"
	"github.com/orchestra/orchestra/internal/memory"
	"github.com/orchestra/orchestra/internal/tools/exec"
	"github.com/orchestra/orchestra/internal/tools/fs"
	"github.com/orchestra/orchestra/internal/permission"
)

// MCPCaller routes mcp:<server>:<tool> calls to the appropriate MCP server.
type MCPCaller interface {
	Call(ctx context.Context, prefixedName string, input json.RawMessage) (json.RawMessage, error)
}

// Runner executes vNext tools inside a workspace (no network tools).
type Runner struct {
	workspaceRoot string
	excludeDirs   []string

	// Defaults for exec.run safety contract.
	execTimeout     time.Duration
	execOutputLimit int // bytes, combined stdout+stderr

	mcpCaller MCPCaller

	// ckgMu guards ckgStore + ckgProvider against concurrent
	// FetchCKGContext / ExploreCodebase readers and Close writers
	// (child subagent goroutines outlive their parent's t.Cleanup,
	// so reads can race with the cleanup-time Close).
	ckgMu       sync.RWMutex
	ckgStore    *ckg.Store
	ckgProvider *ckg.Provider

	// seenInstructionDirs tracks which directories have already had their
	// ORCHESTRA.md injected into a tool result (lazy discovery).
	seenInstructionDirs sync.Map

	memoryCfg memory.Config
	sessionID string

	// Web fetch settings.
	webFetchTimeout    time.Duration
	webMaxContentBytes int

	// Web search settings.
	webSearchCfg            config.WebSearchConfig
	embedCfg                config.EmbedConfig
	webSearchTavilyEndpoint string // override for tests; empty → real Tavily URL
	webSearchBraveEndpoint  string // override for tests; empty → real Brave URL

	lspManager     *lsp.Manager
	lspAutoInstall string
	lspConsent     permission.Requester

	browserClient    *browser.Client
	allowBrowserEval bool

	// Dry-run staging: when dryRun=true, FSWrite/FSEdit accumulate changes in overlay
	// instead of writing to disk. FSRead serves staged content back to the model.
	dryRun                 bool
	dryRunMu               sync.RWMutex
	blockExecInDryRun      bool
	allowExecDespiteDryRun bool
	fsTools                *fs.Client

	// forceDiagnosticsForTest is appended to every write/edit diagnostic response.
	// Only used in tests — nil in production.
	forceDiagnosticsForTest []lsp.ToolDiagnostic
	// forceDiagnosticsHook, when set, receives staged content and returns extra
	// diagnostics (E2E: simulate LSP only while broken code remains).
	forceDiagnosticsHook func(content string) []lsp.ToolDiagnostic

	// bg holds all background processes launched via bash run_in_background=true.
	// Lazily created on first use. Killed on Close().
	bg *exec.BackgroundRegistry
}

type RunnerOptions struct {
	ExcludeDirs []string

	ExecTimeout     time.Duration
	ExecOutputLimit int // bytes, combined stdout+stderr

	WebFetchTimeout    time.Duration
	WebMaxContentBytes int

	WebSearch config.WebSearchConfig

	Embed config.EmbedConfig

	LSP config.LSPConfig

	Browser      config.BrowserConfig
	AllowBrowser bool

	// DryRun enables staging mode: write/edit accumulate in memory instead of disk.
	// FSRead serves staged content. StagedOps() returns write_atomic ops for plan.json.
	DryRun bool

	// BlockExecInDryRun, when true, refuses ExecRun calls while r.dryRun is set.
	// This closes the "exec.run bypasses the staging overlay" hole that lets a
	// model `echo > file` / `rm` its way around the dry-run contract.
	//
	// Off by default to preserve the CLI plan-mode UX: `orchestra apply` without
	// `--apply` constructs a dry-run Runner so write/edit are staged, but the
	// agent legitimately needs `git status`, `go test`, etc. for inspection.
	// Core's JSON-RPC entry (where the staging contract is a hard promise to
	// remote clients) enables this — see `internal/core/core.go::New`.
	BlockExecInDryRun bool

	// DisableASTGate turns off tree-sitter syntax validation before staging
	// (dry-run write/edit and ApplyPatchesToStaged). Default: gate enabled.
	DisableASTGate bool

	// ForceDiagnosticsForTest, if non-nil, is appended to every FSWrite/FSEdit
	// diagnostic response. Only for use in tests.
	ForceDiagnosticsForTest []lsp.ToolDiagnostic

	// ForceDiagnosticsHook, when set, receives staged file content after each
	// dry-run write/edit and may return synthetic LSP diagnostics.
	ForceDiagnosticsHook func(content string) []lsp.ToolDiagnostic
}

func NewRunner(workspaceRoot string, opts RunnerOptions) (*Runner, error) {
	if strings.TrimSpace(workspaceRoot) == "" {
		return nil, fmt.Errorf("workspaceRoot is empty")
	}
	rootAbs, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("abs workspaceRoot: %w", err)
	}

	exclude := append([]string(nil), opts.ExcludeDirs...)
	if len(exclude) == 0 {
		exclude = []string{".git", "node_modules", "dist", "build", ".orchestra"}
	}

	timeout := opts.ExecTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	limit := opts.ExecOutputLimit
	if limit <= 0 {
		limit = 100 * 1024
	}

	orchDir := filepath.Join(rootAbs, ".orchestra")
	if err := os.MkdirAll(orchDir, 0755); err != nil {
		return nil, fmt.Errorf("mkdir .orchestra: %w", err)
	}
	dbPath := filepath.Join(orchDir, "ckg.db")
	store, err := ckg.NewStore("file:" + dbPath + "?cache=shared")
	if err != nil {
		return nil, fmt.Errorf("open ckg store: %w", err)
	}
	provider := ckg.NewProvider(store, rootAbs)

	webTimeout := opts.WebFetchTimeout
	if webTimeout <= 0 {
		webTimeout = 30 * time.Second
	}
	webMaxBytes := opts.WebMaxContentBytes
	if webMaxBytes <= 0 {
		webMaxBytes = 512 * 1024
	}

	var lspMgr *lsp.Manager
	lspCfg := mergeLSPConfig(rootAbs, opts.LSP)
	if len(lspCfg.Servers) > 0 {
		mgr, lspErrs := lsp.NewManager(rootAbs, lspCfg)
		for _, e := range lspErrs {
			fmt.Fprintf(os.Stderr, "orchestra: lsp startup warning: %v\n", e)
		}
		lspMgr = mgr
	}

	var browserCli *browser.Client
	if opts.AllowBrowser {
		browserCli = browser.New(browser.Config{
			Headless:       opts.Browser.Headless,
			TimeoutMS:      opts.Browser.TimeoutMS,
			ViewportWidth:  opts.Browser.ViewportWidth,
			ViewportHeight: opts.Browser.ViewportHeight,
			AllowEval:      opts.Browser.AllowEval,
		})
	}

	r := &Runner{
		workspaceRoot:           rootAbs,
		excludeDirs:             exclude,
		execTimeout:             timeout,
		execOutputLimit:         limit,
		ckgStore:                store,
		ckgProvider:             provider,
		webFetchTimeout:         webTimeout,
		webMaxContentBytes:      webMaxBytes,
		webSearchCfg:            opts.WebSearch,
		embedCfg:                opts.Embed,
		lspManager:              lspMgr,
		lspAutoInstall:          lspCfg.EffectiveAutoInstall(),
		browserClient:           browserCli,
		allowBrowserEval:        opts.Browser.AllowEval,
		dryRun:                  opts.DryRun,
		blockExecInDryRun:       opts.BlockExecInDryRun,
		forceDiagnosticsForTest: opts.ForceDiagnosticsForTest,
		forceDiagnosticsHook:    opts.ForceDiagnosticsHook,
		memoryCfg:               memory.DefaultConfig(),
	}
	r.initFSClient(rootAbs, exclude, opts.DryRun, !opts.DisableASTGate)
	if lspMgr != nil {
		lspMgr.SetContentProvider(r)
	}
	return r, nil
}

// DryRun reports the current staging-mode flag. Cheap (RLock); callers that
// need to save / restore the flag across a one-shot pin (e.g. SkillInvoke)
// read it before SetDryRun and restore it before unlocking.
func (r *Runner) DryRun() bool {
	r.dryRunMu.RLock()
	defer r.dryRunMu.RUnlock()
	return r.dryRun
}

// SetDryRun enables or disables staging mode. Disabling clears all staged state.
func (r *Runner) SetDryRun(v bool) {
	r.dryRunMu.Lock()
	r.dryRun = v
	r.dryRunMu.Unlock()
	if r.fsTools != nil && r.fsTools.Overlay != nil {
		r.fsTools.Overlay.SetDryRun(v)
	}
}

// SetAllowExecDespiteDryRun lets exec.run proceed while file tools stay in the
// staging overlay.
func (r *Runner) SetAllowExecDespiteDryRun(v bool) {
	if r == nil {
		return
	}
	r.dryRunMu.Lock()
	defer r.dryRunMu.Unlock()
	r.allowExecDespiteDryRun = v
}

// convertLSPConfig translates config.LSPConfig to lsp.LSPConfig and merges
// workspace language detection so empty yaml still gets the right servers.
func convertLSPConfig(c config.LSPConfig) lsp.LSPConfig {
	servers := make([]lsp.LSPServerConfig, len(c.Servers))
	for i, s := range c.Servers {
		servers[i] = lsp.LSPServerConfig{
			Language:    s.Language,
			Extensions:  s.Extensions,
			Command:     s.Command,
			Env:         s.Env,
			Disabled:    s.Disabled,
			InitOptions: s.InitOptions,
		}
	}
	return lsp.LSPConfig{
		Enabled:              c.Enabled,
		Servers:              servers,
		DiagnosticsTimeoutMS: c.DiagnosticsTimeoutMS,
		InitializeTimeoutMS:  c.InitializeTimeoutMS,
		LazyStart:            c.LazyStart,
		IdleTTLSeconds:       c.IdleTTLSeconds,
		AutoInstall:          c.AutoInstall,
		EnsureSyncBudgetMS:   c.EnsureSyncBudgetMS,
	}
}

func mergeLSPConfig(workspaceRoot string, c config.LSPConfig) lsp.LSPConfig {
	base := convertLSPConfig(c)
	if c.Enabled != nil && !*c.Enabled {
		base.Servers = nil
		return base
	}
	configured := make([]provision.ServerSpec, 0, len(base.Servers))
	for _, s := range base.Servers {
		configured = append(configured, provision.ServerSpec{
			Language:   s.Language,
			Extensions: s.Extensions,
			Command:    s.Command,
			Disabled:   s.Disabled,
		})
	}
	merged := provision.MergeServersForWorkspace(configured, workspaceRoot)
	out := make([]lsp.LSPServerConfig, 0, len(merged))
	for _, s := range merged {
		out = append(out, lsp.LSPServerConfig{
			Language:   s.Language,
			Extensions: s.Extensions,
			Command:    s.Command,
			Disabled:   s.Disabled,
		})
	}
	base.Servers = out
	return base
}

func (r *Runner) WorkspaceRoot() string { return r.workspaceRoot }

// LSPStatus returns off | idle | installing | active for status UI.
func (r *Runner) LSPStatus() string {
	if r == nil || r.lspManager == nil {
		return "off"
	}
	return r.lspManager.RuntimeStatus()
}

// LSPInstallProgress returns current ensure progress for health/UI, or nil.
func (r *Runner) LSPInstallProgress() *lsp.InstallProgress {
	if r == nil || r.lspManager == nil {
		return nil
	}
	return r.lspManager.GetInstallProgress()
}

// SetLSPInstallConsent wires permission/request for missing language servers.
func (r *Runner) SetLSPInstallConsent(req permission.Requester) {
	if r == nil {
		return
	}
	r.lspConsent = req
	if r.lspManager != nil {
		r.lspManager.SetInstallConsent(req)
	}
}

// WarmupLSP detects project languages and ensures missing automated servers.
// With lazy_start (default), subprocesses spawn on first LSP tool touch only;
// WarmupStart is skipped so a Go+TS monorepo does not eager-spawn both servers.
func (r *Runner) WarmupLSP(ctx context.Context) {
	if r == nil {
		return
	}
	if r.lspManager == nil || r.lspManager.IsEmpty() {
		return
	}
	policy := provision.EnsurePolicy(r.lspAutoInstall)
	if policy == "" {
		policy = "ask"
	}
	// Prefer silent install after detect when policy is ask but no interactive
	// consent yet — still no network without consent. With consent (TUI turn),
	// one batch modal covers all missing servers.
	installed, skipped, err := provision.EnsureDetected(ctx, r.workspaceRoot, policy, r.lspConsent)
	if err != nil {
		fmt.Fprintf(os.Stderr, "orchestra: lsp warmup: %v\n", err)
		return
	}
	if len(installed) > 0 {
		fmt.Fprintf(os.Stderr, "orchestra: lsp ensured: %s\n", strings.Join(installed, ", "))
	}
	if len(skipped) > 0 {
		fmt.Fprintf(os.Stderr, "orchestra: lsp skipped: %s\n", strings.Join(skipped, ", "))
	}
	if r.lspManager != nil {
		r.lspManager.WarmupStart(ctx)
		fmt.Fprintf(os.Stderr, "orchestra: lsp status after warmup: %s\n", r.lspManager.RuntimeStatus())
	}
}

// WarmupCKG launches a background incremental scan of the CKG store so that
// the first explore or FetchCKGContext call doesn't pay the full scan cost.
// The goroutine is bound to ctx and exits silently on completion or cancellation.
func (r *Runner) WarmupCKG(ctx context.Context) {
	r.ckgMu.RLock()
	store := r.ckgStore
	r.ckgMu.RUnlock()
	if store == nil {
		return
	}
	go func() {
		orch := ckg.NewOrchestrator(store, r.workspaceRoot)
		_ = orch.UpdateGraph(ctx)
	}()
}

// Close releases resources held by the Runner (LSP manager, CKG store, etc).
// Safe to call multiple times.
func (r *Runner) Close() error {
	r.closeBg()
	if r.browserClient != nil {
		_ = r.browserClient.Close()
		r.browserClient = nil
	}
	if r.lspManager != nil {
		r.lspManager.Close()
		r.lspManager = nil
	}
	r.ckgMu.Lock()
	store := r.ckgStore
	r.ckgStore = nil
	r.ckgProvider = nil
	r.ckgMu.Unlock()
	if store != nil {
		return store.Close()
	}
	return nil
}

// SetMCPCaller registers an MCP manager for routing mcp:* tool calls.
func (r *Runner) SetMCPCaller(caller MCPCaller) { r.mcpCaller = caller }

func (r *Runner) memoryStore() *memory.Store {
	if r == nil {
		return memory.NewStore("", "", memory.DefaultConfig())
	}
	cfg := r.memoryCfg
	cfg.Normalize()
	return memory.NewStore(r.workspaceRoot, r.sessionID, cfg)
}

// discoverInstructions walks from dir up to workspaceRoot collecting ORCHESTRA.md files
// in directories not yet seen. Returns the combined text, or empty string if nothing new.
func (r *Runner) discoverInstructions(dir string) string {
	const instructionFile = "ORCHESTRA.md"
	root := filepath.Clean(r.workspaceRoot)
	dir = filepath.Clean(dir)

	var parts []string
	for {
		if !strings.HasPrefix(dir+string(filepath.Separator), root+string(filepath.Separator)) && dir != root {
			break
		}

		if _, loaded := r.seenInstructionDirs.LoadOrStore(dir, struct{}{}); !loaded {
			text := r.memoryStore().LazyOrchestra(dir)
			if text != "" {
				candidate := filepath.Join(dir, instructionFile)
				rel, _ := filepath.Rel(root, candidate)
				parts = append(parts, "Instructions from "+filepath.ToSlash(rel)+":\n"+text)
			}
		}

		if dir == root {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return strings.Join(parts, "\n\n---\n\n")
}

// --- exec.run types moved to internal/tools/exec; see aliases.go ---

func (r *Runner) extraTestDiagnostics(content string) []lsp.ToolDiagnostic {
	if r == nil {
		return nil
	}
	var out []lsp.ToolDiagnostic
	if len(r.forceDiagnosticsForTest) > 0 {
		out = append(out, r.forceDiagnosticsForTest...)
	}
	if r.forceDiagnosticsHook != nil {
		out = append(out, r.forceDiagnosticsHook(content)...)
	}
	return out
}
