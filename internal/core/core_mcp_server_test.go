package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/orchestra/orchestra/internal/ckg"
	"github.com/orchestra/orchestra/internal/config"
)

// newToolsOnlyCore builds a Core in ToolsOnly mode against a fresh temp
// workspace with a minimal, real Go module — no LLM, no mocks.
func newToolsOnlyCore(t *testing.T, root string) *Core {
	t.Helper()
	cfgPath := filepath.Join(root, ".orchestra.yml")
	if err := config.Save(cfgPath, config.DefaultConfig(root)); err != nil {
		t.Fatal(err)
	}
	c, err := New(root, Options{ToolsOnly: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// writeMinimalGoModule creates a tiny real Go module in root: one function
// (Hello) that explore/symbols/repo_map can all find something real to say
// about.
func writeMinimalGoModule(t *testing.T, root string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/fixture\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := "package fixture\n\n// Hello returns a greeting.\nfunc Hello(name string) string {\n\treturn \"Hello, \" + name\n}\n"
	if err := os.WriteFile(filepath.Join(root, "fixture.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

// connectMCP wires an in-process client to srv over the SDK's in-memory
// transport pair and returns the connected client session.
func connectMCP(t *testing.T, srv *mcpsdk.Server) *mcpsdk.ClientSession {
	t.Helper()
	ctx := context.Background()
	serverTransport, clientTransport := mcpsdk.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, serverTransport, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test-client", Version: "1"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func TestMCPToolServer_Explore(t *testing.T) {
	root := t.TempDir()
	writeMinimalGoModule(t, root)
	c := newToolsOnlyCore(t, root)

	session := connectMCP(t, c.MCPToolServer())
	res, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "explore",
		Arguments: map[string]any{"symbol_name": "Hello"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned an error result: %+v", res.Content)
	}
	text := res.Content[0].(*mcpsdk.TextContent).Text
	if !strings.Contains(text, "Hello") {
		t.Errorf("explore result = %q, want it to mention Hello", text)
	}
}

func TestMCPToolServer_Symbols(t *testing.T) {
	root := t.TempDir()
	writeMinimalGoModule(t, root)
	c := newToolsOnlyCore(t, root)

	session := connectMCP(t, c.MCPToolServer())
	res, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "symbols",
		Arguments: map[string]any{"path": "fixture.go"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned an error result: %+v", res.Content)
	}
	text := res.Content[0].(*mcpsdk.TextContent).Text
	if !strings.Contains(text, "Hello") {
		t.Errorf("symbols result = %q, want it to mention Hello", text)
	}
}

func TestMCPToolServer_RepoMap(t *testing.T) {
	root := t.TempDir()
	writeMinimalGoModule(t, root)
	c := newToolsOnlyCore(t, root)

	session := connectMCP(t, c.MCPToolServer())
	res, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "repo_map",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned an error result: %+v", res.Content)
	}
	text := res.Content[0].(*mcpsdk.TextContent).Text
	if !strings.Contains(text, "fixture.go") {
		t.Errorf("repo_map result = %q, want it to mention fixture.go", text)
	}
}

// TestMCPToolServer_SemanticSearch_NoEmbedModelConfigured proves the tool is
// wired end-to-end without needing a real embedding backend: SemanticSearch
// already returns a deterministic error when embed.model isn't configured
// (internal/tools/nav/semantic_search.go), which must surface as an MCP
// tool-level error (IsError: true), not a panic or a protocol-level error.
func TestMCPToolServer_SemanticSearch_NoEmbedModelConfigured(t *testing.T) {
	root := t.TempDir()
	writeMinimalGoModule(t, root)
	c := newToolsOnlyCore(t, root)

	session := connectMCP(t, c.MCPToolServer())
	res, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "semantic_search",
		Arguments: map[string]any{"query": "greeting"},
	})
	if err != nil {
		t.Fatalf("CallTool transport/protocol error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected a tool-level error result when embed.model isn't configured")
	}
	text := res.Content[0].(*mcpsdk.TextContent).Text
	if !strings.Contains(text, "embed.model not configured") {
		t.Errorf("error text = %q, want it to mention embed.model not configured", text)
	}
}

func TestMCPToolServer_RuntimeQuery(t *testing.T) {
	root := t.TempDir()
	if err := config.Save(filepath.Join(root, ".orchestra.yml"), config.DefaultConfig(root)); err != nil {
		t.Fatal(err)
	}

	// First pass: let core.New create .orchestra/ckg.db, then close it so
	// the file isn't held open while we seed it directly below.
	c0, err := New(root, Options{ToolsOnly: true})
	if err != nil {
		t.Fatalf("New (seed pass): %v", err)
	}
	if err := c0.Close(); err != nil {
		t.Fatalf("Close (seed pass): %v", err)
	}

	dbPath := filepath.Join(root, ".orchestra", "ckg.db")
	store, err := ckg.NewStore(dbPath)
	if err != nil {
		t.Fatalf("ckg.NewStore: %v", err)
	}
	ctx := context.Background()
	nodes := []ckg.Node{{FQN: "pkg.Handler", ShortName: "Handler", Kind: "func", LineStart: 5, LineEnd: 15}}
	if err := store.SaveFileNodes(ctx, "handler.go", "h1", "go", "ex", "pkg", nodes, nil); err != nil {
		t.Fatalf("SaveFileNodes: %v", err)
	}
	const traceID = "aabb00112233445566778899aabb0011"
	if err := store.IngestTrace(ctx, ckg.TraceData{
		TraceID:   traceID,
		Service:   "mysvc",
		StartedAt: time.Now(),
		Spans:     []ckg.SpanData{{SpanID: "s001", Name: "handle", CodeFile: "handler.go", CodeLineno: 10}},
	}); err != nil {
		t.Fatalf("IngestTrace: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("store.Close: %v", err)
	}

	c, err := New(root, Options{ToolsOnly: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	session := connectMCP(t, c.MCPToolServer())
	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "runtime_query",
		Arguments: map[string]any{"trace_id": traceID},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned an error result: %+v", res.Content)
	}
	text := res.Content[0].(*mcpsdk.TextContent).Text
	if !strings.Contains(text, "mysvc") || !strings.Contains(text, "pkg.Handler") {
		t.Errorf("runtime_query result = %q, want it to mention mysvc and pkg.Handler", text)
	}
}
