package config

import (
	"os"
	"path/filepath"
	"testing"
)

func newMCPCRUDFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".orchestra.yml")
	if err := os.WriteFile(cfgPath, []byte("project_root: .\nllm:\n  api_base: http://localhost:1234/v1\n  model: m\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfgPath
}

func TestSetMCPServer_AddsAndPersists(t *testing.T) {
	cfgPath := newMCPCRUDFixture(t)

	err := SetMCPServer(cfgPath, MCPServerConfig{Name: "fs", Command: []string{"npx", "-y", "server-filesystem"}})
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.MCP.Servers) != 1 || cfg.MCP.Servers[0].Name != "fs" {
		t.Fatalf("MCP.Servers = %+v", cfg.MCP.Servers)
	}
}

func TestSetMCPServer_ReplacesExistingByName(t *testing.T) {
	cfgPath := newMCPCRUDFixture(t)
	if err := SetMCPServer(cfgPath, MCPServerConfig{Name: "fs", Command: []string{"old"}}); err != nil {
		t.Fatal(err)
	}

	if err := SetMCPServer(cfgPath, MCPServerConfig{Name: "fs", Command: []string{"new"}}); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.MCP.Servers) != 1 || cfg.MCP.Servers[0].Command[0] != "new" {
		t.Fatalf("expected replacement not duplication, got %+v", cfg.MCP.Servers)
	}
}

func TestSetMCPServer_RejectsBothTransports(t *testing.T) {
	cfgPath := newMCPCRUDFixture(t)
	err := SetMCPServer(cfgPath, MCPServerConfig{Name: "bad", Command: []string{"x"}, URL: "https://example.com"})
	if err == nil {
		t.Fatal("must reject a server with both command and url")
	}
}

func TestRemoveMCPServer_RemovesOwnEntry(t *testing.T) {
	cfgPath := newMCPCRUDFixture(t)
	if err := SetMCPServer(cfgPath, MCPServerConfig{Name: "fs", Command: []string{"npx"}}); err != nil {
		t.Fatal(err)
	}

	removed, err := RemoveMCPServer(cfgPath, "fs")
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("expected removed=true")
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.MCP.Servers) != 0 {
		t.Fatalf("MCP.Servers = %+v, want empty", cfg.MCP.Servers)
	}
}

func TestRemoveMCPServer_UnknownNameReturnsFalseNoError(t *testing.T) {
	cfgPath := newMCPCRUDFixture(t)
	removed, err := RemoveMCPServer(cfgPath, "nope")
	if err != nil {
		t.Fatal(err)
	}
	if removed {
		t.Error("expected removed=false for an unknown name")
	}
}

func TestRemoveMCPServer_CannotRemoveAnMCPJSONOnlyServer(t *testing.T) {
	cfgPath := newMCPCRUDFixture(t)
	dir := filepath.Dir(cfgPath)
	writeMCPJSON(t, dir, `{"mcpServers":{"fromjson":{"command":"npx"}}}`)

	// It shows up via Load() (merged)...
	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.MCP.Servers) != 1 {
		t.Fatalf("precondition: expected the .mcp.json server merged in, got %+v", cfg.MCP.Servers)
	}

	// ...but `mcp remove` only edits .orchestra.yml, so it cannot remove
	// something that lives in a different file. Silently reporting
	// removed=true here would lie: the very next Load() brings it right
	// back, since .mcp.json was never touched.
	removed, err := RemoveMCPServer(cfgPath, "fromjson")
	if err != nil {
		t.Fatal(err)
	}
	if removed {
		t.Error("must not report removed=true for a server this file does not own")
	}
}

func TestGetMCPServer_FindsOwnAndMCPJSONServers(t *testing.T) {
	cfgPath := newMCPCRUDFixture(t)
	dir := filepath.Dir(cfgPath)
	writeMCPJSON(t, dir, `{"mcpServers":{"fromjson":{"command":"npx"}}}`)
	if err := SetMCPServer(cfgPath, MCPServerConfig{Name: "own", Command: []string{"y"}}); err != nil {
		t.Fatal(err)
	}

	if srv, ok, err := GetMCPServer(cfgPath, "own"); err != nil || !ok || srv.Command[0] != "y" {
		t.Errorf("own server: srv=%+v ok=%v err=%v", srv, ok, err)
	}
	if srv, ok, err := GetMCPServer(cfgPath, "fromjson"); err != nil || !ok || srv.Command[0] != "npx" {
		t.Errorf("get should also see .mcp.json servers (inspection, not ownership): srv=%+v ok=%v err=%v", srv, ok, err)
	}
	if _, ok, err := GetMCPServer(cfgPath, "nope"); err != nil || ok {
		t.Errorf("unknown name: ok=%v err=%v", ok, err)
	}
}
