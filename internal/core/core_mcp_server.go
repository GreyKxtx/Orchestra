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
