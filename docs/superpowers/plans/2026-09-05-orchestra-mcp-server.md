# Orchestra as an MCP Server Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expose Orchestra's ten read-only code-intelligence tools (CKG explore, semantic search, symbols, repo map, OTel-trace-to-code resolution, LSP) as a real MCP server, reachable over stdio and Streamable HTTP, so any MCP-capable client (Claude Code, Claude Desktop, Cursor, …) can use Orchestra's code understanding without going through Orchestra's own agent loop.

**Architecture:** `internal/core` gains a `ToolsOnly` startup mode that skips LLM-client construction, the network model-limit-resolution call, and Orchestra's own MCP-client-manager startup — none of which the tool server needs. A new `Core.MCPToolServer()` builds an `mcpsdk.Server` and registers the ten tools directly against the existing `Runner` methods, reusing the exact `llm.ToolDef` schemas the agent already ships. A new `orchestra mcp serve` subcommand serves that server over stdio (default) or Streamable HTTP (`--http`, always token-gated).

**Tech Stack:** Go, `github.com/modelcontextprotocol/go-sdk/mcp` v1.7.0 (already a dependency, used today only by test fixtures and the MCP *client* transport), Cobra (existing CLI framework), SQLite via the existing `internal/ckg` package (no new dependency).

**Spec:** `docs/superpowers/specs/2026-09-05-orchestra-mcp-server-design.md`

## Global Constraints

- Strict TDD per task: write the failing test, run it and confirm it fails for the *right* reason (missing symbol/wrong behavior, not a typo), write the minimal implementation, confirm green, then commit.
- One thematic commit per task (see each task's commit step) — never bundle unrelated tasks into one commit.
- After every task: `go build ./...`, `go vet ./...`, then the touched package's tests with `-race`. After the final task: the full suite across all four Go modules (root, `llm/`, `patch/`, `protocol/`) via `go test ./...` in each module directory (see Task 7).
- Ten tools only, all read-only: `explore`, `semantic_search`, `symbols`, `repo_map`, `runtime_query`, `lsp.definition`, `lsp.references`, `lsp.hover`, `lsp.diagnostics`, `lsp.rename`. No `write`/`edit`/`bash`/`exec`/`git.*`/`web.*`/memory/task tools, and no CKG-index-mutating tools (`RunCKGEmbed`/`RebuildCKG`) — see the spec's Non-goals section.
- One workspace root per `orchestra mcp serve` process, for both transports — no per-request workspace switching.
- HTTP mode (`--http`) always requires a bearer token (`--mcp-token` or `$ORCH_MCP_TOKEN`); refusing to start without one is required behavior, not a nice-to-have. Default bind stays loopback (`127.0.0.1:0`); a non-loopback `--http-addr` is the operator's own explicit choice, nothing else gates it.
- Every tool's MCP `Description`/`InputSchema` must come from the existing `llm.ToolDef` the agent already uses (`nav.ToolExploreCodebase()` etc.) — never hand-duplicated JSON. Task 5 adds a test enforcing this stays true.
- This feature must not import or reintroduce `internal/daemon` (CI-banned by `tests/importrules.TestNoLegacyInternalSubmodules`) and must not require a reachable LLM endpoint to start.

---

### Task 1: `core.Options.ToolsOnly`

**Files:**
- Modify: `internal/core/core.go:63-67` (add field to `Options`), `internal/core/core.go:117-154` (guard the LLM-client/`ResolveModelLimits` block and the MCP-manager-start block)
- Test: Create `internal/core/core_options_test.go`

**Interfaces:**
- Consumes: nothing new — `config.DefaultConfig`, `config.Save`, `config.MCPServerConfig` (existing).
- Produces: `core.Options.ToolsOnly bool`, consumed by Task 2's `Core.MCPToolServer()` callers and every later task's tests, which construct `Core` via `New(root, Options{ToolsOnly: true})`.

- [ ] **Step 1: Write the failing test**

Create `internal/core/core_options_test.go`:

```go
package core

import (
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
)

// TestNew_ToolsOnly_SkipsLLMClientAndMCPManager verifies the fast, LLM-free
// startup path a tool-only MCP server needs: no LLM client construction (and
// therefore no network call to resolve model limits), no configured MCP
// client servers started, but the tools.Runner is still built normally.
func TestNew_ToolsOnly_SkipsLLMClientAndMCPManager(t *testing.T) {
	root := t.TempDir()
	cfgPath := filepath.Join(root, ".orchestra.yml")

	cfg := config.DefaultConfig(root)
	// Deliberately unreachable: if ToolsOnly didn't skip LLM client
	// construction, ResolveModelLimits' network dial would make this test
	// slow (and flaky under -race) instead of just wrong.
	cfg.LLM.APIBase = "http://127.0.0.1:1/v1"
	cfg.LLM.APIKey = "k"
	cfg.LLM.Model = "unused-model"
	cfg.MCP.Servers = []config.MCPServerConfig{{
		Name:    "should-not-start",
		Command: []string{"does-not-exist-on-this-machine"},
	}}
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}

	c, err := New(root, Options{ToolsOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	if c.llmClient != nil {
		t.Error("ToolsOnly must not construct an LLM client")
	}
	if c.mcpManager != nil {
		t.Error("ToolsOnly must not start configured MCP client servers")
	}
	if c.tools == nil {
		t.Error("ToolsOnly must still construct the tools.Runner")
	}
}

// TestNew_WithoutToolsOnly_StillConstructsLLMClient guards against a future
// edit accidentally making ToolsOnly the default behavior.
func TestNew_WithoutToolsOnly_StillConstructsLLMClient(t *testing.T) {
	root := t.TempDir()
	cfgPath := filepath.Join(root, ".orchestra.yml")
	cfg := config.DefaultConfig(root)
	cfg.LLM.APIBase = "http://127.0.0.1:1/v1"
	cfg.LLM.APIKey = "k"
	cfg.LLM.Model = "unused-model"
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}

	c, err := New(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	if c.llmClient == nil {
		t.Error("without ToolsOnly, New must still construct an LLM client as before")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/ -run TestNew_ToolsOnly -v`
Expected: compile error `unknown field ToolsOnly in struct literal of type Options` — confirms the test is exercising a field that doesn't exist yet, not a typo elsewhere.

- [ ] **Step 3: Write minimal implementation**

In `internal/core/core.go`, change the `Options` struct (currently at lines 63-67):

```go
type Options struct {
	Debug bool
	// LLMClient overrides the default OpenAI client (used in tests).
	LLMClient llm.Client
	// ToolsOnly skips LLM client construction, the network call to resolve
	// model context-window limits, and starting Orchestra's own configured
	// MCP client servers. Set by `orchestra mcp serve`: an MCP tool server
	// needs none of these, and requiring them would mean it can't start at
	// all without a working, reachable LLM endpoint configured.
	ToolsOnly bool
}
```

Then wrap the two blocks identified in the design's research (LLM client + `ResolveModelLimits`, and MCP manager start) in `if !opts.ToolsOnly`. The existing code around lines 117-155 reads:

```go
injected := opts.LLMClient != nil
llmClient := opts.LLMClient
if llmClient == nil {
	discCtx, discCancel := context.WithTimeout(context.Background(), 8*time.Second)
	llm.ResolveModelLimits(discCtx, &cfg.LLM)
	discCancel()
	logger := llm.NewLogger(rootAbs)
	llmClient = llm.NewClient(cfg.LLM)
	if oc, ok := llm.AsOpenAIClient(llmClient); ok {
		oc.SetLogger(logger)
	}
	llmClient = llm.MaybeWrapFallback(llmClient, cfg.LLMRegistry(), cfg.LLM, logger)
}

var mcpMgr *mcp.Manager
mcpErrs := map[string]string{}
if len(cfg.MCP.Servers) > 0 {
	var startErrs []error
	mcpMgr, startErrs = mcp.NewManager(context.Background(), cfg.MCP)
	for _, err := range startErrs {
		fmt.Fprintf(os.Stderr, "orchestra: mcp startup warning: %v\n", err)
		msg := err.Error()
		const prefix = `mcp server "`
		if strings.HasPrefix(msg, prefix) {
			rest := msg[len(prefix):]
			if i := strings.Index(rest, `"`); i > 0 {
				mcpErrs[rest[:i]] = msg
			}
		}
	}
	if !mcpMgr.IsEmpty() {
		tr.SetMCPCaller(mcpMgr)
	}
}
tr.SetMemoryContext("", cfg.Memory.Resolve())
```

Replace it with:

```go
injected := opts.LLMClient != nil
llmClient := opts.LLMClient
if llmClient == nil && !opts.ToolsOnly {
	discCtx, discCancel := context.WithTimeout(context.Background(), 8*time.Second)
	llm.ResolveModelLimits(discCtx, &cfg.LLM)
	discCancel()
	logger := llm.NewLogger(rootAbs)
	llmClient = llm.NewClient(cfg.LLM)
	if oc, ok := llm.AsOpenAIClient(llmClient); ok {
		oc.SetLogger(logger)
	}
	llmClient = llm.MaybeWrapFallback(llmClient, cfg.LLMRegistry(), cfg.LLM, logger)
}

