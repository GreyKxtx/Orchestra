package config

import "testing"

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
