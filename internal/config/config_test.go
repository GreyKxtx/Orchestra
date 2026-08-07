package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestExecConfig_IsCommandAllowed(t *testing.T) {
	cases := []struct {
		name string
		cfg  ExecConfig
		cmd  string
		want bool
	}{
		{
			name: "empty lists deny all",
			cfg:  ExecConfig{},
			cmd:  "go",
			want: false,
		},
		{
			name: "command in allow list",
			cfg:  ExecConfig{Allow: []string{"go", "npm"}},
			cmd:  "go",
			want: true,
		},
		{
			name: "command not in allow list",
			cfg:  ExecConfig{Allow: []string{"go", "npm"}},
			cmd:  "curl",
			want: false,
		},
		{
			name: "deny list blocks even if in allow",
			cfg:  ExecConfig{Allow: []string{"go"}, Deny: []string{"go"}},
			cmd:  "go",
			want: false,
		},
		{
			name: "deny list only - blocks listed",
			cfg:  ExecConfig{Deny: []string{"rm", "curl"}},
			cmd:  "rm",
			want: false,
		},
		{
			name: "deny list only - allows unlisted (empty allow = deny all, deny list irrelevant)",
			cfg:  ExecConfig{Deny: []string{"rm"}},
			cmd:  "go",
			want: false, // allow list empty → deny all
		},
		{
			name: "deny list + allow list - unlisted allowed cmd passes",
			cfg:  ExecConfig{Allow: []string{"go", "npm"}, Deny: []string{"curl"}},
			cmd:  "npm",
			want: true,
		},
		{
			name: "case insensitive allow",
			cfg:  ExecConfig{Allow: []string{"Go"}},
			cmd:  "go",
			want: true,
		},
		{
			name: "case insensitive deny",
			cfg:  ExecConfig{Allow: []string{"go"}, Deny: []string{"RM"}},
			cmd:  "rm",
			want: false,
		},
		{
			name: "windows .exe stripped",
			cfg:  ExecConfig{Allow: []string{"go"}},
			cmd:  "go.exe",
			want: true,
		},
		{
			name: "full path - basename used",
			cfg:  ExecConfig{Allow: []string{"go"}},
			cmd:  "/usr/local/bin/go",
			want: true,
		},
		{
			name: "empty command denied",
			cfg:  ExecConfig{Allow: []string{"go"}},
			cmd:  "",
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.cfg.IsCommandAllowed(tc.cmd)
			if got != tc.want {
				t.Errorf("IsCommandAllowed(%q) = %v, want %v", tc.cmd, got, tc.want)
			}
		})
	}
}