var mcpMgr *mcp.Manager
mcpErrs := map[string]string{}
if !opts.ToolsOnly && len(cfg.MCP.Servers) > 0 {
	var startErrs []error
	mcpMgr, startErrs = mcp.NewManager(context.Background(), cfg.MCP)
	for _, err := range startErrs {
		fmt.Fprintf(os.Stderr, "orchestra: mcp startup warning: %v\n", err)
		msg := err.Error()
		const prefix = `mcp server "`
		if strings.HasPrefix(msg, prefix) {
			rest := msg[len(prefix):]
			if i := strings.Index(rest, `"`); i > 0 {
				mcpErrs[rest[:i]] = msg
			}
		}
	}
	if !mcpMgr.IsEmpty() {
		tr.SetMCPCaller(mcpMgr)
	}
}
tr.SetMemoryContext("", cfg.Memory.Resolve())
```

(Only two conditions changed: `if llmClient == nil && !opts.ToolsOnly` and `if !opts.ToolsOnly && len(cfg.MCP.Servers) > 0`. Everything else — including `tr.SetMemoryContext`, which the ten MCP tools don't need but which is harmless and cheap to leave unconditional — is untouched.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/core/ -run TestNew_ToolsOnly -v` and `go test ./internal/core/ -run TestNew_WithoutToolsOnly -v`
Expected: both PASS.

- [ ] **Step 5: Run the full package test suite + race, build, vet**

Run:
```
go build ./...
go vet ./...
go test ./internal/core/... -race
```
Expected: all pass, no new failures.

- [ ] **Step 6: Commit**

