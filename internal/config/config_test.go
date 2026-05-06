package config

import (
	"strings"
	"testing"
)

func TestExecConfig_IsCommandAllowed(t *testing.T) {
	cases := []struct {
		name    string
		cfg     ExecConfig
		cmd     string
		want    bool
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
