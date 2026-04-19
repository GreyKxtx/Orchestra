package mcp

import (
	"testing"
)

func TestParseMCPToolName_Valid(t *testing.T) {
	srv, tool, err := parseMCPToolName("mcp:myserver:mytool")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if srv != "myserver" {
		t.Fatalf("expected server=myserver, got %q", srv)
	}
	if tool != "mytool" {
		t.Fatalf("expected tool=mytool, got %q", tool)
	}
}

func TestParseMCPToolName_ToolWithColons(t *testing.T) {
	// Tool names may contain colons; SplitN(name, ":", 3) keeps the rest intact
	srv, tool, err := parseMCPToolName("mcp:server:tool:with:colons")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if srv != "server" {
		t.Fatalf("expected server=server, got %q", srv)
	}
	if tool != "tool:with:colons" {
		t.Fatalf("expected tool=tool:with:colons, got %q", tool)
	}
}

func TestParseMCPToolName_InvalidPrefix(t *testing.T) {
	_, _, err := parseMCPToolName("notmcp:server:tool")
	if err == nil {
		t.Fatal("expected error for invalid prefix")
	}
}

func TestParseMCPToolName_MissingServer(t *testing.T) {
	_, _, err := parseMCPToolName("mcp:onlyone")
	if err == nil {
		t.Fatal("expected error when tool part is missing")
	}
}

func TestParseMCPToolName_Empty(t *testing.T) {
	_, _, err := parseMCPToolName("")
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestIsMCPTool_True(t *testing.T) {
	if !IsMCPTool("mcp:server:tool") {
		t.Fatal("expected true for mcp: prefix")
	}
}

func TestIsMCPTool_False(t *testing.T) {
	if IsMCPTool("fs.read") {
		t.Fatal("expected false for non-mcp tool")
	}
	if IsMCPTool("mcpfoo") {
		t.Fatal("expected false — no colon after mcp")
	}
}

func TestManager_IsEmpty_Nil(t *testing.T) {
	var m *Manager
	if !m.IsEmpty() {
		t.Fatal("nil manager should be empty")
	}
}

func TestManager_IsEmpty_NoClients(t *testing.T) {
	m := &Manager{}
	if !m.IsEmpty() {
		t.Fatal("manager with no clients should be empty")
	}
}

func TestManager_ListToolDefs_Empty(t *testing.T) {
	m := &Manager{}
	defs := m.ListToolDefs()
	if defs != nil {
		t.Fatalf("expected nil tool defs for empty manager, got %v", defs)
	}
}

func TestManager_Close_Nil(t *testing.T) {
	var m *Manager
	m.Close() // must not panic
}

func TestManager_Close_Empty(t *testing.T) {
	m := &Manager{}
	m.Close() // must not panic
}

func TestManager_Call_UnknownServer(t *testing.T) {
	m := &Manager{}
	_, err := m.Call(nil, "mcp:ghost:tool", nil)
	if err == nil {
		t.Fatal("expected error for unknown server")
	}
}

func TestManager_Call_InvalidName(t *testing.T) {
	m := &Manager{}
	_, err := m.Call(nil, "invalid-name", nil)
	if err == nil {
		t.Fatal("expected error for invalid tool name")
	}
}