```bash
git add internal/core/core.go internal/core/core_options_test.go
git commit -m "$(cat <<'EOF'
feat(core): Options.ToolsOnly skips LLM client + MCP client manager startup

A tool-only MCP server (orchestra mcp serve, next task) needs core.New's
tools.Runner but none of its LLM/session machinery. Without this flag,
starting the server would require a working, reachable LLM endpoint
configured just to answer a code-intelligence tool call.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: `Core.MCPToolServer()` — skeleton + the four `nav` tools

**Files:**
- Create: `internal/core/core_mcp_server.go`
- Test: Create `internal/core/core_mcp_server_test.go`

**Interfaces:**
- Consumes: `Core.tools *tools.Runner` (existing field), `nav.ToolExploreCodebase()`, `nav.ToolSemanticSearch()`, `nav.ToolCodeSymbols()`, `nav.ToolRepoMap()` (existing, `internal/tools/nav/registry.go`), `Runner.ExploreCodebase`, `Runner.SemanticSearch`, `Runner.CodeSymbols`, `Runner.RepoMap` (existing, `internal/tools/nav_delegate.go`), `Options.ToolsOnly` (Task 1).
- Produces: `func (c *Core) MCPToolServer() *mcpsdk.Server` — the entry point every later task (3, 4, 6, 7) builds on. `mcpTextResult(v any) (*mcpsdk.CallToolResult, error)` — shared response-marshaling helper reused by every tool in Tasks 2-4.

- [ ] **Step 1: Write the failing test**

Create `internal/core/core_mcp_server_test.go`:

```go
package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/ -run TestMCPToolServer -v`
Expected: compile error `c.MCPToolServer undefined (type *Core has no field or method MCPToolServer)`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/core/core_mcp_server.go`:

```go
package core

import (
	"context"
	"encoding/json"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/orchestra/orchestra/internal/tools/nav"
)

// MCPToolServer builds an MCP server exposing Orchestra's read-only
// code-intelligence tools (CKG explore, semantic search, symbols, repo map,
// runtime trace resolution, LSP) over c.tools, with no LLM/session/agent-loop
// involvement. Every tool's description and input schema is taken directly
// from the same llm.ToolDef Orchestra's own agent already uses, so an
// external MCP caller sees identical semantics to Orchestra's own agent.
func (c *Core) MCPToolServer() *mcpsdk.Server {
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "orchestra", Version: "1"}, nil)

	exploreDef := nav.ToolExploreCodebase().Function
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        exploreDef.Name,
		Description: exploreDef.Description,
		InputSchema: exploreDef.Parameters,
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in nav.ExploreCodebaseRequest) (*mcpsdk.CallToolResult, any, error) {
		resp, err := c.tools.ExploreCodebase(ctx, in)
		if err != nil {
			return nil, nil, err
		}
		res, err := mcpTextResult(resp)
		return res, nil, err
	})

	semanticDef := nav.ToolSemanticSearch().Function
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        semanticDef.Name,
		Description: semanticDef.Description,
		InputSchema: semanticDef.Parameters,
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in nav.SemanticSearchRequest) (*mcpsdk.CallToolResult, any, error) {
		resp, err := c.tools.SemanticSearch(ctx, in)
		if err != nil {
			return nil, nil, err
		}
		res, err := mcpTextResult(resp)
		return res, nil, err
	})

	symbolsDef := nav.ToolCodeSymbols().Function
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        symbolsDef.Name,
		Description: symbolsDef.Description,
		InputSchema: symbolsDef.Parameters,
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in nav.CodeSymbolsRequest) (*mcpsdk.CallToolResult, any, error) {
		resp, err := c.tools.CodeSymbols(ctx, in)
		if err != nil {
			return nil, nil, err
		}
		res, err := mcpTextResult(resp)
		return res, nil, err
	})

	repoMapDef := nav.ToolRepoMap().Function
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        repoMapDef.Name,
		Description: repoMapDef.Description,
		InputSchema: repoMapDef.Parameters,
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in nav.RepoMapRequest) (*mcpsdk.CallToolResult, any, error) {
		resp, err := c.tools.RepoMap(ctx, in)
		if err != nil {
			return nil, nil, err
		}
		res, err := mcpTextResult(resp)
		return res, nil, err
	})

	return srv
}

// mcpTextResult marshals a tool's response struct to JSON and wraps it as
// MCP text content — the same JSON shape these tools already produce for
// Orchestra's own agent loop.
func mcpTextResult(v any) (*mcpsdk.CallToolResult, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: string(b)}}}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/core/ -run TestMCPToolServer -v`
Expected: all four PASS (`TestMCPToolServer_Explore`, `_Symbols`, `_RepoMap`, `_SemanticSearch_NoEmbedModelConfigured`).

- [ ] **Step 5: Run the full package test suite + race, build, vet**

Run:
```
go build ./...
go vet ./...
go test ./internal/core/... -race
```
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/core/core_mcp_server.go internal/core/core_mcp_server_test.go
git commit -m "$(cat <<'EOF'
feat(core): MCPToolServer exposes explore/semantic_search/symbols/repo_map

First four of ten tools. Each tool's MCP description and input schema come
directly from the same llm.ToolDef Orchestra's own agent already sends to
LLM providers - one source of truth, no hand-duplicated JSON schema.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: `runtime_query` tool

**Files:**
- Modify: `internal/core/core_mcp_server.go` (add one `AddTool` registration)
- Modify: `internal/core/core_mcp_server_test.go` (add one test)

