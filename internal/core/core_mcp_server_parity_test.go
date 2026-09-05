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