func TestValidateAgents(t *testing.T) {
	baseValid := func() *ProjectConfig {
		cfg := DefaultConfig("/tmp")
		return cfg
	}

	cases := []struct {
		name    string
		agents  []AgentDefinition
		wantErr string // substring of expected error; "" = no error
	}{
		{
			name:    "no agents — valid",
			agents:  nil,
			wantErr: "",
		},
		{
			name: "valid advisor agent",
			agents: []AgentDefinition{
				{Name: "advisor", SystemPrompt: "review", Tools: []string{"read", "grep"}},
			},
			wantErr: "",
		},
		{
			name: "valid agent with nil tools (inherits build set)",
			agents: []AgentDefinition{
				{Name: "helper", SystemPrompt: "help"},
			},
			wantErr: "",
		},
		{
			name: "valid agent with model override",
			agents: []AgentDefinition{
				{Name: "smart", Model: "claude-opus-4-7", Tools: []string{"read"}},
			},
			wantErr: "",
		},
		{
			name: "empty name",
			agents: []AgentDefinition{
				{Name: ""},
			},
			wantErr: "name is required",
		},
		{
			name: "collision with built-in mode build",
			agents: []AgentDefinition{
				{Name: "build"},
			},
			wantErr: "collides with a built-in agent mode",
		},
		{
			name: "collision with built-in mode plan",
			agents: []AgentDefinition{
				{Name: "plan"},
			},
			wantErr: "collides with a built-in agent mode",
		},
		{
			name: "duplicate names",
			agents: []AgentDefinition{
				{Name: "advisor"},
				{Name: "advisor"},
			},
			wantErr: "duplicate agent name",
		},
		{
			name: "empty tools list (not nil)",
			agents: []AgentDefinition{
				{Name: "myagent", Tools: []string{}},
			},
			wantErr: "tools list is empty",
		},
		{
			name: "unknown tool name",
			agents: []AgentDefinition{
				{Name: "myagent", Tools: []string{"read", "fly"}},
			},
			wantErr: `unknown tool name "fly"`,
		},
		{
			name: "multiple valid agents",
			agents: []AgentDefinition{
				{Name: "advisor", Tools: []string{"read", "grep"}},
				{Name: "writer", Tools: []string{"write", "edit"}},
			},
			wantErr: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := baseValid()
			cfg.Agents = tc.agents
			err := cfg.validateAgents()
			if tc.wantErr == "" {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Errorf("expected error containing %q, got nil", tc.wantErr)
				return
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestFindAgent(t *testing.T) {
	cfg := &ProjectConfig{
		Agents: []AgentDefinition{
			{Name: "advisor", SystemPrompt: "review code"},
			{Name: "writer", SystemPrompt: "write code"},
		},
	}

	got := cfg.FindAgent("advisor")
	if got == nil || got.SystemPrompt != "review code" {
		t.Errorf("FindAgent(advisor) = %v, want advisor", got)
	}

	got = cfg.FindAgent("writer")
	if got == nil || got.Name != "writer" {
		t.Errorf("FindAgent(writer) = %v, want writer", got)
	}

	got = cfg.FindAgent("unknown")
	if got != nil {
		t.Errorf("FindAgent(unknown) = %v, want nil", got)
	}

	empty := &ProjectConfig{}
	if empty.FindAgent("x") != nil {
		t.Error("FindAgent on empty config should return nil")
	}
}

func TestWebSearchConfig_DefaultProvider(t *testing.T) {
	cfg := &ProjectConfig{}
	cfg.applyDefaults()
	if cfg.Web.Search.Provider != "" {
		t.Errorf("Web.Search.Provider = %q, want empty string", cfg.Web.Search.Provider)
	}
}

func TestWebSearchConfig_YAML(t *testing.T) {
	raw := `
web:
  search:
    provider: tavily
    api_key: tvly-test123
    max_results: 10
`
	var cfg ProjectConfig
	if err := yaml.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	if cfg.Web.Search.Provider != "tavily" {
		t.Errorf("Provider = %q, want %q", cfg.Web.Search.Provider, "tavily")
	}
	if cfg.Web.Search.APIKey != "tvly-test123" {
		t.Errorf("APIKey = %q, want %q", cfg.Web.Search.APIKey, "tvly-test123")
	}
	if cfg.Web.Search.MaxResults != 10 {
		t.Errorf("MaxResults = %d, want %d", cfg.Web.Search.MaxResults, 10)
	}
}

func TestFindProvider_InheritsAPIKeyFromLLM(t *testing.T) {
	cfg := &ProjectConfig{
		LLM: LLMConfig{
			Provider: "vllm",
			APIBase:  "https://example.ngrok-free.dev/v1",
			APIKey:   "secret-from-llm",
		},
		Providers: map[string]LLMConfig{
			"vllm": {Provider: "vllm", APIBase: "https://example.ngrok-free.dev/v1"},
		},
	}
	prov, ok := cfg.FindProvider("vllm")
	if !ok {
		t.Fatal("expected find")
	}
	if prov.APIKey != "secret-from-llm" {
		t.Fatalf("APIKey = %q, want inherited from llm:", prov.APIKey)
	}
}

func TestFindProvider_InheritsMaxTokensFromLLM(t *testing.T) {
	cfg := &ProjectConfig{
		LLM: LLMConfig{
			Provider:    "vllm",
			APIBase:     "https://example.ngrok-free.dev/v1",
			MaxTokens:   8192,
			Temperature: 0.2,
			ToolChoice:  "omit",
			ExtraBody:   map[string]any{"num_ctx": 20000},
		},
		Providers: map[string]LLMConfig{
			"vllm": {Provider: "vllm", APIBase: "https://example.ngrok-free.dev/v1", Model: "qwen"},
		},
	}
	prov, ok := cfg.FindProvider("vllm")
	if !ok {
		t.Fatal("expected find")
	}
	if prov.MaxTokens != 8192 {
		t.Fatalf("MaxTokens = %d, want 8192", prov.MaxTokens)
	}
	if prov.ToolChoice != "omit" {
		t.Fatalf("ToolChoice = %q", prov.ToolChoice)
	}
	if prov.ExtraBody["num_ctx"] == nil {
		t.Fatal("ExtraBody not inherited")
	}
}

func TestFindProvider_Found(t *testing.T) {
	cfg := &ProjectConfig{
		Providers: map[string]LLMConfig{
			"anthropic": {Provider: "anthropic", APIKey: "sk-ant-test"},
		},
	}
	prov, ok := cfg.FindProvider("anthropic")
	if !ok {
		t.Fatal("FindProvider: expected true for 'anthropic'")
	}
	if prov.Provider != "anthropic" {
		t.Fatalf("FindProvider: expected Provider='anthropic', got %q", prov.Provider)
	}
	if prov.APIKey != "sk-ant-test" {
		t.Fatalf("FindProvider: expected APIKey='sk-ant-test', got %q", prov.APIKey)
	}
}

func TestFindProvider_NotFound(t *testing.T) {
	cfg := &ProjectConfig{}
	_, ok := cfg.FindProvider("missing")
	if ok {
		t.Fatal("FindProvider: expected false for missing provider")
	}
}

func TestFindProvider_NilMap(t *testing.T) {
	cfg := &ProjectConfig{Providers: nil}
	_, ok := cfg.FindProvider("any")
	if ok {
		t.Fatal("FindProvider: expected false when Providers is nil")
	}
}

func TestFindProvider_EmptyName(t *testing.T) {
	cfg := &ProjectConfig{
		Providers: map[string]LLMConfig{
			"anthropic": {Provider: "anthropic"},
		},
	}
	_, ok := cfg.FindProvider("")
	if ok {
		t.Fatal("FindProvider: empty name should not match any provider")
	}
}

func TestValidate_AgentProviderNotDefined(t *testing.T) {
	cfg := DefaultConfig(".")
	cfg.Agents = []AgentDefinition{
		{Name: "myagent", Provider: "nonexistent"},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for agent with undefined provider, got nil")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Fatalf("expected error to mention provider name, got: %v", err)
	}
}

func TestFindAgent_WithProvider(t *testing.T) {
	cfg := &ProjectConfig{
		Providers: map[string]LLMConfig{
			"anthropic": {Provider: "anthropic", APIKey: "key"},
		},
		Agents: []AgentDefinition{
			{Name: "reviewer", Provider: "anthropic", Model: "claude-3-5-sonnet-20241022"},
		},
	}
	agent := cfg.FindAgent("reviewer")
	if agent == nil {
		t.Fatal("FindAgent: expected to find 'reviewer'")
	}
	if agent.Provider != "anthropic" {
		t.Fatalf("expected Provider='anthropic', got %q", agent.Provider)
	}
	prov, ok := cfg.FindProvider(agent.Provider)
	if !ok {
		t.Fatal("FindProvider: expected to find provider referenced by agent")
	}
	if prov.APIKey != "key" {
		t.Fatalf("expected APIKey='key', got %q", prov.APIKey)
	}
}

func TestValidate_ApplyOutputAndProfile(t *testing.T) {
	cfg := DefaultConfig("/tmp/proj")
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("defaults should validate: %v", err)
	}

	cfg.Apply.Output = "nope"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "apply.output") {
		t.Fatalf("expected apply.output error, got %v", err)
	}
	cfg.Apply.Output = ApplyOutputPatch
	cfg.Agent.Profile = "turbo"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "agent.profile") {
		t.Fatalf("expected agent.profile error, got %v", err)
	}
	cfg.Agent.Profile = "fast"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("fast+patch should validate: %v", err)
	}
}

