package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	llmpkg "github.com/orchestra/orchestra/llm"
	"gopkg.in/yaml.v3"
)

// LLM wire/config types live in the llm module; aliases preserve YAML and call sites.
type (
	LLMConfig    = llmpkg.LLMConfig
	RouterConfig = llmpkg.RouterConfig
	ModelPreset  = llmpkg.ModelPreset
)

// AgentConfig controls the agent loop retry and step limits.
type AgentConfig struct {
	// MaxSteps is the hard cap on agent loop iterations.
	MaxSteps int `yaml:"max_steps"`
	// MaxInvalidRetries is the number of extra LLM attempts after a JSON/schema validation failure.
	MaxInvalidRetries int `yaml:"max_invalid_retries"`
	// MaxFinalFailures is the max resolve/apply failures before giving up.
	MaxFinalFailures int `yaml:"max_final_failures"`
	// MaxToolErrors is the max consecutive tool call errors before giving up.
	MaxToolErrors int `yaml:"max_tool_errors"`
	// MaxDeniedRepeats is the max repeated calls to a denied tool before giving up.
	MaxDeniedRepeats int `yaml:"max_denied_repeats"`
	// CompactThresholdPct triggers history compaction when history exceeds this % of MaxPromptBytes.
	// 0 = auto (derived from the model context window, see AutoCompactThresholdPct).
	// Negative (-1) = disabled.
	CompactThresholdPct int `yaml:"compact_threshold_pct"`
	// BytesPerContextToken calibrates prompt token estimates (default 4).
	BytesPerContextToken int `yaml:"bytes_per_context_token,omitempty"`
	// ToolDigestKB replaces tool outputs larger than this in LLM history with a digest (default 16). -1 = off.
	ToolDigestKB int `yaml:"tool_digest_kb,omitempty"`
	// HistoryPruneKeepRecent keeps the last N tool-bearing history atoms full during retroactive prune (default 2).
	HistoryPruneKeepRecent int `yaml:"history_prune_keep_recent,omitempty"`
	// AutoSessionMemory writes explore/grep notes to session memory automatically.
	AutoSessionMemory *bool `yaml:"auto_session_memory,omitempty"`
	// AutoSummaryMemory writes a ModeSummary note to project memory after long turns.
	AutoSummaryMemory *bool `yaml:"auto_summary_memory,omitempty"`
	// WorkingState enables <working_state> inject (default true).
	WorkingState *bool `yaml:"working_state,omitempty"`
	// TurnDigestKeep injects last N rule-based turn digests (default 3; 0 = off).
	TurnDigestKeep *int `yaml:"turn_digest_keep,omitempty"`
	// TurnDigestEveryN writes a mid-run micro-digest every N steps (default 6; 0 = end-of-run only).
	TurnDigestEveryN *int `yaml:"turn_digest_every_n,omitempty"`
	// Profile selects an adaptive execution preset: "" (defaults), "fast", or "precision".
	Profile string `yaml:"profile,omitempty"`
	// ChildMaxSteps caps child agent MaxSteps for task/task_spawn (default 12).
	ChildMaxSteps int `yaml:"child_max_steps,omitempty"`
}

// ApplyOutputDisk writes changes to the workspace (subject to --apply / dry-run).
const ApplyOutputDisk = "disk"

// ApplyOutputPatch exports a unified .patch file and never writes workspace files.
const ApplyOutputPatch = "patch"

// ApplyConfig controls how generated changes are materialised after an agent run.
type ApplyConfig struct {
	// Output is "disk" (default) or "patch".
	Output string `yaml:"output,omitempty"`
	// PatchDir is where .patch files are written when Output=patch.
	// Relative paths are resolved against project_root. Default: .orchestra/patches.
	PatchDir string `yaml:"patch_dir,omitempty"`
}

// LimitsConfig contains context/IO limits (vNext).
type LimitsConfig struct {
	ContextKB       int   `yaml:"context_kb"`
	MaxFiles        int   `yaml:"max_files"`
	MaxBytesPerFile int64 `yaml:"max_bytes_per_file"`
}

// ExecConfig contains exec.run safety + consent settings (vNext).
type ExecConfig struct {
	Confirm       *bool    `yaml:"confirm"`
	Allow         []string `yaml:"allow,omitempty"` // commands explicitly allowed (basename, e.g. "go", "npm")
	Deny          []string `yaml:"deny,omitempty"`  // commands explicitly denied (takes precedence over Allow)
	TimeoutS      int      `yaml:"timeout_s"`
	OutputLimitKB int      `yaml:"output_limit_kb"`
}

// IsCommandAllowed reports whether cmd may run given the allow/deny lists.
// Called only when Confirm=true (binary consent is already checked by the caller).
// Deny list takes precedence over Allow list.
// Empty Allow list with no Deny list → deny all (require explicit allowlist).
func (e ExecConfig) IsCommandAllowed(cmd string) bool {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(cmd)))
	base = strings.TrimSuffix(base, ".exe") // Windows: strip extension for comparison
	if base == "" || base == "." {
		return false
	}
	for _, d := range e.Deny {
		if strings.ToLower(strings.TrimSpace(d)) == base {
			return false
		}
	}
	if len(e.Allow) == 0 {
		return false // no allowlist configured → deny all
	}
	for _, a := range e.Allow {
		if strings.ToLower(strings.TrimSpace(a)) == base {
			return true
		}
	}
	return false
}

// LSPServerConfig configures a single language server process.
type LSPServerConfig struct {
	Language    string            `yaml:"language"`
	Extensions  []string          `yaml:"extensions"`
	Command     []string          `yaml:"command"`
	Env         map[string]string `yaml:"env,omitempty"`
	Disabled    bool              `yaml:"disabled,omitempty"`
	InitOptions map[string]any    `yaml:"init_options,omitempty"`
}

