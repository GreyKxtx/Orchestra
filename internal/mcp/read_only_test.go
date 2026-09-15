package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/llm"
)

func mutatingByName(defs []llm.ToolDef) map[string]bool {
	out := map[string]bool{}
	for _, d := range defs {
		out[d.Function.Name] = d.Mutating
	}
	return out
}

// Orchestra did not read tool annotations, so it could not tell a server's
// read_file from its write_file, and every MCP tool ran unasked in every mode —
// server-filesystem wrote a file to disk in a turn that was not applying
// changes. The server's readOnlyHint is now carried on the tool def; without it
// a tool counts as one that changes things.
func TestMCPManager_ToolsWithoutReadOnlyHintAreMutating(t *testing.T) {
	t.Setenv("ORCH_TEST_MCP_SERVER", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	mgr, errs := NewManager(ctx, config.MCPConfig{Servers: []config.MCPServerConfig{{
		Name: "testserver", Command: testSelfBinary(),
	}}}, t.TempDir())
	if len(errs) > 0 {
		t.Fatalf("NewManager: %v", errs)
	}
	defer mgr.Close()

	got := mutatingByName(mgr.ListToolDefs())
	if m, ok := got["mcp:testserver:cwd"]; !ok || m {
		t.Errorf("cwd is marked readOnlyHint: true and must not be Mutating (present=%v mutating=%v)", ok, m)
	}
	if m, ok := got["mcp:testserver:echo"]; !ok || !m {
		t.Errorf("echo has no annotations and must be Mutating (present=%v mutating=%v)", ok, m)
	}
}

func TestRemoteClient_CarriesReadOnlyHint(t *testing.T) {
	endpoint, _ := startTestMCPServer(t)
	c, err := StartRemote(context.Background(), RemoteConfig{Name: "test", URL: endpoint}, StartOptions{})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	hints := map[string]bool{}
	for _, tool := range c.Tools() {
		hints[tool.Name] = tool.Annotations.ReadOnlyHint
	}
	if !hints["echo"] {
		t.Errorf("echo is annotated readOnlyHint: true over Streamable HTTP; got %v", hints)
	}
	if hints["secret"] {
		t.Errorf("secret has no annotations and must not read as read-only; got %v", hints)
	}
}
