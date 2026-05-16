// internal/browser/client_test.go
package browser

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// TestMain branches: if BE_MOCK_MCP_SERVER=1, act as mock MCP server.
func TestMain(m *testing.M) {
	if os.Getenv("BE_MOCK_MCP_SERVER") == "1" {
		runMockMCPServer()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// runMockMCPServer reads MCP JSON-RPC requests from stdin and writes responses to stdout.
func runMockMCPServer() {
	dec := json.NewDecoder(os.Stdin)
	enc := json.NewEncoder(os.Stdout)

	for {
		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      any             `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		if err := dec.Decode(&req); err != nil {
			return
		}

		switch req.Method {
		case "initialize":
			_ = enc.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"protocolVersion": "2024-11-05",
					"capabilities":    map[string]any{"tools": map[string]any{}},
					"serverInfo":      map[string]any{"name": "playwright"},
				},
			})
		case "notifications/initialized":
			// no response
		case "tools/call":
			var params struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal(req.Params, &params)
			var text string
			switch params.Name {
			case "browser_navigate":
				text = "Navigated to https://example.com"
			case "browser_snapshot":
				text = "- heading \"Example Domain\" [level=1]"
			case "browser_close":
				text = "Browser closed"
			default:
				text = "ok"
			}
			_ = enc.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"content": []map[string]any{
						{"type": "text", "text": text},
					},
				},
			})
		}
	}
}

// newMockClient creates a Client that uses the test binary as the MCP server.
func newMockClient(t *testing.T) *Client {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cfg := Config{
		Headless:       true,
		TimeoutMS:      5000,
		ViewportWidth:  1280,
		ViewportHeight: 720,
		CmdOverride:    []string{exe, "-test.run=^TestMain$", "-test.v=false"},
		EnvOverride:    append(os.Environ(), "BE_MOCK_MCP_SERVER=1"),
	}
	c := New(cfg)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestClient_Navigate(t *testing.T) {
	c := newMockClient(t)
	ctx := context.Background()

	res, err := c.Call(ctx, "browser_navigate", map[string]any{"url": "https://example.com"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	text := res.TextContent()
	if !strings.Contains(text, "Navigated") {
		t.Errorf("expected text to contain 'Navigated', got %q", text)
	}
}

func TestClient_Snapshot(t *testing.T) {
	c := newMockClient(t)
	ctx := context.Background()

	res, err := c.Call(ctx, "browser_snapshot", map[string]any{})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	text := res.TextContent()
	if !strings.Contains(text, "heading") {
		t.Errorf("expected snapshot to contain 'heading', got %q", text)
	}
}

func TestClient_MultipleCallsReuseSubprocess(t *testing.T) {
	c := newMockClient(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, err := c.Call(ctx, "browser_navigate", map[string]any{"url": "https://example.com"}); err != nil {
			t.Fatalf("Call %d: %v", i, err)
		}
	}
}

func TestClient_TimeoutReturnsError(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	// Use a very short timeout so the initialize handshake times out.
	cfg := Config{
		Headless:    true,
		TimeoutMS:   1, // 1ms — will always time out
		CmdOverride: []string{exe, "-test.run=^TestMain$", "-test.v=false"},
		EnvOverride: append(os.Environ(), "BE_MOCK_MCP_SERVER=1"),
	}
	c := New(cfg)
	defer func() { _ = c.Close() }()

	ctx := context.Background()
	_, err = c.Call(ctx, "browser_navigate", map[string]any{"url": "https://example.com"})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestClient_FailsIfNpxMissing(t *testing.T) {
	cfg := Config{
		Headless:    true,
		TimeoutMS:   5000,
		CmdOverride: []string{"orchestra-nonexistent-binary-xyz"},
	}
	c := New(cfg)
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := c.Call(ctx, "browser_navigate", map[string]any{"url": "https://example.com"})
	if err == nil {
		t.Fatal("expected error for missing binary, got nil")
	}
}

func TestClient_Close_Idempotent(t *testing.T) {
	c := newMockClient(t)
	ctx := context.Background()

	if _, err := c.Call(ctx, "browser_navigate", map[string]any{"url": "https://example.com"}); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}