// LSPConfig holds the native LSP integration settings.
type LSPConfig struct {
	Enabled              *bool             `yaml:"enabled,omitempty"`
	Servers              []LSPServerConfig `yaml:"servers,omitempty"`
	DiagnosticsTimeoutMS int               `yaml:"diagnostics_timeout_ms,omitempty"`
	InitializeTimeoutMS  int               `yaml:"initialize_timeout_ms,omitempty"`
	LazyStart            *bool             `yaml:"lazy_start,omitempty"`
	IdleTTLSeconds       *int              `yaml:"idle_ttl_seconds,omitempty"`
	// AutoInstall controls language-server provisioning: ask | true | false.
	// Empty default = true (auto-install after workspace language detect).
	AutoInstall string `yaml:"auto_install,omitempty"`
	// EnsureSyncBudgetMS: wait this long for sync install before continuing
	// async (empty diags + diagnostics_pending). Default 2500. 0 = always
	// async after consent; negative = always wait (legacy sync).
	EnsureSyncBudgetMS int `yaml:"ensure_sync_budget_ms,omitempty"`
}

// EffectiveAutoInstall returns ask|true|false (empty → true).
func (c LSPConfig) EffectiveAutoInstall() string {
	switch strings.ToLower(strings.TrimSpace(c.AutoInstall)) {
	case "true", "yes", "1", "always":
		return "true"
	case "false", "no", "0", "never", "off":
		return "false"
	case "ask":
		return "ask"
	case "":
		return "true"
	default:
		return "ask"
	}
}

// MCPServerConfig configures a single MCP server (Phase 8).
type MCPServerConfig struct {
	// Name is the server identifier — tools appear as mcp:<name>:<tool>.
	Name string `yaml:"name"`
	// Command is the executable + args to start the MCP server via stdio.
	Command []string `yaml:"command"`
	// Env is additional environment variables (values support ${VAR} expansion).
	Env map[string]string `yaml:"env,omitempty"`
	// Disabled skips this server without removing it from the config.
	Disabled bool `yaml:"disabled,omitempty"`

	// CallTimeoutS caps a single tools/call duration in seconds. 0 or
	// negative = no per-call timeout (relies on the caller's ctx only).
	// Useful when an MCP server may hang on a slow tool — a single agent
	// step would otherwise wait on the caller's much larger context
	// timeout. M27 in audit ledger.
	CallTimeoutS int `yaml:"call_timeout_s,omitempty"`

	// AllowedTools, if non-empty, restricts which tools from this server
	// are exposed to the agent. Tool names match the bare MCP tool name
	// (not the prefixed `mcp:server:tool` form). Globs supported via
	// `path.Match`. Nil/empty = expose every tool. M31 in audit ledger.
	AllowedTools []string `yaml:"allowed_tools,omitempty"`
}

// MCPConfig holds the list of MCP servers to connect to.
type MCPConfig struct {
	Servers []MCPServerConfig `yaml:"servers,omitempty"`
}

// WebSearchConfig configures the web.search tool provider.
// Supported providers: "tavily", "brave".
type WebSearchConfig struct {
	Provider   string `yaml:"provider,omitempty"`
	APIKey     string `yaml:"api_key,omitempty"`
	MaxResults int    `yaml:"max_results,omitempty"`
}

// EmbedConfig configures the embeddings provider used to index CKG nodes
// for semantic_search. OpenAI-compatible HTTP shape — works with OpenAI,
// Ollama (/v1/embeddings), LM Studio, Voyage. When Model is empty the
// embed CLI / semantic_search tool refuse to run.
//
// Prefer Provider + Model (same named gateway as Orchestra roles). APIBase/APIKey
// are legacy overrides; ResolvedEmbed inherits credentials from Provider.
type EmbedConfig struct {
	Provider   string `yaml:"provider,omitempty"`
	APIBase    string `yaml:"api_base,omitempty"`
	APIKey     string `yaml:"api_key,omitempty"`
	Model      string `yaml:"model,omitempty"`
	Dimensions int    `yaml:"dimensions,omitempty"`
	BatchSize  int    `yaml:"batch_size,omitempty"`
	TimeoutS   int    `yaml:"timeout_s,omitempty"`
	// SemanticAutoExplore runs explore(FQN) for top semantic_search hits (default true when model set).
	SemanticAutoExplore *bool `yaml:"semantic_auto_explore,omitempty"`
	// SemanticAutoExploreTopK is how many hits to enrich with explore summaries (default 2).
	SemanticAutoExploreTopK int `yaml:"semantic_auto_explore_top_k,omitempty"`
}

// WebConfig contains web fetch safety settings.
type WebConfig struct {
	// Confirm gates webfetch: true = require --allow-web flag (default). false = allow without flag.
	Confirm         *bool           `yaml:"confirm"`
	FetchTimeoutS   int             `yaml:"fetch_timeout_s"`
	MaxContentBytes int             `yaml:"max_content_bytes"`
	Search          WebSearchConfig `yaml:"search,omitempty"`
}

// BrowserConfig contains Playwright browser automation settings.
type BrowserConfig struct {
	Headless       bool `yaml:"headless"`
	TimeoutMS      int  `yaml:"timeout_ms"`
	ViewportWidth  int  `yaml:"viewport_width"`
	ViewportHeight int  `yaml:"viewport_height"`
	AllowEval      bool `yaml:"allow_eval"`
}