**Interfaces:**
- Consumes: `session.ToolRuntimeQuery()`, `session.RuntimeQueryRequest` (`internal/tools/session/registry.go`, `runtime.go`), `Runner.RuntimeQuery` (`internal/tools/session_delegate.go`), `ckg.NewStore`, `ckg.Node`, `ckg.TraceData`, `ckg.SpanData`, `store.SaveFileNodes`, `store.IngestTrace` (existing, `internal/ckg`), `mcpTextResult` (Task 2).
- Produces: nothing new consumed by later tasks — `runtime_query` is now registered alongside the four from Task 2.

- [ ] **Step 1: Write the failing test**

Add to `internal/core/core_mcp_server_test.go`:

```go
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
```

Add `"time"` and `"github.com/orchestra/orchestra/internal/ckg"` to the file's import block.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/ -run TestMCPToolServer_RuntimeQuery -v`
Expected: FAIL — `res.IsError` is true because no tool named `runtime_query` is registered, so `CallTool` returns a protocol-level "unknown tool" error (fails at the `err != nil` check with a message like `unknown tool "runtime_query"`).

- [ ] **Step 3: Write minimal implementation**

In `internal/core/core_mcp_server.go`, add `"github.com/orchestra/orchestra/internal/tools/session"` to the imports, then add this registration inside `MCPToolServer` before `return srv`:

```go
	runtimeQueryDef := session.ToolRuntimeQuery().Function
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        runtimeQueryDef.Name,
		Description: runtimeQueryDef.Description,
		InputSchema: runtimeQueryDef.Parameters,
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in session.RuntimeQueryRequest) (*mcpsdk.CallToolResult, any, error) {
		resp, err := c.tools.RuntimeQuery(ctx, in)
		if err != nil {
			return nil, nil, err
		}
		res, err := mcpTextResult(resp)
		return res, nil, err
	})
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/core/ -run TestMCPToolServer -v`
Expected: all five tests PASS.

- [ ] **Step 5: Run the full package test suite + race, build, vet**

Run:
```
go build ./...
go vet ./...
go test ./internal/core/... -race
```
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/core/core_mcp_server.go internal/core/core_mcp_server_test.go
git commit -m "$(cat <<'EOF'
feat(core): MCPToolServer exposes runtime_query

Resolves an OpenTelemetry trace_id onto CKG nodes (code_file, code_lineno,
node_fqn) for an external MCP caller diagnosing a bug from a trace, the
same way Orchestra's own agent already can.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 4: The five `lsp.*` tools

This task is also where the design spec's risk item — do dotted tool names (`lsp.definition`, etc.) work correctly through the MCP SDK's registration and call-dispatch machinery — gets a real, automated answer, five tools in.

**Files:**
- Modify: `internal/core/core_mcp_server.go` (add five `AddTool` registrations)
- Modify: `internal/core/core_mcp_server_test.go` (add five tests)

**Interfaces:**
- Consumes: `toolslsp.ToolLSPDefinition()`, `ToolLSPReferences()`, `ToolLSPHover()`, `ToolLSPDiagnostics()`, `ToolLSPRename()` and their `*Request` types (`internal/tools/toolslsp/registry.go`, `lsp_tools.go`), `Runner.LSPDefinition`, `LSPReferences`, `LSPHover`, `LSPDiagnostics`, `LSPRename` (`internal/tools/web_delegate.go`), `mcpTextResult` (Task 2).
- Produces: nothing new consumed by later tasks — all ten tools are now registered.

- [ ] **Step 1: Write the failing test**

Add to `internal/core/core_mcp_server_test.go`. This workspace has no LSP servers configured (`config.DefaultConfig` leaves `LSP.Servers` empty), so every call deterministically hits the same "no servers configured" guard `internal/tools/toolslsp/lsp_tools.go` already returns — no `gopls` or any other external LSP binary needed for this required test:

```go
func TestMCPToolServer_LSPToolsReportNoServersConfigured(t *testing.T) {
	root := t.TempDir()
	writeMinimalGoModule(t, root)
	c := newToolsOnlyCore(t, root)
	session := connectMCP(t, c.MCPToolServer())
	ctx := context.Background()

	cases := []struct {
		name string
		args map[string]any
	}{
		{"lsp.definition", map[string]any{"path": "fixture.go", "line": 4, "col": 6}},
		{"lsp.references", map[string]any{"path": "fixture.go", "line": 4, "col": 6}},
		{"lsp.hover", map[string]any{"path": "fixture.go", "line": 4, "col": 6}},
		{"lsp.diagnostics", map[string]any{"path": "fixture.go"}},
		{"lsp.rename", map[string]any{"path": "fixture.go", "line": 4, "col": 6, "new_name": "Greet"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{Name: tc.name, Arguments: tc.args})
			if err != nil {
				t.Fatalf("CallTool(%s): %v", tc.name, err)
			}
			if !res.IsError {
				t.Fatalf("%s: expected a tool-level error result with no LSP servers configured", tc.name)
			}
			text := res.Content[0].(*mcpsdk.TextContent).Text
			if !strings.Contains(text, "no servers configured") {
				t.Errorf("%s: error text = %q, want it to mention 'no servers configured'", tc.name, text)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/ -run TestMCPToolServer_LSPToolsReportNoServersConfigured -v`
Expected: every subtest FAILs with a protocol-level "unknown tool" error (none of the five tools are registered yet).

