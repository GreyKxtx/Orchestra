// internal/mcp/integration_test.go
package mcp

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/config"
)

// TestMain detects when running as a fake MCP server subprocess.
// When ORCH_TEST_MCP_SERVER=1, serves the MCP protocol on stdin/stdout.
func TestMain(m *testing.M) {
	if os.Getenv("ORCH_TEST_MCP_SERVER") == "1" {
		runFakeMCPServer()
		os.Exit(0)
	}
	os.Unsetenv("ORCH_TEST_MCP_SERVER") // prevent inherited env from infecting child processes
	os.Exit(m.Run())
}

// runFakeMCPServer serves a minimal MCP protocol on stdin/stdout.
// Supports: initialize, tools/list, tools/call (echo tool).
// Protocol: newline-delimited JSON.
func runFakeMCPServer() {
	dec := json.NewDecoder(os.Stdin)
	enc := json.NewEncoder(os.Stdout)

	for {
		var req map[string]json.RawMessage
		if err := dec.Decode(&req); err != nil {
			return // EOF or pipe closed — normal shutdown
		}

		// Skip notifications (no "id" field, e.g. notifications/initialized).
		idRaw, hasID := req["id"]
		if !hasID {
			continue
		}

		var method string
		_ = json.Unmarshal(req["method"], &method)

		switch method {
		case "initialize":
			_ = enc.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      idRaw,
				"result": map[string]any{
					"protocolVersion": "2024-11-05",
					"capabilities":    map[string]any{"tools": map[string]any{}},
					"serverInfo":      map[string]any{"name": "testserver", "version": "1.0"},
				},
			})
		case "tools/list":
			_ = enc.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      idRaw,
				"result": map[string]any{
					"tools": []any{
						map[string]any{
							"name":        "echo",
							"description": "Echo back the input message",
							"inputSchema": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"message": map[string]any{
										"type":        "string",
										"description": "Message to echo",
									},
								},
								"required": []string{"message"},
							},
						},
					},
				},
			})
		case "tools/call":
			var params struct {
				Arguments map[string]any `json:"arguments"`
			}
			_ = json.Unmarshal(req["params"], &params)
			msg, _ := params.Arguments["message"].(string)
			_ = enc.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      idRaw,
				"result": map[string]any{
					"content": []any{
						map[string]any{"type": "text", "text": "echo: " + msg},
					},
					"isError": false,
				},
			})
		default:
			_ = enc.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      idRaw,
				"error":   map[string]any{"code": -32601, "message": "method not found: " + method},
			})
		}
	}
}

// testSelfBinary returns args to re-spawn this test binary as a fake MCP server.
// -test.run=^$ ensures no tests match; TestMain intercepts via env var.
func testSelfBinary() []string {
	return []string{os.Args[0], "-test.run=^$"}
}

func TestMCPClient_RealSubprocess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c, err := Start(ctx, "testserver", testSelfBinary(), map[string]string{"ORCH_TEST_MCP_SERVER": "1"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer c.Close()

	tools := c.Tools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "echo" {
		t.Fatalf("expected tool name 'echo', got %q", tools[0].Name)
	}
	if tools[0].Description != "Echo back the input message" {
		t.Fatalf("unexpected description: %q", tools[0].Description)
	}

	result, err := c.Call(ctx, "echo", json.RawMessage(`{"message":"hello world"}`))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if result != "echo: hello world" {
		t.Fatalf("expected %q, got %q", "echo: hello world", result)
	}
}

func TestMCPManager_RealSubprocess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg := config.MCPConfig{
		Servers: []config.MCPServerConfig{
			{
				Name:    "testserver",
				Command: testSelfBinary(),
				Env:     map[string]string{"ORCH_TEST_MCP_SERVER": "1"},
			},
		},
	}

	mgr, errs := NewManager(ctx, cfg)
	if len(errs) > 0 {
		t.Fatalf("NewManager errors: %v", errs)
	}
	defer mgr.Close()

	// Verify tool discovery.
	defs := mgr.ListToolDefs()
	if len(defs) != 1 {
		t.Fatalf("expected 1 tool def, got %d", len(defs))
	}
	wantName := "mcp:testserver:echo"
	if defs[0].Function.Name != wantName {
		t.Fatalf("expected tool name %q, got %q", wantName, defs[0].Function.Name)
	}

	// Verify tool call routing through manager.
	result, err := mgr.Call(ctx, "mcp:testserver:echo", json.RawMessage(`{"message":"test"}`))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	// Manager.Call wraps result in {"result":"..."}.
	var out struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if out.Result != "echo: test" {
		t.Fatalf("expected %q, got %q", "echo: test", out.Result)
	}
}
