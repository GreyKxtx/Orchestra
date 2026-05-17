package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
)

func TestPrintMCPTools_NoServers(t *testing.T) {
	var buf bytes.Buffer
	err := printMCPTools(&buf, config.MCPConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "No MCP servers configured") {
		t.Fatalf("expected 'No MCP servers configured', got:\n%s", out)
	}
}

func TestPrintMCPTools_DisabledOnly(t *testing.T) {
	var buf bytes.Buffer
	cfg := config.MCPConfig{
		Servers: []config.MCPServerConfig{
			{Name: "myserver", Disabled: true, Command: []string{"fake"}},
		},
	}
	err := printMCPTools(&buf, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "myserver") {
		t.Fatalf("expected server name 'myserver' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "disabled") {
		t.Fatalf("expected 'disabled' in output, got:\n%s", out)
	}
}
