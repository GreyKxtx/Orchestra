package ckg

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseModulePath(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("// header\n\nmodule example.com/foo/bar\n\ngo 1.25\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := ParseModulePath(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if got != "example.com/foo/bar" {
		t.Fatalf("got %q, want example.com/foo/bar", got)
	}
}

func TestParseModulePathMissing(t *testing.T) {
	tmp := t.TempDir()
	got, err := ParseModulePath(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestGoFQN(t *testing.T) {
	root := filepath.FromSlash("/repo")
	tests := []struct {
		name, modulePath, file, recvType, symbol, want string
	}{
		{"top-level func", "github.com/x/y", "/repo/internal/agent/agent.go", "", "Run",
			"github.com/x/y/internal/agent.Run"},
		{"method", "github.com/x/y", "/repo/internal/agent/agent.go", "Agent", "Run",
			"github.com/x/y/internal/agent.Agent.Run"},
		{"root pkg", "github.com/x/y", "/repo/main.go", "", "main",
			"github.com/x/y.main"},
		{"no module path", "", "/repo/internal/agent/agent.go", "", "Run",
			"internal/agent.Run"},
		{"no module path root file", "", "/repo/main.go", "", "Run",
			"Run"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GoFQN(tt.modulePath, root, filepath.FromSlash(tt.file), tt.recvType, tt.symbol)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsLikelyFQN(t *testing.T) {
	cases := map[string]bool{
		"Run":                      false,
		"Agent.Run":                false,
		"github.com/x/y.Agent.Run": true,
		"internal/agent.Run":       true,
		"fmt.Println":              false,
	}
	for in, want := range cases {
		if got := IsLikelyFQN(in); got != want {
			t.Errorf("IsLikelyFQN(%q): got %v, want %v", in, got, want)
		}
	}
}