// PermissionRule is a single entry in the permission ruleset.
// Rules are evaluated in order; the first matching rule determines the outcome.
//
//   - tool: canonical or alias tool name, or "*" to match any tool.
//   - pattern: glob against the tool's subject (command string for bash,
//     URL for webfetch, file path for fs tools). Omit or set to "" to match any subject.
//     Glob syntax: standard path.Match — '*' matches any sequence of non-separator chars,
//     '?' matches a single non-separator char. '**' is NOT supported.
//   - action: "allow", "deny", or "ask" (ask requires PermissionRequester for edit/write/bash).
//
// An explicit "allow" rule permits the tool call even when --allow-exec / --allow-web
// are not set. An explicit "deny" always blocks the call with TOOL_DENIED.
// If no rule matches, the call falls through to the existing consent gates.
type PermissionRule struct {
	Tool    string `yaml:"tool"`
	Pattern string `yaml:"pattern,omitempty"`
	Action  string `yaml:"action"` // "allow" | "deny" | "ask"
}

// PermissionsConfig holds the ordered permission ruleset evaluated before
// every tool call.
type PermissionsConfig struct {
	Rules []PermissionRule `yaml:"rules,omitempty"`
}

// AgentDefinition defines a custom named agent in .orchestra.yml.
// Custom agents override the built-in "build" mode behaviour with a different
// system prompt, tool set, and/or model — without breaking the two-layer patch
// contract.
type AgentDefinition struct {
	// Name is required and must be unique; cannot collide with built-in modes.
	Name string `yaml:"name" json:"name"`
	// SystemPrompt replaces the built-in mode system prompt for this agent.
	// .orchestra/system.txt still takes precedence when present.
	SystemPrompt string `yaml:"system_prompt,omitempty" json:"system_prompt,omitempty"`
	// Tools is the explicit tool list.
	//   nil (omitted) → inherit the full build toolset.
	//   []  (empty)   → config load error (caught by validateAgents).
	//   [ls, read, …] → exactly these tools are exposed to the model.
	Tools []string `yaml:"tools,omitempty" json:"tools,omitempty"`
	// Model overrides the model name within the same provider (v1).
	// Provider, api_base, and api_key are inherited from the top-level llm config.
	Model string `yaml:"model,omitempty" json:"model,omitempty"`
	// Provider references a named entry in the top-level providers: map.
	// When set, the agent uses that provider's full LLMConfig (api_base, api_key, etc.)
	// instead of the global llm: config. Model (above) still overrides the provider model.
	Provider string `yaml:"provider,omitempty" json:"provider,omitempty"`
}

// ModeKind classifies how a built-in agent mode may be started.
type ModeKind int

const (
	// ModeKindTopLevel can be requested by the user (CLI --mode, RPC agent.run)
	// and can also be spawned as a subagent.
	ModeKindTopLevel ModeKind = iota
	// ModeKindChildOnly is a subagent role with its own protocol (task_result,
	// WorkOrder, scoped writes). Starting one top-level skips the contract that
	// gives it its input, so the CLI and RPC refuse it.
	ModeKindChildOnly
	// ModeKindInternal is driven by the runtime itself (history compaction,
	// title, summary) and never selected by a user.
	ModeKindInternal
)

// builtInAgentModes is the single registry of reserved mode names.
//
// It backs three questions that used to be answered by separate hardcoded
// lists: is this name reserved against custom agents / skills, may the user
// start this mode, and does agent.IsKnownMode recognise it. `product` and
// `documentation` were missing here while existing as real modes with their
// own tool sets and write scopes — so a custom agent or skill could take
// those names and shadow them.
var builtInAgentModes = map[string]ModeKind{
	"build":         ModeKindTopLevel,
	"plan":          ModeKindTopLevel,
	"explore":       ModeKindTopLevel,
	"general":       ModeKindTopLevel,
	"ask":           ModeKindTopLevel,
	"debug":         ModeKindTopLevel,
	"architecture":  ModeKindTopLevel,
	"agent":         ModeKindTopLevel,
	"orchestra":     ModeKindTopLevel,
	"worker":        ModeKindChildOnly,
	"verifier":      ModeKindChildOnly,
	"product":       ModeKindChildOnly,
	"documentation": ModeKindChildOnly,
	"compaction":    ModeKindInternal,
	"title":         ModeKindInternal,
	"summary":       ModeKindInternal,
}

// validAgentToolNames lists all short tool names that are valid in
// AgentDefinition.Tools. Hardcoded here to avoid an import cycle between
// config and tools (tools → llm → config).
var validAgentToolNames = map[string]bool{
	"ls": true, "read": true, "glob": true, "write": true, "edit": true, "fs.delete": true, "fs.rename": true,
	"grep": true, "symbols": true, "explore": true, "bash": true,
	"webfetch": true, "todowrite": true, "todoread": true,
	"bash.output": true, "bash.kill": true,
	"semantic_search": true, "repo_map": true, "ast_rename": true,
	"memory_write": true, "memory_read": true, "memory_search": true, "runtime_query": true,
	"lesson_promote": true, "playbook_promote": true,
	"task_spawn": true, "task_wait": true, "task_cancel": true, "task_result": true,
	"plan_exit": true, "question": true,
	"lsp.definition": true, "lsp.references": true, "lsp.hover": true,
	"lsp.diagnostics": true, "lsp.rename": true,
	"diff.preview": true,
	"git.status":   true, "git.log": true, "git.diff": true,
	"git.commit": true, "git.branch": true, "git.checkout": true, "git.push": true,
	"gh.pr.list": true, "gh.pr.create": true, "gh.pr.view": true,
	"gh.issue.list": true, "gh.issue.view": true,
	"browser.navigate": true, "browser.snapshot": true, "browser.screenshot": true,
	"browser.click": true, "browser.type": true, "browser.fill": true,
	"browser.select": true, "browser.eval": true, "browser.wait": true, "browser.close": true,
	"websearch": true,
}

