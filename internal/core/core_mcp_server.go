package core

import (
	"context"
	"encoding/json"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/orchestra/orchestra/internal/tools/nav"
	"github.com/orchestra/orchestra/internal/tools/session"
	"github.com/orchestra/orchestra/internal/tools/toolslsp"
	"github.com/orchestra/orchestra/llm"
)

// MCPToolServer builds an MCP server exposing Orchestra's read-only
// code-intelligence tools (CKG explore, semantic search, symbols, repo map,
// runtime trace resolution, LSP) over c.tools, with no LLM/session/agent-loop
// involvement. Every tool's description and input schema is taken directly
// from the same llm.ToolDef Orchestra's own agent already uses, so an
// external MCP caller sees identical semantics to Orchestra's own agent.
func (c *Core) MCPToolServer() *mcpsdk.Server {
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "orchestra", Version: "1"}, nil)

	addRunnerTool(srv, nav.ToolExploreCodebase(), c.tools.ExploreCodebase)
	addRunnerTool(srv, nav.ToolSemanticSearch(), c.tools.SemanticSearch)
	addRunnerTool(srv, nav.ToolCodeSymbols(), c.tools.CodeSymbols)
	addRunnerTool(srv, nav.ToolRepoMap(), c.tools.RepoMap)
	addRunnerTool(srv, session.ToolRuntimeQuery(), c.tools.RuntimeQuery)
	addRunnerTool(srv, toolslsp.ToolLSPDefinition(), c.tools.LSPDefinition)
	addRunnerTool(srv, toolslsp.ToolLSPReferences(), c.tools.LSPReferences)
	addRunnerTool(srv, toolslsp.ToolLSPHover(), c.tools.LSPHover)
	addRunnerTool(srv, toolslsp.ToolLSPDiagnostics(), c.tools.LSPDiagnostics)
	addRunnerTool(srv, toolslsp.ToolLSPRename(), c.tools.LSPRename)

	return srv
}

// addRunnerTool registers one MCP tool on srv whose name/description/input
// schema come from def (the same llm.ToolDef Orchestra's own agent loop
// uses) and whose handler delegates to run, marshaling the response via
// mcpTextResult. All ten of Core's MCP tool registrations share this exact
// shape - pull .Function from a llm.ToolDef, call one Runner method, marshal
// the result - so this generic helper replaces ten near-identical blocks
// with one line each.
func addRunnerTool[In, Out any](srv *mcpsdk.Server, def llm.ToolDef, run func(context.Context, In) (Out, error)) {
	fn := def.Function
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: fn.Name, Description: fn.Description, InputSchema: fn.Parameters},
		func(ctx context.Context, _ *mcpsdk.CallToolRequest, in In) (*mcpsdk.CallToolResult, any, error) {
			resp, err := run(ctx, in)
			if err != nil {
				return nil, nil, err
			}
			res, err := mcpTextResult(resp)
			return res, nil, err
		})
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