- [ ] **Step 3: Write minimal implementation**

In `internal/core/core_mcp_server.go`, add `"github.com/orchestra/orchestra/internal/tools/toolslsp"` to the imports, then add these five registrations before `return srv`:

```go
	lspDefDef := toolslsp.ToolLSPDefinition().Function
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        lspDefDef.Name,
		Description: lspDefDef.Description,
		InputSchema: lspDefDef.Parameters,
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in toolslsp.LSPDefinitionRequest) (*mcpsdk.CallToolResult, any, error) {
		resp, err := c.tools.LSPDefinition(ctx, in)
		if err != nil {
			return nil, nil, err
		}
		res, err := mcpTextResult(resp)
		return res, nil, err
	})

	lspRefDef := toolslsp.ToolLSPReferences().Function
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        lspRefDef.Name,
		Description: lspRefDef.Description,
		InputSchema: lspRefDef.Parameters,
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in toolslsp.LSPReferencesRequest) (*mcpsdk.CallToolResult, any, error) {
		resp, err := c.tools.LSPReferences(ctx, in)
		if err != nil {
			return nil, nil, err
		}
		res, err := mcpTextResult(resp)
		return res, nil, err
	})

	lspHoverDef := toolslsp.ToolLSPHover().Function
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        lspHoverDef.Name,
		Description: lspHoverDef.Description,
		InputSchema: lspHoverDef.Parameters,
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in toolslsp.LSPHoverRequest) (*mcpsdk.CallToolResult, any, error) {
		resp, err := c.tools.LSPHover(ctx, in)
		if err != nil {
			return nil, nil, err
		}
		res, err := mcpTextResult(resp)
		return res, nil, err
	})

	lspDiagDef := toolslsp.ToolLSPDiagnostics().Function
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        lspDiagDef.Name,
		Description: lspDiagDef.Description,
		InputSchema: lspDiagDef.Parameters,
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in toolslsp.LSPDiagnosticsRequest) (*mcpsdk.CallToolResult, any, error) {
		resp, err := c.tools.LSPDiagnostics(ctx, in)
		if err != nil {
			return nil, nil, err
		}
		res, err := mcpTextResult(resp)
		return res, nil, err
	})

	lspRenameDef := toolslsp.ToolLSPRename().Function
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        lspRenameDef.Name,
		Description: lspRenameDef.Description,
		InputSchema: lspRenameDef.Parameters,
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in toolslsp.LSPRenameRequest) (*mcpsdk.CallToolResult, any, error) {
		resp, err := c.tools.LSPRename(ctx, in)
		if err != nil {
			return nil, nil, err
		}
		res, err := mcpTextResult(resp)
		return res, nil, err
	})
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/core/ -run TestMCPToolServer -v`
Expected: all tests PASS, including all five `TestMCPToolServer_LSPToolsReportNoServersConfigured` subtests — this is the automated confirmation that dotted MCP tool names (`lsp.definition`, etc.) register and round-trip correctly through `mcpsdk.AddTool`/`CallTool`.

- [ ] **Step 5: Run the full package test suite + race, build, vet**

Run:
```
go build ./...
go vet ./...
go test ./internal/core/... -race
```
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/core/core_mcp_server.go internal/core/core_mcp_server_test.go
git commit -m "$(cat <<'EOF'
feat(core): MCPToolServer exposes lsp.definition/references/hover/diagnostics/rename

All ten tools from the design spec are now registered. Confirms the dotted
tool names (lsp.*) round-trip cleanly through mcpsdk's AddTool/CallTool -
the design's risk item 1, verified here rather than assumed.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 5: Schema-parity test

**Files:**
- Test: Create `internal/core/core_mcp_server_parity_test.go`

**Interfaces:**
- Consumes: `Core.MCPToolServer()` (Task 2-4), `connectMCP` (Task 2), the ten `nav.Tool*()`/`session.ToolRuntimeQuery()`/`toolslsp.Tool*()` functions used to build the server.
- Produces: nothing consumed by later tasks — this is a standing regression test enforcing the "one source of truth" claim from the design spec for as long as the feature exists.

- [ ] **Step 1: Write the failing test**

Create `internal/core/core_mcp_server_parity_test.go`:

