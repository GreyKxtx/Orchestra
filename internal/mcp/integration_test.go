// internal/mcp/integration_test.go
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
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

	// clientCaps is what the client advertised at initialize. The point of a
	// capability is that a server reads it and acts on it, so the fake server
	// records it and the test asserts on what it saw.
	var clientCaps map[string]json.RawMessage

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

		// A message with an id and no method is a REPLY to something this
		// fake server sent (see the ask_model tool below), not a request.
		if method == "" {
			continue
		}

		switch method {
		case "initialize":
			var ip struct {
				Capabilities map[string]json.RawMessage `json:"capabilities"`
			}
			_ = json.Unmarshal(req["params"], &ip)
			clientCaps = ip.Capabilities
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
							"name":        "ask_model",
							"description": "Asks the client to sample its model, then reports what came back",
							"inputSchema": map[string]any{"type": "object"},
						},
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
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			_ = json.Unmarshal(req["params"], &params)

			if params.Name == "ask_model" {
				_, advertised := clientCaps["sampling"]
				text, rpcErr := fakeServerSamples(dec, enc)
				_ = enc.Encode(map[string]any{
					"jsonrpc": "2.0",
					"id":      idRaw,
					"result": map[string]any{
						"content": []any{map[string]any{
							"type": "text",
							"text": fmt.Sprintf("advertised=%v sampled=%q err=%q", advertised, text, rpcErr),
						}},
					},
				})
				continue
			}

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

// fakeServerSamples sends a sampling/createMessage request to the CLIENT and
// waits for its reply — the server→client direction, over a real pipe between
// two real processes. Returns the sampled text, or the error message the
// client refused with.
func fakeServerSamples(dec *json.Decoder, enc *json.Encoder) (string, string) {
	const samplingID = 9001
	_ = enc.Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      samplingID,
		"method":  "sampling/createMessage",
		"params": map[string]any{
			"maxTokens": 64,
			"messages": []any{map[string]any{
				"role":    "user",
				"content": map[string]any{"type": "text", "text": "what is 2+2?"},
			}},
		},
	})
	for {
		var msg map[string]json.RawMessage
		if err := dec.Decode(&msg); err != nil {
			return "", "no reply: " + err.Error()
		}
		var id int64
		if err := json.Unmarshal(msg["id"], &id); err != nil || id != samplingID {
			continue
		}
		if raw, ok := msg["error"]; ok {
			var e struct {
				Message string `json:"message"`
			}
			_ = json.Unmarshal(raw, &e)
			return "", e.Message
		}
		var res struct {
			Content struct {
				Text string `json:"text"`
			} `json:"content"`
		}
		_ = json.Unmarshal(msg["result"], &res)
		return res.Content.Text, ""
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

// The whole path, through two real processes: Start's initialize advertises
// the capability, the fake server reads it and sends a sampling request back,
// the read loop routes it, the gates run, and the reply reaches the server.
//
// Everything else about this feature is tested in pieces. Pieces passing is
// how a capability that never reaches the wire, or a handler installed after
// the handshake, goes unnoticed.
func TestStdioSampling_EndToEnd(t *testing.T) {
	t.Setenv("ORCH_TEST_MCP_SERVER", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c, err := Start(ctx, "fake", testSelfBinary(), nil, StartOptions{
		Inbound: InboundOptions{
			AllowSampling: true,
			Consent:       func(context.Context, ConsentRequest) bool { return true },
			Sample: func(_ context.Context, req SamplingRequest) (SamplingResult, error) {
				if len(req.Messages) != 1 || req.Messages[0].Text != "what is 2+2?" {
					return SamplingResult{}, fmt.Errorf("messages did not cross the pipe: %+v", req.Messages)
				}
				return SamplingResult{Model: "fake-model", Text: "4"}, nil
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	out, err := c.Call(ctx, "ask_model", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "advertised=true") {
		t.Errorf("the server did not see the sampling capability at initialize: %q", out)
	}
	if !strings.Contains(out, `sampled="4"`) {
		t.Errorf("the sampled answer did not reach the server: %q", out)
	}
}

// The same path with the server not opted in: the capability must be absent
// from initialize, and a request sent anyway must come back refused rather
// than served or dropped.
func TestStdioSampling_NotEnabledIsAdvertisedAndRefused(t *testing.T) {
	t.Setenv("ORCH_TEST_MCP_SERVER", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sampled := false
	c, err := Start(ctx, "fake", testSelfBinary(), nil, StartOptions{
		Inbound: InboundOptions{
			AllowSampling: false,
			Consent:       func(context.Context, ConsentRequest) bool { return true },
			Sample: func(context.Context, SamplingRequest) (SamplingResult, error) {
				sampled = true
				return SamplingResult{Text: "should never happen"}, nil
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	out, err := c.Call(ctx, "ask_model", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "advertised=false") {
		t.Errorf("sampling was advertised to a server that never opted in: %q", out)
	}
	if !strings.Contains(out, "err=") || strings.Contains(out, `err=""`) {
		t.Errorf("the server got no refusal — a dropped request hangs it: %q", out)
	}
	if sampled {
		t.Error("the model was called for a server with sampling disabled")
	}
}
