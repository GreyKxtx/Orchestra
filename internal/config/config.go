package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// LLMConfig contains LLM API settings
type LLMConfig struct {
	// Provider selects the API provider: "openai" (default), "anthropic".
	Provider    string  `yaml:"provider"`
	APIBase     string  `yaml:"api_base"`
	APIKey      string  `yaml:"api_key"`
	Model       string  `yaml:"model"`
	MaxTokens   int     `yaml:"max_tokens"`
	Temperature float32 `yaml:"temperature"`
	// TimeoutS bounds a single LLM request (agent step attempt).
	TimeoutS int `yaml:"timeout_s"`

	// PromptFamily selects a model-family-specific system prompt template.
	// Auto-detected from Model name if empty.
	// Supported: "openai" (default), "qwen", "llama", "mistral", "deepseek".
	PromptFamily string `yaml:"prompt_family"`

	// ResponseFormatType requests structured output from the provider.
	// "" (default) — no constraint; "json_object" — valid JSON output;
	// "json_schema" — strict schema-constrained JSON (requires provider support).
	// Set "json_object" for vLLM/lm-studio; leave empty for cloud APIs.
	ResponseFormatType string `yaml:"response_format_type"`

	// ExtraBody contains arbitrary key-value pairs merged into every API request body.
	// Use this to pass provider-specific parameters, e.g.:
	//   extra_body:
	//     chat_template_kwargs:
	//       enable_thinking: false
	ExtraBody map[string]any `yaml:"extra_body,omitempty"`
}

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
}

// DaemonConfig contains local daemon settings (v0.3+).
type DaemonConfig struct {
	Enabled      bool   `yaml:"enabled"`
	Address      string `yaml:"address"`
	Port         int    `yaml:"port"`
	ScanInterval int    `yaml:"scan_interval"` // seconds

	CacheEnabled *bool  `yaml:"cache_enabled"`
	CachePath    string `yaml:"cache_path"` // default: .orchestra/cache.json
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
	Allow         []string `yaml:"allow,omitempty"`  // commands explicitly allowed (basename, e.g. "go", "npm")
	Deny          []string `yaml:"deny,omitempty"`   // commands explicitly denied (takes precedence over Allow)
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

// LanguagesConfig selects enabled language parsers (vNext).
type LanguagesConfig struct {
	Enabled []string `yaml:"enabled"`
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
}

// MCPConfig holds the list of MCP servers to connect to.
type MCPConfig struct {
	Servers []MCPServerConfig `yaml:"servers,omitempty"`
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

// ProjectConfig represents the Orchestra configuration
type ProjectConfig struct {
	ProjectRoot string   `yaml:"project_root"`
	ExcludeDirs []string `yaml:"exclude_dirs"`
	// ContextLimit is the v0.2/v0.3 name kept for backward compatibility.
	// Prefer Limits.ContextKB.
	ContextLimit int             `yaml:"context_limit_kb"`
	Limits       LimitsConfig    `yaml:"limits"`
	LLM          LLMConfig       `yaml:"llm"`
	Agent        AgentConfig     `yaml:"agent"`
	Daemon       DaemonConfig    `yaml:"daemon"`
	Exec         ExecConfig      `yaml:"exec"`
	Hooks        HooksConfig     `yaml:"hooks"`
	MCP          MCPConfig       `yaml:"mcp"`
	Languages    LanguagesConfig `yaml:"languages"`
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
			ContextKB:       50,
			MaxFiles:        30,
			MaxBytesPerFile: 200 * 1024,
		},
		LLM: LLMConfig{
			APIBase:     "http://localhost:8000/v1",
			Model:       "qwen2.5-coder-7b",
			Temperature: 0.7,
			MaxTokens:   4096,
			TimeoutS:    300,
			// ResponseFormatType: "json_object" — раскомментируй если провайдер поддерживает
		},
		Agent: AgentConfig{
			MaxSteps:          24,
			MaxInvalidRetries: 3,
			MaxFinalFailures:  6,
			MaxToolErrors:     6,
			MaxDeniedRepeats:  2,
		},
		Daemon: DaemonConfig{
			Enabled:      false,
			Address:      "127.0.0.1",
			Port:         8080,
			ScanInterval: 10,
			CacheEnabled: boolPtr(true),
			CachePath:    ".orchestra/cache.json",
		},
		Exec: ExecConfig{
			Confirm:       boolPtr(true),
			TimeoutS:      30,
			OutputLimitKB: 100,
		},
		Languages: LanguagesConfig{
			Enabled: []string{"go"},
		},
	}
}

// Load loads configuration from file
func Load(path string) (*ProjectConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg ProjectConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	cfg.applyDefaults()

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}

// Save saves configuration to file
func Save(path string, cfg *ProjectConfig) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

func (c *ProjectConfig) applyDefaults() {
	// Daemon defaults (so older configs without daemon section still work).
	if c.Daemon.Address == "" {
		c.Daemon.Address = "127.0.0.1"
	}
	if c.Daemon.Port == 0 {
		c.Daemon.Port = 8080
	}
	if c.Daemon.ScanInterval == 0 {
		c.Daemon.ScanInterval = 10
	}
	if c.Daemon.CachePath == "" {
		c.Daemon.CachePath = ".orchestra/cache.json"
	}
	// Default true for cache unless explicitly set to false.
	if c.Daemon.CacheEnabled == nil {
		c.Daemon.CacheEnabled = boolPtr(true)
	}

	// vNext limits: inherit legacy context_limit_kb if limits.context_kb is missing.
	if c.Limits.ContextKB <= 0 && c.ContextLimit > 0 {
		c.Limits.ContextKB = c.ContextLimit
	}
	if c.Limits.ContextKB <= 0 {
		c.Limits.ContextKB = 50
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

	// Languages defaults
	if len(c.Languages.Enabled) == 0 {
		c.Languages.Enabled = []string{"go"}
	}

	// LLM defaults
	if c.LLM.TimeoutS <= 0 {
		c.LLM.TimeoutS = 300
	}

	// Agent defaults (tuned for local models).
	if c.Agent.MaxSteps <= 0 {
		c.Agent.MaxSteps = 24
	}
	if c.Agent.MaxInvalidRetries <= 0 {
		c.Agent.MaxInvalidRetries = 3
	}
	if c.Agent.MaxFinalFailures <= 0 {
		c.Agent.MaxFinalFailures = 6
	}
	if c.Agent.MaxToolErrors <= 0 {
		c.Agent.MaxToolErrors = 6
	}
	if c.Agent.MaxDeniedRepeats <= 0 {
		c.Agent.MaxDeniedRepeats = 2
	}
}

func boolPtr(v bool) *bool { return &v }

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

	if c.Daemon.Port < 0 || c.Daemon.Port > 65535 {
		return fmt.Errorf("daemon.port must be between 0 and 65535")
	}
	if c.Daemon.ScanInterval < 0 {
		return fmt.Errorf("daemon.scan_interval must be >= 0")
	}

	return nil
}
