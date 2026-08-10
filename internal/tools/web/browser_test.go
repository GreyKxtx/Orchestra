package web

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/orchestra/orchestra/internal/browser"
	"github.com/orchestra/orchestra/internal/config"
)

func skipIfNoBrowser(t *testing.T) {
	t.Helper()
	if os.Getenv("ORCH_E2E_BROWSER") != "1" {
		t.Skip("set ORCH_E2E_BROWSER=1 to run browser integration tests")
	}
}

// TestBrowserMain acts as a mock MCP server when BE_BROWSER_MOCK=1.
// Invoked as a subprocess by newMockBrowserConfig.
func TestBrowserMain(t *testing.T) {
	if os.Getenv("BE_BROWSER_MOCK") != "1" {
		t.Skip()
		return
	}
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
				"jsonrpc": "2.0", "id": req.ID,
				"result": map[string]any{
					"protocolVersion": "2024-11-05",
					"capabilities":    map[string]any{"tools": map[string]any{}},
					"serverInfo":      map[string]any{"name": "playwright"},
				},
			})
		case "notifications/initialized":
		case "tools/call":
			var p struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal(req.Params, &p)
			text := "ok:" + p.Name
			_ = enc.Encode(map[string]any{
				"jsonrpc": "2.0", "id": req.ID,
				"result": map[string]any{
					"content": []map[string]any{{"type": "text", "text": text}},
				},
			})
		}
	}
}

func newMockBrowserConfig(t *testing.T, allowEval bool) Config {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cli := browser.New(browser.Config{
		Headless:    true,
		TimeoutMS:   5000,
		AllowEval:   allowEval,
		CmdOverride: []string{exe, "-test.run=^TestBrowserMain$", "-test.v=false"},
		EnvOverride: append(os.Environ(), "BE_BROWSER_MOCK=1"),
	})
	t.Cleanup(func() { _ = cli.Close() })
	return Config{
		Browser:          cli,
		AllowBrowserEval: allowEval,
	}
}

func TestBrowserNavigate_RejectsEmptyURL(t *testing.T) {
	cfg := newMockBrowserConfig(t, false)
	ctx := context.Background()
	_, err := BrowserNavigate(ctx, cfg, BrowserNavigateRequest{URL: ""})
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
}

func TestBrowserNavigate_DisabledWithoutClient(t *testing.T) {
	_, err := BrowserNavigate(context.Background(), Config{}, BrowserNavigateRequest{URL: "https://example.com"})
	if err == nil {
		t.Fatal("expected error when browser client is nil")
	}
}

func TestBrowserEval_RequiresAllowEval(t *testing.T) {
	cfg := newMockBrowserConfig(t, false)
	ctx := context.Background()
	_, err := BrowserEval(ctx, cfg, BrowserEvalRequest{Expression: "document.title"})
	if err == nil {
		t.Fatal("expected error when allowEval=false")
	}
}

func TestBrowserEval_WorksWhenAllowed(t *testing.T) {
	cfg := newMockBrowserConfig(t, true)
	ctx := context.Background()
	res, err := BrowserEval(ctx, cfg, BrowserEvalRequest{Expression: "document.title"})
	if err != nil {
		t.Fatalf("BrowserEval: %v", err)
	}
	if res.Result == "" {
		t.Error("expected non-empty result")
	}
}

func TestBrowserClick_RequiresElementOrRef(t *testing.T) {
	cfg := newMockBrowserConfig(t, false)
	ctx := context.Background()
	_, err := BrowserClick(ctx, cfg, BrowserClickRequest{})
	if err == nil {
		t.Fatal("expected error when both element and ref are empty")
	}
}

func TestBrowserWait_RequiresCondition(t *testing.T) {
	cfg := newMockBrowserConfig(t, false)
	ctx := context.Background()
	_, err := BrowserWait(ctx, cfg, BrowserWaitRequest{})
	if err == nil {
		t.Fatal("expected error when no condition provided")
	}
}

func TestBrowserFill_RejectsEmptyFields(t *testing.T) {
	cfg := newMockBrowserConfig(t, false)
	ctx := context.Background()
	_, err := BrowserFill(ctx, cfg, BrowserFillRequest{Fields: nil})
	if err == nil {
		t.Fatal("expected error for empty fields")
	}
}

func TestBrowserType_RejectsEmptyText(t *testing.T) {
	cfg := newMockBrowserConfig(t, false)
	ctx := context.Background()
	_, err := BrowserType(ctx, cfg, BrowserTypeRequest{Element: "input", Text: ""})
	if err == nil {
		t.Fatal("expected error for empty text")
	}
}