```go
package core

import (
	"context"
	"encoding/json"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/orchestra/orchestra/internal/tools/nav"
	"github.com/orchestra/orchestra/internal/tools/session"
	"github.com/orchestra/orchestra/internal/tools/toolslsp"
	"github.com/orchestra/orchestra/llm"
)

// TestMCPToolServer_SchemaMatchesAgentToolDefs enforces the design's "one
// source of truth" claim: an external MCP caller must see exactly the same
// name, description and input schema Orchestra's own agent already sends to
// LLM providers for each of the ten exposed tools - never a hand-duplicated
// copy that can drift.
func TestMCPToolServer_SchemaMatchesAgentToolDefs(t *testing.T) {
	root := t.TempDir()
	writeMinimalGoModule(t, root)
	c := newToolsOnlyCore(t, root)
	session_ := connectMCP(t, c.MCPToolServer())

	want := []llm.ToolDef{
		nav.ToolExploreCodebase(),
		nav.ToolSemanticSearch(),
		nav.ToolCodeSymbols(),
		nav.ToolRepoMap(),
		session.ToolRuntimeQuery(),
		toolslsp.ToolLSPDefinition(),
		toolslsp.ToolLSPReferences(),
		toolslsp.ToolLSPHover(),
		toolslsp.ToolLSPDiagnostics(),
		toolslsp.ToolLSPRename(),
	}

	listed, err := session_.ListTools(context.Background(), &mcpsdk.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	byName := map[string]*mcpsdk.Tool{}
	for _, tool := range listed.Tools {
		byName[tool.Name] = tool
	}

	if len(listed.Tools) != len(want) {
		t.Fatalf("server exposes %d tools, want exactly %d", len(listed.Tools), len(want))
	}

	for _, def := range want {
		mt, ok := byName[def.Function.Name]
		if !ok {
			t.Errorf("tool %q from the agent's own ToolDef table is not exposed over MCP", def.Function.Name)
			continue
		}
		if mt.Description != def.Function.Description {
			t.Errorf("%s: MCP description does not match the agent's ToolDef description", def.Function.Name)
		}

		var wantSchema, gotSchema any
		if err := json.Unmarshal(def.Function.Parameters, &wantSchema); err != nil {
			t.Fatalf("%s: unmarshal agent schema: %v", def.Function.Name, err)
		}
		gotBytes, err := json.Marshal(mt.InputSchema)
		if err != nil {
			t.Fatalf("%s: marshal MCP schema: %v", def.Function.Name, err)
		}
		if err := json.Unmarshal(gotBytes, &gotSchema); err != nil {
			t.Fatalf("%s: unmarshal MCP schema: %v", def.Function.Name, err)
		}
		wantJSON, _ := json.Marshal(wantSchema)
		gotJSON, _ := json.Marshal(gotSchema)
		if string(wantJSON) != string(gotJSON) {
			t.Errorf("%s: MCP input schema does not match the agent's ToolDef schema\n  agent: %s\n  mcp:   %s", def.Function.Name, wantJSON, gotJSON)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/ -run TestMCPToolServer_SchemaMatchesAgentToolDefs -v`
Expected: PASSes immediately if Tasks 2-4 were done correctly — this is a verification test for already-implemented behavior, not new production code. If it fails, the failure message pinpoints exactly which tool's `Description`/`InputSchema` was copied incorrectly in an earlier task; fix the registration in `core_mcp_server.go`, not the test.

- [ ] **Step 3: N/A — this task adds no production code**

If Step 2 passed, skip directly to Step 4. If it failed, go back to the specific `AddTool` call named in the failure and correct its `Description`/`InputSchema` source, then re-run.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/core/ -run TestMCPToolServer_SchemaMatchesAgentToolDefs -v`
Expected: PASS.

- [ ] **Step 5: Run the full package test suite + race, build, vet**

Run:
```
go build ./...
go vet ./...
go test ./internal/core/... -race
```
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/core/core_mcp_server_parity_test.go
git commit -m "$(cat <<'EOF'
test(core): enforce MCP tool schemas match the agent's own ToolDef table

Standing regression guard for the design's "one source of truth" claim -
an external MCP caller must never see a hand-duplicated, driftable copy of
a tool's name/description/schema.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 6: `orchestra mcp serve` — stdio default, `--http` with mandatory token

**Files:**
- Create: `internal/cli/mcp_serve.go`
- Test: Create `internal/cli/mcp_serve_test.go`

**Interfaces:**
- Consumes: `core.New`, `core.Options{ToolsOnly: true}` (Task 1), `Core.MCPToolServer()` (Task 2-4), `mcpCmd` (existing, `internal/cli/mcp.go`), `mcpsdk.StdioTransport{}`, `mcpsdk.NewStreamableHTTPHandler` (existing SDK API, already used by `internal/mcp/remote_test.go`).
- Produces: `mcpServeCmd` (registered under `mcpCmd`), `runMCPServe(cmd *cobra.Command, args []string) error` — nothing later in this plan depends on it, but it's the feature's whole user-facing entry point.

- [ ] **Step 1: Write the failing test**

Create `internal/cli/mcp_serve_test.go`:

```go
package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMCPServeCmd_RegisteredUnderMCPCommand(t *testing.T) {
	found := false
	for _, sub := range mcpCmd.Commands() {
		if sub.Name() == "serve" {
			found = true
		}
	}
	if !found {
		t.Fatal(`"orchestra mcp serve" is not registered under the "mcp" command`)
	}
}

func TestMCPServeCmd_Flags(t *testing.T) {
	for _, name := range []string{"workspace-root", "http", "http-addr", "mcp-token"} {
		if mcpServeCmd.Flags().Lookup(name) == nil {
			t.Errorf("missing --%s flag", name)
		}
	}
}

func TestMCPServeCmd_HTTPAddrDefaultsToLoopback(t *testing.T) {
	flag := mcpServeCmd.Flags().Lookup("http-addr")
	if flag == nil {
		t.Fatal("--http-addr flag not registered")
	}
	if !strings.HasPrefix(flag.DefValue, "127.0.0.1:") {
		t.Errorf("--http-addr default = %q, want a 127.0.0.1 default", flag.DefValue)
	}
}

func TestResolveMCPToken_MissingReturnsError(t *testing.T) {
	t.Setenv("ORCH_MCP_TOKEN", "")
	if _, err := resolveMCPToken(""); err == nil {
		t.Fatal("expected an error when no token is provided by flag or env")
	}
}