func TestValidate_LSPAutoInstall(t *testing.T) {
	cfg := DefaultConfig("/tmp/proj")
	cfg.applyDefaults()
	if cfg.LSP.EffectiveAutoInstall() != "true" {
		t.Fatalf("default EffectiveAutoInstall=%q", cfg.LSP.EffectiveAutoInstall())
	}
	cfg.LSP.AutoInstall = "maybe"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "lsp.auto_install") {
		t.Fatalf("expected lsp.auto_install error, got %v", err)
	}
	cfg.LSP.AutoInstall = "true"
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.LSP.EffectiveAutoInstall() != "true" {
		t.Fatal(cfg.LSP.EffectiveAutoInstall())
	}
}

func TestApplyDefaults_PatchDir(t *testing.T) {
	cfg := &ProjectConfig{
		ProjectRoot:  "/tmp",
		ContextLimit: 50,
		Limits:       LimitsConfig{ContextKB: 50},
		LLM:          LLMConfig{APIBase: "http://x", Model: "m", TimeoutS: 1},
		Exec:         ExecConfig{TimeoutS: 1, OutputLimitKB: 1},
	}
	cfg.applyDefaults()
	if cfg.Apply.Output != ApplyOutputDisk {
		t.Fatalf("output=%q", cfg.Apply.Output)
	}
	if cfg.Apply.PatchDir != ".orchestra/patches" {
		t.Fatalf("patch_dir=%q", cfg.Apply.PatchDir)
	}
}