func TestBrowserType_RejectsNoTarget(t *testing.T) {
	cfg := newMockBrowserConfig(t, false)
	ctx := context.Background()
	_, err := BrowserType(ctx, cfg, BrowserTypeRequest{Text: "hello"})
	if err == nil {
		t.Fatal("expected error when element and ref both empty")
	}
}

func TestBrowserSelect_RejectsNoTarget(t *testing.T) {
	cfg := newMockBrowserConfig(t, false)
	ctx := context.Background()
	_, err := BrowserSelect(ctx, cfg, BrowserSelectRequest{Value: "option1"})
	if err == nil {
		t.Fatal("expected error when element and ref both empty")
	}
}

func TestBrowserSelect_RejectsEmptyValue(t *testing.T) {
	cfg := newMockBrowserConfig(t, false)
	ctx := context.Background()
	_, err := BrowserSelect(ctx, cfg, BrowserSelectRequest{Element: "select", Value: ""})
	if err == nil {
		t.Fatal("expected error for empty value")
	}
}

func TestBrowserFill_RejectsFieldWithoutTarget(t *testing.T) {
	cfg := newMockBrowserConfig(t, false)
	ctx := context.Background()
	_, err := BrowserFill(ctx, cfg, BrowserFillRequest{
		Fields: []BrowserFillField{{Value: "alice"}},
	})
	if err == nil {
		t.Fatal("expected error for field without element or ref")
	}
}

func TestBrowserClose_CallsClient(t *testing.T) {
	cfg := newMockBrowserConfig(t, false)
	ctx := context.Background()
	res, err := BrowserClose(ctx, cfg, BrowserCloseRequest{})
	if err != nil {
		t.Fatalf("BrowserClose: %v", err)
	}
	if !res.Closed {
		t.Error("expected Closed=true")
	}
}

func TestBrowserScreenshot_CallsClient(t *testing.T) {
	cfg := newMockBrowserConfig(t, false)
	ctx := context.Background()
	res, err := BrowserScreenshot(ctx, cfg, BrowserScreenshotRequest{})
	if err != nil {
		t.Fatalf("BrowserScreenshot: %v", err)
	}
	if res.Image == "" {
		t.Error("expected non-empty image/result")
	}
}

func TestBrowserNavigate_CallsClient(t *testing.T) {
	cfg := newMockBrowserConfig(t, false)
	ctx := context.Background()
	res, err := BrowserNavigate(ctx, cfg, BrowserNavigateRequest{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("BrowserNavigate: %v", err)
	}
	if res.Result == "" {
		t.Error("expected non-empty result")
	}
}

func TestBrowserSnapshot_CallsClient(t *testing.T) {
	cfg := newMockBrowserConfig(t, false)
	ctx := context.Background()
	res, err := BrowserSnapshot(ctx, cfg, BrowserSnapshotRequest{})
	if err != nil {
		t.Fatalf("BrowserSnapshot: %v", err)
	}
	if res.Snapshot == "" {
		t.Error("expected non-empty snapshot")
	}
}

func TestBrowserE2E_NavigateAndSnapshot(t *testing.T) {
	skipIfNoBrowser(t)
	bc := config.BrowserConfig{
		Headless:       true,
		TimeoutMS:      30000,
		ViewportWidth:  1280,
		ViewportHeight: 720,
	}
	cli := browser.New(browser.Config{
		Headless:       bc.Headless,
		TimeoutMS:      bc.TimeoutMS,
		ViewportWidth:  bc.ViewportWidth,
		ViewportHeight: bc.ViewportHeight,
		AllowEval:      bc.AllowEval,
	})
	t.Cleanup(func() { _ = cli.Close() })

	cfg := Config{Browser: cli}
	ctx := context.Background()

	navRes, err := BrowserNavigate(ctx, cfg, BrowserNavigateRequest{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("BrowserNavigate: %v", err)
	}
	t.Logf("navigate result: %s", navRes.Result)

	snapRes, err := BrowserSnapshot(ctx, cfg, BrowserSnapshotRequest{})
	if err != nil {
		t.Fatalf("BrowserSnapshot: %v", err)
	}
	if snapRes.Snapshot == "" {
		t.Error("expected non-empty snapshot")
	}
	n := min(200, len(snapRes.Snapshot))
	t.Logf("snapshot (first %d chars): %s", n, snapRes.Snapshot[:n])
}
