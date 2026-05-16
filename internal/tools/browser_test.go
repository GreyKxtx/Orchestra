// internal/tools/browser_test.go
package tools

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/orchestra/orchestra/internal/browser"
	"github.com/orchestra/orchestra/internal/config"
)

// skipIfNoBrowser skips if ORCH_E2E_BROWSER=1 is not set.
func skipIfNoBrowser(t *testing.T) {
	t.Helper()
	if os.Getenv("ORCH_E2E_BROWSER") != "1" {
		t.Skip("set ORCH_E2E_BROWSER=1 to run browser integration tests")
	}
}

// TestBrowserMain acts as a mock MCP server when BE_BROWSER_MOCK=1.
// It is invoked as a subprocess by newBrowserRunner.
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

// newBrowserRunner creates a Runner with a mock browser.Client backed by the test binary.
func newBrowserRunner(t *testing.T, allowEval bool) *Runner {
	t.Helper()
	root := t.TempDir()
	r, err := NewRunner(root, RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cfg := browser.Config{
		Headless:    true,
		TimeoutMS:   5000,
		AllowEval:   allowEval,
		CmdOverride: []string{exe, "-test.run=^TestBrowserMain$", "-test.v=false"},
		EnvOverride: append(os.Environ(), "BE_BROWSER_MOCK=1"),
	}
	r.browserClient = browser.New(cfg)
	r.allowBrowserEval = allowEval
	return r
}

func TestBrowserNavigate_RejectsEmptyURL(t *testing.T) {
	r := newBrowserRunner(t, false)
	ctx := context.Background()
	_, err := r.BrowserNavigate(ctx, BrowserNavigateRequest{URL: ""})
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
}

func TestBrowserNavigate_DisabledWithoutFlag(t *testing.T) {
	root := t.TempDir()
	r, err := NewRunner(root, RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer func() { _ = r.Close() }()
	// browserClient is nil — simulates --allow-browser not set
	_, err = r.BrowserNavigate(context.Background(), BrowserNavigateRequest{URL: "https://example.com"})
	if err == nil {
		t.Fatal("expected error when browserClient is nil")
	}
}

func TestBrowserEval_RequiresAllowEval(t *testing.T) {
	r := newBrowserRunner(t, false) // allowEval=false
	ctx := context.Background()
	_, err := r.BrowserEval(ctx, BrowserEvalRequest{Expression: "document.title"})
	if err == nil {
		t.Fatal("expected error when allowEval=false")
	}
}

func TestBrowserEval_WorksWhenAllowed(t *testing.T) {
	r := newBrowserRunner(t, true) // allowEval=true
	ctx := context.Background()
	res, err := r.BrowserEval(ctx, BrowserEvalRequest{Expression: "document.title"})
	if err != nil {
		t.Fatalf("BrowserEval: %v", err)
	}
	if res.Result == "" {
		t.Error("expected non-empty result")
	}
}

func TestBrowserClick_RequiresElementOrRef(t *testing.T) {
	r := newBrowserRunner(t, false)
	ctx := context.Background()
	_, err := r.BrowserClick(ctx, BrowserClickRequest{})
	if err == nil {
		t.Fatal("expected error when both element and ref are empty")
	}
}

func TestBrowserWait_RequiresCondition(t *testing.T) {
	r := newBrowserRunner(t, false)
	ctx := context.Background()
	_, err := r.BrowserWait(ctx, BrowserWaitRequest{})
	if err == nil {
		t.Fatal("expected error when no condition provided")
	}
}

func TestBrowserFill_RejectsEmptyFields(t *testing.T) {
	r := newBrowserRunner(t, false)
	ctx := context.Background()
	_, err := r.BrowserFill(ctx, BrowserFillRequest{Fields: nil})
	if err == nil {
		t.Fatal("expected error for empty fields")
	}
}

func TestBrowserNavigate_CallsClient(t *testing.T) {
	r := newBrowserRunner(t, false)
	ctx := context.Background()
	res, err := r.BrowserNavigate(ctx, BrowserNavigateRequest{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("BrowserNavigate: %v", err)
	}
	if res.Result == "" {
		t.Error("expected non-empty result")
	}
}

func TestBrowserSnapshot_CallsClient(t *testing.T) {
	r := newBrowserRunner(t, false)
	ctx := context.Background()
	res, err := r.BrowserSnapshot(ctx, BrowserSnapshotRequest{})
	if err != nil {
		t.Fatalf("BrowserSnapshot: %v", err)
	}
	if res.Snapshot == "" {
		t.Error("expected non-empty snapshot")
	}
}

// --- Integration tests (real browser, gated by ORCH_E2E_BROWSER=1) ---

func TestBrowserE2E_NavigateAndSnapshot(t *testing.T) {
	skipIfNoBrowser(t)
	root := t.TempDir()
	r, err := NewRunner(root, RunnerOptions{
		Browser:      config.BrowserConfig{Headless: true, TimeoutMS: 30000, ViewportWidth: 1280, ViewportHeight: 720},
		AllowBrowser: true,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer func() { _ = r.Close() }()

	ctx := context.Background()
	navRes, err := r.BrowserNavigate(ctx, BrowserNavigateRequest{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("BrowserNavigate: %v", err)
	}
	t.Logf("navigate result: %s", navRes.Result)

	snapRes, err := r.BrowserSnapshot(ctx, BrowserSnapshotRequest{})
	if err != nil {
		t.Fatalf("BrowserSnapshot: %v", err)
	}
	if snapRes.Snapshot == "" {
		t.Error("expected non-empty snapshot")
	}
	t.Logf("snapshot (first 200 chars): %s", snapRes.Snapshot[:min(200, len(snapRes.Snapshot))])
}