func TestEffectiveNumCtx(t *testing.T) {
	cfg := &ProjectConfig{
		LLM: LLMConfig{
			Model:     "qwen/qwen3.6-27b",
			ExtraBody: map[string]any{"num_ctx": 20000},
			ModelPresets: map[string]ModelPreset{
				"qwen/qwen3.6-27b": {NumCtx: 70000},
			},
		},
	}
	if got := cfg.EffectiveNumCtx(); got != 70000 {
		t.Fatalf("preset: got %d want 70000", got)
	}
	cfg.LLM.ModelPresets = nil
	if got := cfg.EffectiveNumCtx(); got != 20000 {
		t.Fatalf("extra_body: got %d want 20000", got)
	}
	cfg.LLM.ExtraBody = nil
	if got := cfg.EffectiveNumCtx(); got != 0 {
		t.Fatalf("empty: got %d want 0", got)
	}
}

func TestEffectiveMaxPromptBytes(t *testing.T) {
	cfg := &ProjectConfig{
		Limits: LimitsConfig{ContextKB: 64},
		LLM: LLMConfig{
			Model:     "qwen/qwen3.6-27b",
			MaxTokens: 8192,
			ExtraBody: map[string]any{"num_ctx": 60000},
		},
	}
	// promptTok = 60000 - 8192 - 2048 = 49760; bytes = 49760 * 4
	want := (60000 - 8192 - 2048) * bytesPerContextToken
	if got := cfg.EffectiveMaxPromptBytes(); got != want {
		t.Fatalf("num_ctx share: got %d want %d", got, want)
	}
	cfg.LLM.ExtraBody = nil
	if got := cfg.EffectiveMaxPromptBytes(); got != 64*1024 {
		t.Fatalf("context_kb only: got %d want %d", got, 64*1024)
	}
}

func TestApplyDefaults_CompactThreshold(t *testing.T) {
	cfg := &ProjectConfig{}
	cfg.applyDefaults()
	if cfg.Agent.CompactThresholdPct != 60 {
		t.Fatalf("default compact=%d want 60", cfg.Agent.CompactThresholdPct)
	}
	cfg.Agent.CompactThresholdPct = -1
	cfg.applyDefaults()
	if cfg.Agent.CompactThresholdPct != 0 {
		t.Fatalf("disabled compact=%d want 0", cfg.Agent.CompactThresholdPct)
	}
}

func TestValidate_OrchestraAndAutoRouter(t *testing.T) {
	cfg := DefaultConfig("/tmp/proj")
	cfg.applyDefaults()
	cfg.Providers = map[string]LLMConfig{
		"fast": {APIBase: "http://x", Model: "m", TimeoutS: 1},
	}
	cfg.Orchestra = OrchestraConfig{
		Planner: OrchestraRole{Provider: "missing"},
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "orchestra.planner") {
		t.Fatalf("expected planner provider error, got %v", err)
	}
	cfg.Orchestra.Planner.Provider = "fast"
	cfg.Orchestra.Tiers = []OrchestraTier{{Name: "focused", Provider: "fast"}}
	cfg.Orchestra.DefaultTier = "nope"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "default_tier") {
		t.Fatalf("expected default_tier error, got %v", err)
	}
	cfg.Orchestra.DefaultTier = "focused"
	cfg.AutoRouter.Provider = "missing"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "auto_router") {
		t.Fatalf("expected auto_router error, got %v", err)
	}
	cfg.AutoRouter.Provider = "fast"
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}
