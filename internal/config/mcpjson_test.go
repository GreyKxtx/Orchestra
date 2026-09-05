package config

import (
	"os"
	"path/filepath"
	"testing"
)

// A server configured for Claude Code or Cursor already carries everything
// Orchestra needs (command/args/env, or url/headers) — .mcp.json support
// means it works here too, without retyping it into .orchestra.yml.

func writeMCPJSON(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadMCPJSON_ParsesStdioAndRemoteServers(t *testing.T) {
	dir := t.TempDir()
	writeMCPJSON(t, dir, `{
  "mcpServers": {
    "filesystem": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"],
      "env": { "API_KEY": "secret" }
    },
    "linear": {
      "url": "https://mcp.linear.app/sse",
      "headers": { "X-Custom": "1" }
    }
  }
}`)

	servers, err := LoadMCPJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 2 {
		t.Fatalf("servers = %d, want 2", len(servers))
	}
	byName := map[string]MCPServerConfig{}
	for _, s := range servers {
		byName[s.Name] = s
	}

	fs := byName["filesystem"]
	wantCmd := []string{"npx", "-y", "@modelcontextprotocol/server-filesystem", "/tmp"}
	if len(fs.Command) != len(wantCmd) {
		t.Fatalf("filesystem.Command = %v, want %v", fs.Command, wantCmd)
	}
	for i := range wantCmd {
		if fs.Command[i] != wantCmd[i] {
			t.Fatalf("filesystem.Command = %v, want %v", fs.Command, wantCmd)
		}
	}
	if fs.Env["API_KEY"] != "secret" {
		t.Errorf("filesystem.Env = %v", fs.Env)
	}

	linear := byName["linear"]
	if linear.URL != "https://mcp.linear.app/sse" {
		t.Errorf("linear.URL = %q", linear.URL)
	}
	if linear.Headers["X-Custom"] != "1" {
		t.Errorf("linear.Headers = %v", linear.Headers)
	}
}

func TestLoadMCPJSON_MissingFileReturnsNilNoError(t *testing.T) {
	servers, err := LoadMCPJSON(t.TempDir())
	if err != nil || servers != nil {
		t.Fatalf("got %v, %v; want nil, nil", servers, err)
	}
}

func TestLoadMCPJSON_MalformedJSONIsAnError(t *testing.T) {
	dir := t.TempDir()
	writeMCPJSON(t, dir, `{not json`)

	if _, err := LoadMCPJSON(dir); err == nil {
		t.Fatal("malformed .mcp.json must be a reported error, not silently ignored")
	}
}

func TestLoadMCPJSON_DisabledServerCarriesThrough(t *testing.T) {
	dir := t.TempDir()
	writeMCPJSON(t, dir, `{"mcpServers":{"off":{"command":"foo","disabled":true}}}`)

	servers, err := LoadMCPJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 || !servers[0].Disabled {
		t.Fatalf("servers = %+v, want one disabled server", servers)
	}
}

func TestMergeMCPServers_OwnConfigWinsOnNameConflict(t *testing.T) {
	own := []MCPServerConfig{{Name: "shared", Command: []string{"own-binary"}}}
	fromJSON := []MCPServerConfig{{Name: "shared", Command: []string{"json-binary"}}, {Name: "extra", Command: []string{"x"}}}

	merged := MergeMCPServers(own, fromJSON)

	if len(merged) != 2 {
		t.Fatalf("merged = %+v, want 2 entries", merged)
	}
	byName := map[string]MCPServerConfig{}
	for _, s := range merged {
		byName[s.Name] = s
	}
	if byName["shared"].Command[0] != "own-binary" {
		t.Errorf(".orchestra.yml must win a name conflict, got %+v", byName["shared"])
	}
	if byName["extra"].Command[0] != "x" {
		t.Errorf(".mcp.json-only server must still appear, got %+v", byName["extra"])
	}
}

func TestLoad_MergesMCPJSON(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".orchestra.yml")
	if err := os.WriteFile(cfgPath, []byte("project_root: .\nllm:\n  api_base: http://localhost:1234/v1\n  model: m\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeMCPJSON(t, dir, `{"mcpServers":{"filesystem":{"command":"npx","args":["-y","server-filesystem"]}}}`)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.MCP.Servers) != 1 || cfg.MCP.Servers[0].Name != "filesystem" {
		t.Fatalf("MCP.Servers = %+v, want the .mcp.json server merged in", cfg.MCP.Servers)
	}
}

func TestLoad_OwnMCPServersWinOverMCPJSON(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".orchestra.yml")
	yml := "project_root: .\nllm:\n  api_base: http://localhost:1234/v1\n  model: m\n" +
		"mcp:\n  servers:\n    - name: shared\n      command: [\"own\"]\n"
	if err := os.WriteFile(cfgPath, []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}
	writeMCPJSON(t, dir, `{"mcpServers":{"shared":{"command":"from-json"}}}`)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.MCP.Servers) != 1 || cfg.MCP.Servers[0].Command[0] != "own" {
		t.Fatalf("MCP.Servers = %+v, want .orchestra.yml's own server to win", cfg.MCP.Servers)
	}
}

func TestMergeMCPServers_EmptyInputsAreFine(t *testing.T) {
	if got := MergeMCPServers(nil, nil); len(got) != 0 {
		t.Errorf("got %+v, want empty", got)
	}
	if got := MergeMCPServers([]MCPServerConfig{{Name: "a"}}, nil); len(got) != 1 {
		t.Errorf("got %+v, want 1", got)
	}
}