// ValidAgentTool reports whether name is a valid short tool name usable
// in AgentDefinition.Tools or in a skill's tools: list. Exported so the
// skills loader (outside this package) can validate without forking the
// allow-list.
func ValidAgentTool(name string) bool {
	return validAgentToolNames[name]
}

// ValidAgentToolNames returns every short tool name allowed in AgentDefinition.Tools,
// sorted for stable UI / RPC catalogs.
func ValidAgentToolNames() []string {
	out := make([]string, 0, len(validAgentToolNames))
	for name := range validAgentToolNames {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// HooksConfig configures pre/post tool call hooks (Phase 6).
type HooksConfig struct {
	// Enabled gates all hook execution. Hooks are disabled by default.
	Enabled bool `yaml:"enabled"`
	// PreTool is the command + args to run before each tool call.
	// Non-zero exit denies the tool call. Env: ORCH_TOOL_NAME, ORCH_TOOL_INPUT, ORCH_WORKSPACE_ROOT.
	PreTool []string `yaml:"pre_tool,omitempty"`
	// PostTool is the command + args to run after each successful tool call.
	// Non-zero exit is logged but does not fail the tool.
	PostTool []string `yaml:"post_tool,omitempty"`
	// TimeoutMS is the per-hook subprocess timeout (default: 5000ms).
	TimeoutMS int `yaml:"timeout_ms"`
}

// MemoryConfig controls layered project/session/global memory.
type MemoryConfig struct {
	InjectKB       int    `yaml:"inject_kb,omitempty"`
	LazyKB         int    `yaml:"lazy_kb,omitempty"`
	Mode           string `yaml:"mode,omitempty"` // eager | lazy | hybrid
	GlobalEnabled  *bool  `yaml:"global_enabled,omitempty"`
	SessionEnabled *bool  `yaml:"session_enabled,omitempty"`
	MaxAgentKB     int    `yaml:"max_agent_kb,omitempty"`
}

// UIConfig holds TUI-only presentation preferences.
type UIConfig struct {
	// Theme is a registered theme name (see ui/tui/theme). Empty / unknown
	// values fall back to the default ("neutral").
	Theme string `yaml:"theme,omitempty"`
	// AutoApply is deprecated: TUI always commits staged ops on agent final.
	// Kept for config migration only; ignored by TUI.
	AutoApply bool `yaml:"auto_apply,omitempty"`
	// AllowExec mirrors `--allow-exec` for TUI agent runs (bash/git commit, etc.).
	AllowExec bool `yaml:"allow_exec,omitempty"`
}

// ProjectConfig represents the Orchestra configuration
type ProjectConfig struct {
	ProjectRoot string   `yaml:"project_root"`
	ExcludeDirs []string `yaml:"exclude_dirs"`
	// ContextLimit is the v0.2/v0.3 name kept for backward compatibility.
	// Prefer Limits.ContextKB.
	ContextLimit int               `yaml:"context_limit_kb"`
	Limits       LimitsConfig      `yaml:"limits"`
	LLM          LLMConfig         `yaml:"llm"`
	Agent        AgentConfig       `yaml:"agent"`
	Apply        ApplyConfig       `yaml:"apply,omitempty"`
	Exec         ExecConfig        `yaml:"exec"`
	Hooks        HooksConfig       `yaml:"hooks"`
	MCP          MCPConfig         `yaml:"mcp"`
	Web          WebConfig         `yaml:"web"`
	Browser      BrowserConfig     `yaml:"browser"`
	Permissions  PermissionsConfig `yaml:"permissions,omitempty"`
	Agents       []AgentDefinition `yaml:"agents,omitempty"`
	LSP          LSPConfig         `yaml:"lsp,omitempty"`
	UI           UIConfig          `yaml:"ui,omitempty"`
	Memory       MemoryConfig      `yaml:"memory,omitempty"`
	Embed        EmbedConfig       `yaml:"embed,omitempty"`
	// AutoRouter classifies queries when mode=agent (build|plan|explore).
	AutoRouter AutoRouterConfig `yaml:"auto_router,omitempty"`
	// Orchestra configures Lead + worker tiers for mode=orchestra.
	Orchestra OrchestraConfig `yaml:"orchestra,omitempty"`
	// Providers is an optional map of named LLM provider configurations.
	// Use in agents: via provider: <name> or with --provider <name> CLI flag.
	Providers map[string]LLMConfig `yaml:"providers,omitempty"`
	// Pricing maps provider → model → per-1M-token USD rates for cost telemetry.
	// Optional; when missing, .orchestra/usage.jsonl records only token counts.
	Pricing PricingConfig `yaml:"pricing,omitempty"`
	// Routing is the parsed orchestra_routing.yaml (tier → provider/model
	// bindings). Loaded from the config directory by Load; never marshalled
	// back into .orchestra.yml.
	Routing *OrchestraRouting `yaml:"-"`
}

// ModelPricing is the per-1M-token USD price for one model.
type ModelPricing struct {
	InputPer1M  float64 `yaml:"input_per_1m"`
	OutputPer1M float64 `yaml:"output_per_1m"`
}

// PricingConfig is the nested provider→model pricing map.
// Use "default" as the provider key for fallbacks shared across providers.
type PricingConfig map[string]map[string]ModelPricing

// FindAgent looks up a custom agent by name. Returns nil when not found.
func (c *ProjectConfig) FindAgent(name string) *AgentDefinition {
	for i := range c.Agents {
		if c.Agents[i].Name == name {
			return &c.Agents[i]
		}
	}
	return nil
}

// FindProvider looks up a named provider from the providers: map.
// When the named entry omits api_key/api_base, inherits from top-level llm:
// if the names match (or named provider field equals llm.provider).
func (c *ProjectConfig) FindProvider(name string) (LLMConfig, bool) {
	if c.Providers == nil {
		return LLMConfig{}, false
	}
	cfg, ok := c.Providers[name]
	if !ok {
		return LLMConfig{}, false
	}
	// Inherit credentials from main llm: when the named slot left them blank.
	sameAsMain := strings.EqualFold(strings.TrimSpace(name), strings.TrimSpace(c.LLM.Provider)) ||
		strings.EqualFold(strings.TrimSpace(cfg.Provider), strings.TrimSpace(c.LLM.Provider))
	if strings.TrimSpace(cfg.APIKey) == "" && sameAsMain {
		cfg.APIKey = c.LLM.APIKey
	}
	if strings.TrimSpace(cfg.APIBase) == "" {
		if sameAsMain && strings.TrimSpace(c.LLM.APIBase) != "" {
			cfg.APIBase = c.LLM.APIBase
		}
	}
	if strings.TrimSpace(cfg.APIKey) == "" && strings.TrimSpace(c.LLM.APIKey) != "" {
		// Soft inherit: many TUI flows write key only under llm: while providers:
		// hold api_base/model for orchestra roles on the same endpoint.
		if strings.TrimSpace(cfg.APIBase) == "" ||
			NormalizeAPIBase(cfg.APIBase) == NormalizeAPIBase(c.LLM.APIBase) {
			cfg.APIKey = c.LLM.APIKey
		}
	}
	// Inherit tuning when the named slot left them at zero/empty — otherwise
	// NewClient omits max_tokens and vLLM may default to ~50k completion tokens.
	if cfg.MaxTokens <= 0 && c.LLM.MaxTokens > 0 {
		cfg.MaxTokens = c.LLM.MaxTokens
	}
	if cfg.Temperature == 0 && c.LLM.Temperature != 0 {
		cfg.Temperature = c.LLM.Temperature
	}
	if cfg.TimeoutS <= 0 && c.LLM.TimeoutS > 0 {
		cfg.TimeoutS = c.LLM.TimeoutS
	}
	if strings.TrimSpace(cfg.ToolChoice) == "" && strings.TrimSpace(c.LLM.ToolChoice) != "" {
		cfg.ToolChoice = c.LLM.ToolChoice
	}
	if len(cfg.ExtraBody) == 0 && len(c.LLM.ExtraBody) > 0 {
		cfg.ExtraBody = inheritedExtraBody(name, cfg, c.LLM.ExtraBody)
	}
	return cfg, true
}

// inheritedExtraBody prepares the main llm.extra_body for a named provider.
// num_ctx describes the MAIN endpoint's local context window (vLLM/Ollama
// RAM budget); inheriting it into a cloud provider fakes a smaller window and
// makes the client preflight reject prompts the real model would accept
// ("prompt too large (~N) for model context num_ctx"). Cloud providers get
// the extras without num_ctx; local providers keep it as before.
func inheritedExtraBody(name string, cfg LLMConfig, extra map[string]any) map[string]any {
	if _, hasNumCtx := extra["num_ctx"]; !hasNumCtx {
		return extra
	}
	provider := strings.TrimSpace(cfg.Provider)
	if provider == "" {
		provider = strings.TrimSpace(name)
	}
	cat, ok := llmpkg.FindCatalogProvider(provider)
	if !ok || cat.Local {
		return extra
	}
	out := make(map[string]any, len(extra))
	for k, v := range extra {
		if k != "num_ctx" {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// LLMRegistry exposes llm + providers for the llm sub-module (no config→llm cycle).
func (c *ProjectConfig) LLMRegistry() llmpkg.ProviderRegistry {
	if c == nil {
		return llmpkg.ProviderRegistry{}
	}
	return llmpkg.NewProviderRegistry(c.LLM, c.Providers)
}

// ResolvedEmbed returns embed settings with credentials inherited from the
// selected named provider (or the main llm: block when provider is empty).
// When Provider is set, that gateway's api_base/api_key win over a stale
// embed.api_base leftover (for example an offline ngrok tunnel).
func (c *ProjectConfig) ResolvedEmbed() EmbedConfig {
	if c == nil {
		return EmbedConfig{}
	}
	out := c.Embed
	prov := strings.TrimSpace(out.Provider)
	if prov != "" {
		resolved, _, ok := llmpkg.ResolveProviderConfig(c.LLMRegistry(), prov)
		if ok {
			if base := strings.TrimSpace(resolved.APIBase); base != "" {
				out.APIBase = base
			}
			if key := strings.TrimSpace(resolved.APIKey); key != "" {
				out.APIKey = key
			}
		}
		return out
	}
	if strings.TrimSpace(out.APIBase) == "" {
		out.APIBase = strings.TrimSpace(c.LLM.APIBase)
	}
	if strings.TrimSpace(out.APIKey) == "" {
		out.APIKey = strings.TrimSpace(c.LLM.APIKey)
	}
	return out
}

// NormalizeAPIBase trims trailing slashes for endpoint comparison.
func NormalizeAPIBase(u string) string {
	return strings.TrimRight(strings.TrimSpace(u), "/")
}

// EffectiveNumCtx returns the user-configured LLM context window for the
// active model: per-model preset num_ctx, else llm.extra_body.num_ctx.
func (c *ProjectConfig) EffectiveNumCtx() int64 {
	if c == nil {
		return 0
	}
	model := strings.TrimSpace(c.LLM.Model)
	if model != "" && c.LLM.ModelPresets != nil {
		if p, ok := c.LLM.ModelPresets[model]; ok && p.NumCtx > 0 {
			return p.NumCtx
		}
	}
	return c.extraBodyNumCtx()
}

// bytesPerContextToken is the byte budget heuristic when deriving MaxPromptBytes
// from num_ctx (≈4 bytes per token for Latin/Cyrillic mixed text + JSON).
const bytesPerContextToken = 4

// EffectiveMaxPromptBytes returns the agent history byte budget. Uses the
// larger of limits.context_kb and a share of num_ctx so compaction aligns with
// the real model window — but never the full window: vLLM requires
// prompt + max_tokens ≤ max_model_len, so history tracks PromptBudgetTokens.
func (c *ProjectConfig) EffectiveMaxPromptBytes() int {
	if c == nil {
		return 0
	}
	kb := c.Limits.ContextKB
	if kb <= 0 && c.ContextLimit > 0 {
		kb = c.ContextLimit
	}
	if kb <= 0 {
		kb = 128
	}
	bytes := kb * 1024
	bpt := c.Agent.ResolvedBytesPerContextToken()
	if bpt <= 0 {
		bpt = bytesPerContextToken
	}
	if n := c.EffectiveNumCtx(); n > 0 {
		// Same reserve as llm.PromptBudgetTokens: leave room for max_tokens + safety.
		want := c.LLM.MaxTokens
		if want <= 0 {
			want = 4096
		}
		promptTok := int(n) - want - 2048
		floor := int(n) / 4
		if promptTok < floor {
			promptTok = floor
		}
		fromCtx := promptTok * bpt
		if fromCtx > bytes {
			bytes = fromCtx
		}
	}
	return bytes
}

// EffectiveCompactThresholdPct resolves the compaction trigger percent,
// scaling the auto (0) setting to the real model context window. Returns 0
// when compaction is disabled.
func (c *ProjectConfig) EffectiveCompactThresholdPct() int {
	if c == nil {
		return 0
	}
	if c.Agent.CompactThresholdPct < 0 {
		return 0
	}
	if c.Agent.CompactThresholdPct > 0 {
		return c.Agent.CompactThresholdPct
	}
	return AutoCompactThresholdPct(int(c.EffectiveNumCtx()))
}

func (c *ProjectConfig) extraBodyNumCtx() int64 {
	if c.LLM.ExtraBody == nil {
		return 0
	}
	v, ok := c.LLM.ExtraBody["num_ctx"]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	case float64:
		return int64(n)
	case float32:
		return int64(n)
	}
	return 0
}

// DefaultConfig creates a default configuration for the project root
func DefaultConfig(projectRoot string) *ProjectConfig {
	return &ProjectConfig{
		ProjectRoot: projectRoot,
		ExcludeDirs: []string{
			".git",
			"node_modules",
			"dist",
			"build",
			".orchestra",
		},
		ContextLimit: 50,
		Limits: LimitsConfig{
			ContextKB:       128,
			MaxFiles:        30,
			MaxBytesPerFile: 200 * 1024,
		},
		LLM: LLMConfig{
			APIBase:     "http://localhost:1234/v1",
			Model:       "qwen2.5-coder-7b",
			Temperature: 0.7,
			MaxTokens:   4096,
			TimeoutS:    600,
			// response_format_type: json_schema — явно; omit на local → auto json_schema
			// supports_json_schema: false — отключить auto/явный json_schema
		},
		Agent: AgentConfig{
			MaxSteps:            128,
			MaxInvalidRetries:   0, // 0 = auto (provider-tuned at launch)
			MaxFinalFailures:    0,
			MaxToolErrors:       0,
			MaxDeniedRepeats:    0,
			CompactThresholdPct: 0, // auto: derived from the model context window
			ToolDigestKB:        48,
			AutoSessionMemory:   boolPtr(true),
			AutoSummaryMemory:   boolPtr(true),
			TurnDigestEveryN:    intPtr(4),
		},
		Exec: ExecConfig{
			Confirm:       boolPtr(true),
			TimeoutS:      30,
			OutputLimitKB: 100,
		},
		Web: WebConfig{
			Confirm:         boolPtr(true),
			FetchTimeoutS:   30,
			MaxContentBytes: 512 * 1024,
		},
		Browser: BrowserConfig{
			Headless:       true,
			TimeoutMS:      30000,
			ViewportWidth:  1280,
			ViewportHeight: 720,
			AllowEval:      false,
		},
	}
}

// Load loads configuration from file. When a .orchestra.local.yml overlay
// exists next to it, overlay values are deep-merged on top (secrets and
// personal overrides live there — see local_overlay.go).
func Load(path string) (*ProjectConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	data, err = mergeLocalOverlay(path, data)
	if err != nil {
		return nil, err
	}

	var cfg ProjectConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	cfg.applyDefaults()

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	routing, err := LoadOrchestraRouting(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	if routing != nil {
		if err := routing.Validate(cfg.Providers); err != nil {
			return nil, err
		}
		cfg.Routing = routing
	}

	return &cfg, nil
}

// Save saves configuration to file.
//
// The write is atomic (temp file → fsync → rename): .orchestra.yml is shared
// by every frontend (TUI, VS Code extension, CLI), so a crash or a concurrent
// reader must never observe a half-written config — that would lose all
// user settings at once.
//
// Keys defined in .orchestra.local.yml are masked back to their on-disk
// values before writing: the in-memory cfg holds merged overlay values (e.g.
// api_key), and persisting them here would leak local secrets into the
// shared config.
func Save(path string, cfg *ProjectConfig) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Serialise writers across processes (TUI core + extension core on the
	// same project): concurrent load→modify→Save cycles silently drop each
	// other's changes without this.
	unlock := acquireFileLock(path)
	defer unlock()

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	data, err = maskLocalOverlay(path, data)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temp config file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("failed to write config file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("failed to sync config file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("failed to close config file: %w", err)
	}
	if err := os.Chmod(tmpName, 0600); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("failed to chmod config file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("failed to replace config file: %w", err)
	}
	return nil
}

func (c *ProjectConfig) applyDefaults() {
	// vNext limits: inherit legacy context_limit_kb if limits.context_kb is missing.
	if c.Limits.ContextKB <= 0 && c.ContextLimit > 0 {
		c.Limits.ContextKB = c.ContextLimit
	}
	if c.Limits.ContextKB <= 0 {
		c.Limits.ContextKB = 128
	}
	// Keep legacy field in sync so old code paths still work.
	if c.ContextLimit <= 0 {
		c.ContextLimit = c.Limits.ContextKB
	}
	if c.Limits.MaxFiles <= 0 {
		c.Limits.MaxFiles = 30
	}
	if c.Limits.MaxBytesPerFile <= 0 {
		c.Limits.MaxBytesPerFile = 200 * 1024
	}

	// Exec defaults
	if c.Exec.TimeoutS <= 0 {
		c.Exec.TimeoutS = 30
	}
	if c.Exec.OutputLimitKB <= 0 {
		c.Exec.OutputLimitKB = 100
	}
	// Default confirm=true unless explicitly set.
	if c.Exec.Confirm == nil {
		c.Exec.Confirm = boolPtr(true)
	}

	// Hooks defaults
	if c.Hooks.TimeoutMS <= 0 {
		c.Hooks.TimeoutMS = 5000
	}

	// Web defaults
	if c.Web.Confirm == nil {
		c.Web.Confirm = boolPtr(true)
	}
	if c.Web.FetchTimeoutS <= 0 {
		c.Web.FetchTimeoutS = 30
	}
	if c.Web.MaxContentBytes <= 0 {
		c.Web.MaxContentBytes = 512 * 1024
	}

	// LLM defaults
	if c.LLM.TimeoutS <= 0 {
		c.LLM.TimeoutS = 600
	}

	// Agent defaults (tuned for local models).
	if c.Agent.MaxSteps <= 0 {
		c.Agent.MaxSteps = 128
	}
	// Retry limits: 0 leaves provider-aware FillRetryLimits at launch/RPC.
	// Compact: 0 → auto (window-derived); negative → disabled (stored as -1).
	if c.Agent.CompactThresholdPct < 0 {
		// Keep the "disabled" intent distinguishable from 0 = auto.
		c.Agent.CompactThresholdPct = -1
	}

	// Apply output defaults.
	if c.Apply.Output == "" {
		c.Apply.Output = ApplyOutputDisk
	}
	if c.Apply.PatchDir == "" {
		c.Apply.PatchDir = ".orchestra/patches"
	}
}

func boolPtr(v bool) *bool { return &v }

func intPtr(v int) *int { return &v }

// Validate validates the configuration
func (c *ProjectConfig) Validate() error {
	if c.ProjectRoot == "" {
		return fmt.Errorf("project_root is required")
	}

	if c.LLM.APIBase == "" {
		return fmt.Errorf("llm.api_base is required")
	}

	if c.LLM.Model == "" {
		return fmt.Errorf("llm.model is required")
	}
	if c.LLM.TimeoutS <= 0 {
		return fmt.Errorf("llm.timeout_s must be > 0")
	}

	if c.ContextLimit <= 0 {
		return fmt.Errorf("context_limit_kb must be greater than 0")
	}

	if c.Limits.ContextKB <= 0 {
		return fmt.Errorf("limits.context_kb must be greater than 0")
	}
	if c.Limits.MaxFiles < 0 {
		return fmt.Errorf("limits.max_files must be >= 0")
	}
	if c.Limits.MaxBytesPerFile < 0 {
		return fmt.Errorf("limits.max_bytes_per_file must be >= 0")
	}

	if c.Exec.TimeoutS <= 0 {
		return fmt.Errorf("exec.timeout_s must be > 0")
	}
	if c.Exec.OutputLimitKB <= 0 {
		return fmt.Errorf("exec.output_limit_kb must be > 0")
	}

	if err := c.validateAgents(); err != nil {
		return err
	}
	if err := c.validateOrchestra(); err != nil {
		return err
	}
	if err := c.validateAutoRouter(); err != nil {
		return err
	}

	if err := c.validateMCP(); err != nil {
		return err
	}

	if err := c.validateLSP(); err != nil {
		return err
	}

	switch strings.ToLower(strings.TrimSpace(c.Apply.Output)) {
	case "", ApplyOutputDisk, ApplyOutputPatch:
		// ok (empty normalised in applyDefaults)
	default:
		return fmt.Errorf("apply.output must be %q or %q, got %q", ApplyOutputDisk, ApplyOutputPatch, c.Apply.Output)
	}
	switch strings.ToLower(strings.TrimSpace(c.Agent.Profile)) {
	case "", "fast", "precision":
		// ok
	default:
		return fmt.Errorf("agent.profile must be empty, \"fast\", or \"precision\", got %q", c.Agent.Profile)
	}

	if err := c.validateMemory(); err != nil {
		return err
	}

	return nil
}

// validateMCP enforces MCP server-name invariants used by routing.
// H12 in audit ledger: a server name containing `:` produces a tool name
// like `mcp:foo:bar:baz` which `parseMCPToolName` splits as server=foo
// tool=bar:baz, then findClient("foo") returns nil and every call fails.
// Duplicate names silently make the second server's tools unroutable.
func (c *ProjectConfig) validateMCP() error {
	seen := map[string]bool{}
	for i, srv := range c.MCP.Servers {
		name := strings.TrimSpace(srv.Name)
		if name == "" {
			return fmt.Errorf("mcp.servers[%d]: name is required", i)
		}
		if strings.Contains(name, ":") {
			return fmt.Errorf("mcp.servers[%d]: name %q must not contain ':' (tool routing splits on ':')", i, name)
		}
		if seen[name] {
			return fmt.Errorf("mcp.servers[%d]: duplicate name %q (each MCP server name must be unique within the project)", i, name)
		}
		seen[name] = true
	}
	return nil
}

// ValidateMCPOnly validates mcp.servers without loading the full config.
func (c *ProjectConfig) ValidateMCPOnly() error {
	if c == nil {
		return nil
	}
	return c.validateMCP()
}

func (c *ProjectConfig) validateLSP() error {
	v := strings.ToLower(strings.TrimSpace(c.LSP.AutoInstall))
	switch v {
	case "", "ask", "true", "false", "yes", "no", "1", "0", "always", "never", "off":
		return nil
	default:
		return fmt.Errorf("lsp.auto_install must be ask, true, or false, got %q", c.LSP.AutoInstall)
	}
}

func (c *ProjectConfig) validateOrchestra() error {
	o := c.Orchestra
	if o.Planner.Provider != "" {
		if _, ok := c.Providers[o.Planner.Provider]; !ok {
			return fmt.Errorf("orchestra.planner.provider %q not defined in providers", o.Planner.Provider)
		}
	}
	seen := map[string]bool{}
	for i, t := range o.Tiers {
		name := strings.TrimSpace(t.Name)
		if name == "" {
			return fmt.Errorf("orchestra.tiers[%d]: name is required", i)
		}
		key := strings.ToLower(name)
		if seen[key] {
			return fmt.Errorf("orchestra.tiers: duplicate name %q", name)
		}
		seen[key] = true
		if t.Provider != "" {
			if _, ok := c.Providers[t.Provider]; !ok {
				return fmt.Errorf("orchestra.tiers[%d] (%q): provider %q not defined in providers", i, name, t.Provider)
			}
		}
	}
	if def := strings.TrimSpace(o.DefaultTier); def != "" && len(o.Tiers) > 0 {
		if o.FindTier(def) == nil {
			return fmt.Errorf("orchestra.default_tier %q not found in tiers", def)
		}
	}
	switch o.ResolvedPhaseEnforcement() {
	case "strict", "prompt_only":
	default:
		return fmt.Errorf("orchestra.phase_enforcement must be strict or prompt_only, got %q", o.PhaseEnforcement)
	}
	for k, v := range o.Gates {
		switch k {
		case GateGitCommit, GateGitPush, GateContractFreeze:
		default:
			return fmt.Errorf("orchestra.gates: unknown gate %q (known: %s, %s, %s)", k, GateGitCommit, GateGitPush, GateContractFreeze)
		}
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "required", "off":
		default:
			return fmt.Errorf("orchestra.gates.%s must be required or off, got %q", k, v)
		}
	}
	return nil
}

func (c *ProjectConfig) validateAutoRouter() error {
	if c.AutoRouter.Provider == "" {
		return nil
	}
	if _, ok := c.Providers[c.AutoRouter.Provider]; !ok {
		return fmt.Errorf("auto_router.provider %q not defined in providers", c.AutoRouter.Provider)
	}
	return nil
}

// IsBuiltInMode reports whether name is a reserved built-in agent mode.
// Reserved means "a custom agent or skill may not take this name" — it covers
// child-only and internal modes too.
func IsBuiltInMode(name string) bool {
	_, ok := builtInAgentModes[name]
	return ok
}

// BuiltInModeKind returns the mode's kind and whether it is built in at all.
func BuiltInModeKind(name string) (ModeKind, bool) {
	k, ok := builtInAgentModes[name]
	return k, ok
}

// IsUserSelectableMode reports whether the user may start this mode directly
// (CLI --mode, RPC agent.run). Child-only and internal modes are not.
func IsUserSelectableMode(name string) bool {
	k, ok := builtInAgentModes[name]
	return ok && k == ModeKindTopLevel
}

// BuiltInModeNames returns reserved agent mode names (sorted).
func BuiltInModeNames() []string {
	out := make([]string, 0, len(builtInAgentModes))
	for name := range builtInAgentModes {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// UserSelectableModeNames returns the modes a user may start directly (sorted).
func UserSelectableModeNames() []string {
	out := make([]string, 0, len(builtInAgentModes))
	for name, k := range builtInAgentModes {
		if k == ModeKindTopLevel {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// ValidateAgentsOnly validates agents: without a full config reload.
func (c *ProjectConfig) ValidateAgentsOnly() error {
	if c == nil {
		return nil
	}
	return c.validateAgents()
}

// validateAgents checks all AgentDefinition entries for correctness.
func (c *ProjectConfig) validateAgents() error {
	seen := make(map[string]bool, len(c.Agents))
	for i, a := range c.Agents {
		if a.Name == "" {
			return fmt.Errorf("agents[%d]: name is required", i)
		}
		if IsBuiltInMode(a.Name) {
			return fmt.Errorf("agents[%d]: name %q collides with a built-in agent mode", i, a.Name)
		}
		if seen[a.Name] {
			return fmt.Errorf("agents[%d]: duplicate agent name %q", i, a.Name)
		}
		seen[a.Name] = true

		// Tools == nil → inherit; len == 0 → programmer error (useless agent).
		if a.Tools != nil && len(a.Tools) == 0 {
			return fmt.Errorf("agents[%d] (%q): tools list is empty; omit the field to inherit the build toolset", i, a.Name)
		}
		for _, t := range a.Tools {
			if !validAgentToolNames[t] {
				return fmt.Errorf("agents[%d] (%q): unknown tool name %q", i, a.Name, t)
			}
		}

		// Validate provider reference exists in providers: map.
		if a.Provider != "" {
			if _, ok := c.Providers[a.Provider]; !ok {
				return fmt.Errorf("agent %q: provider %q not defined in providers", a.Name, a.Provider)
			}
		}
	}
	return nil
}