func TestResolveMCPToken_FallsBackToEnv(t *testing.T) {
	t.Setenv("ORCH_MCP_TOKEN", "from-env")
	got, err := resolveMCPToken("")
	if err != nil {
		t.Fatal(err)
	}
	if got != "from-env" {
		t.Errorf("token = %q, want from-env", got)
	}
}

func TestResolveMCPToken_FlagWinsOverEnv(t *testing.T) {
	t.Setenv("ORCH_MCP_TOKEN", "from-env")
	got, err := resolveMCPToken("from-flag")
	if err != nil {
		t.Fatal(err)
	}
	if got != "from-flag" {
		t.Errorf("token = %q, want from-flag", got)
	}
}

func TestRequireBearerToken_RejectsWrongOrMissingToken(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	handler := requireBearerToken("secret", next)

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no Authorization header: status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	req.Header.Set("Authorization", "Bearer wrong")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong token: status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("correct token: status = %d, want %d", rec.Code, http.StatusOK)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run 'TestMCPServeCmd|TestResolveMCPToken|TestRequireBearerToken' -v`
Expected: compile errors `undefined: mcpServeCmd`, `undefined: resolveMCPToken`, `undefined: requireBearerToken` — none of `internal/cli/mcp_serve.go`'s symbols exist yet.

- [ ] **Step 3: Write minimal implementation**

Create `internal/cli/mcp_serve.go`:

```go
package cli

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/orchestra/orchestra/internal/core"
)

var (
	mcpServeWorkspaceRoot string
	mcpServeHTTP          bool
	mcpServeHTTPAddr      string
	mcpServeToken         string
)

var mcpServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Serve Orchestra's code-intelligence tools over MCP",
	Long: `Exposes explore, semantic_search, symbols, repo_map, runtime_query and the
lsp.* tools as MCP tools, so any MCP-capable client (Claude Code, Claude
Desktop, Cursor, ...) can use Orchestra's code understanding directly,
without going through Orchestra's own agent loop.

Binds to one workspace root for the whole process lifetime. stdio is the
default transport, matching how MCP hosts expect a locally configured
server to behave; --http switches to Streamable HTTP and always requires
a token (--mcp-token or $ORCH_MCP_TOKEN).`,
	Args: cobra.NoArgs,
	RunE: runMCPServe,
}

func init() {
	mcpServeCmd.Flags().StringVar(&mcpServeWorkspaceRoot, "workspace-root", "", "Workspace root (default: current directory)")
	mcpServeCmd.Flags().BoolVar(&mcpServeHTTP, "http", false, "Serve over Streamable HTTP instead of stdio")
	mcpServeCmd.Flags().StringVar(&mcpServeHTTPAddr, "http-addr", "127.0.0.1:0", "HTTP bind address; change only to expose the server beyond localhost")
	mcpServeCmd.Flags().StringVar(&mcpServeToken, "mcp-token", "", "Bearer token required on every HTTP request (or set ORCH_MCP_TOKEN); mandatory with --http")
	mcpCmd.AddCommand(mcpServeCmd)
}

func runMCPServe(cmd *cobra.Command, _ []string) error {
	workspace := mcpServeWorkspaceRoot
	if workspace == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get cwd: %w", err)
		}
		workspace = cwd
	}
	workspace, _ = filepath.Abs(workspace)

	c, err := core.New(workspace, core.Options{ToolsOnly: true})
	if err != nil {
		return fmt.Errorf("start core: %w", err)
	}
	defer func() { _ = c.Close() }()

	srv := c.MCPToolServer()

	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	if !mcpServeHTTP {
		return srv.Run(ctx, &mcpsdk.StdioTransport{})
	}

	token, err := resolveMCPToken(mcpServeToken)
	if err != nil {
		return err
	}
	return serveMCPHTTP(ctx, srv, mcpServeHTTPAddr, token)
}

// resolveMCPToken picks the bearer token an --http server will require:
// --mcp-token if set, otherwise $ORCH_MCP_TOKEN, otherwise an error. HTTP
// mode never starts without one - unlike `orchestra core --http` (debug-only,
// auto-generates a token if omitted), this server's whole point is real
// remote/multi-client use, so silently generating a token nobody was told
// about is the wrong default here.
func resolveMCPToken(flagToken string) (string, error) {
	token := flagToken
	if token == "" {
		token = os.Getenv("ORCH_MCP_TOKEN")
	}
	if token == "" {
		return "", fmt.Errorf("mcp serve --http requires a token: pass --mcp-token or set ORCH_MCP_TOKEN")
	}
	return token, nil
}

