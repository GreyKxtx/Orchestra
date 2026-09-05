package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Load() merges .mcp.json servers into cfg.MCP.Servers in memory (previous
// commit). Save() must not let that merge leak into the committed
// .orchestra.yml — exactly the same ownership rule already enforced for the
// local and global config layers.

func TestSave_DoesNotLeakMCPJSONServersIntoOrchestraYML(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".orchestra.yml")
	if err := os.WriteFile(cfgPath, []byte("project_root: .\nllm:\n  api_base: http://localhost:1234/v1\n  model: m\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeMCPJSON(t, dir, `{"mcpServers":{"filesystem":{"command":"npx","args":["-y","server-filesystem"]}}}`)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.MCP.Servers) != 1 {
		t.Fatalf("precondition: expected the .mcp.json server merged in, got %+v", cfg.MCP.Servers)
	}

	if err := Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "filesystem") {
		t.Errorf(".mcp.json server leaked into the committed config:\n%s", raw)
	}
}

func TestSave_KeepsExplicitlyOwnedMCPServerDespiteMCPJSON(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".orchestra.yml")
	yml := "project_root: .\nllm:\n  api_base: http://localhost:1234/v1\n  model: m\n" +
		"mcp:\n  servers:\n    - name: shared\n      command: [\"own-binary\"]\n"
	if err := os.WriteFile(cfgPath, []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}
	writeMCPJSON(t, dir, `{"mcpServers":{"shared":{"command":"json-binary"}}}`)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}

	after, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.MCP.Servers) != 1 || after.MCP.Servers[0].Command[0] != "own-binary" {
		t.Errorf("explicitly-owned server lost or overwritten: %+v", after.MCP.Servers)
	}
}