// requireBearerToken wraps next with a constant-time bearer-token check.
func requireBearerToken(token string, next http.Handler) http.Handler {
	want := "Bearer " + token
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte(want)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// serveMCPHTTP serves srv over Streamable HTTP at addr, gated by token, until
// ctx is cancelled.
func serveMCPHTTP(ctx context.Context, srv *mcpsdk.Server, addr, token string) error {
	handler := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return srv }, nil)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	fmt.Fprintf(os.Stderr, "[orchestra] mcp serve: listening on http://%s\n", ln.Addr())

	httpSrv := &http.Server{Handler: requireBearerToken(token, handler)}
	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.Serve(ln) }()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return httpSrv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -run 'TestMCPServeCmd|TestResolveMCPToken|TestRequireBearerToken' -v`
Expected: all PASS.

- [ ] **Step 5: Run the full package test suite + race, build, vet**

Run:
```
go build ./...
go vet ./...
go test ./internal/cli/... -race
```
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/mcp_serve.go internal/cli/mcp_serve_test.go
git commit -m "$(cat <<'EOF'
feat(cli): orchestra mcp serve - stdio default, --http with mandatory token

New subcommand alongside mcp add/remove/get/list-tools. stdio is the
default transport, matching how MCP hosts expect a locally configured
server to behave. --http always requires --mcp-token or ORCH_MCP_TOKEN -
unlike orchestra core --http (debug-only, auto-generates a token), this
server's whole point is real remote/multi-client use. Default bind stays
loopback; a non-loopback --http-addr is the operator's own explicit choice.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 7: Full-suite verification + docs

**Files:**
- Modify: `docs/parity-plan-2026-09.md` (strike C2 in the Wave C table and in §1.5, add "Где в коде")
- Modify: `README.md`, `README.ru.md` (one short mention under the MCP config section)

**Interfaces:**
- Consumes: everything from Tasks 1-6.
- Produces: nothing — this is the closing task.

- [ ] **Step 1: Run the full build/vet/test/race suite across all four Go modules**

Run, from the repo root:
```
go work sync
go build ./...
go vet ./...
go test ./...
go test ./internal/core/... ./internal/cli/... -race
```
Then, from each sub-module directory:
```
cd llm && go build ./... && go vet ./... && go test ./...
cd ../patch && go build ./... && go vet ./... && go test ./...
cd ../protocol && go build ./... && go vet ./... && go test ./...
```
Also run the import-rules gate explicitly, since this feature adds a new `internal/cli` → `internal/core` → `mcpsdk` dependency chain the CI gate should already tolerate (per the design's own research: no existing rule forbids it), but must be re-confirmed now that the code exists:
```
go test ./tests/importrules/... -count=1
```
Expected: everything passes, with no new failures anywhere in the four modules.

- [ ] **Step 2: Update `docs/parity-plan-2026-09.md`**

In the §1.5 table, find the row for MCP-server (item #8) and the Wave C table's `C2` row (see `docs/parity-plan-2026-09.md` around lines 229-230 as of this plan's writing — line numbers may have shifted since; search for "Orchestra как MCP-сервер" and "C2"). Strike both through and add a "Где в коде" note, following the exact style already used for every other closed item in this document (e.g. the B7/B8 rows): wrap the task description in `~~...~~`, append "— закрыто", and add file:line references — `internal/core/core.go` (`Options.ToolsOnly`), `internal/core/core_mcp_server.go` (`MCPToolServer`), `internal/cli/mcp_serve.go` (`orchestra mcp serve`), naming all ten exposed tool names and noting the schema-parity test (`internal/core/core_mcp_server_parity_test.go`) that keeps them from drifting from the agent's own tool definitions.

- [ ] **Step 3: Add a short README mention**

In `README.md`, under the "### Hooks" / "### Project memory" style subsections inside "## Configuration (`.orchestra.yml`)", add a new subsection after "### Project memory" (before the closing `---`):

```markdown
### MCP server mode

`orchestra mcp serve` exposes Orchestra's own code-intelligence tools (`explore`, `semantic_search`, `symbols`, `repo_map`, `runtime_query`, `lsp.*`) as an MCP server, so any MCP-capable client (Claude Code, Claude Desktop, Cursor, …) can use them directly. stdio is the default transport; `--http` serves Streamable HTTP instead and always requires a token (`--mcp-token` or `$ORCH_MCP_TOKEN`).

```bash
orchestra mcp serve --workspace-root /path/to/project
```
```

Add the matching Russian version to `README.ru.md` in the same location:

```markdown
### Режим MCP-сервера

`orchestra mcp serve` отдаёт собственные инструменты понимания кода Orchestra (`explore`, `semantic_search`, `symbols`, `repo_map`, `runtime_query`, `lsp.*`) как MCP-сервер — любой MCP-совместимый клиент (Claude Code, Claude Desktop, Cursor и т.д.) может пользоваться ими напрямую. По умолчанию — stdio; `--http` включает Streamable HTTP и всегда требует токен (`--mcp-token` или `$ORCH_MCP_TOKEN`).

```bash
orchestra mcp serve --workspace-root /path/to/project
```
```

- [ ] **Step 4: Commit**

```bash
git add docs/parity-plan-2026-09.md README.md README.ru.md
git commit -m "$(cat <<'EOF'
docs: C2 closed - Orchestra as an MCP server

orchestra mcp serve exposes explore/semantic_search/symbols/repo_map/
runtime_query/lsp.* as a real MCP server (stdio + token-gated Streamable
HTTP), reusing the agent's own tool schemas verbatim (enforced by a
standing parity test) and internal/core's existing Runner - no daemon
revival, no LLM required to start.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

- [ ] **Step 5: Push**

```bash
git push origin master
```

---

## Manual follow-up (not automatable, not part of this plan's tasks)

- **Live cross-host check**: configure a real MCP host (e.g. Claude Code's own MCP settings) to run `orchestra mcp serve` and confirm it lists and calls the dotted `lsp.*` tool names correctly end-to-end. Task 4's test proves the SDK itself round-trips dotted names correctly; it cannot prove every third-party MCP host's own tool-name handling (e.g. Claude Code's `mcp__<server>__<tool>` display convention) behaves identically. Do this once, by hand, after Task 7 ships.
- **Homebrew/Scoop/winget/Marketplace packaging** (B9, already shipped) needs no changes for this feature — `orchestra mcp serve` ships in the same `orchestra` binary.
