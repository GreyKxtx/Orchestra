package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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
//
// It refuses arguments the pinned @playwright/mcp would refuse: every call is
// validated against the input schemas recorded from that version in
// testdata/playwright-mcp-tools.json. A mock that answered "ok" to anything is
// how six of the ten browser tools stayed broken behind green tests.
// BE_BROWSER_LOG names a file each call is appended to; BE_BROWSER_EVAL_RESULT
// is what browser_evaluate answers (default true).
func TestBrowserMain(t *testing.T) {
	if os.Getenv("BE_BROWSER_MOCK") != "1" {
		t.Skip()
		return
	}
	schemas := loadPinnedToolSchemas(t)
	evalResult := os.Getenv("BE_BROWSER_EVAL_RESULT")
	if evalResult == "" {
		evalResult = "true"
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
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			_ = json.Unmarshal(req.Params, &p)
			if logPath := os.Getenv("BE_BROWSER_LOG"); logPath != "" {
				if f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
					_, _ = f.WriteString(p.Name + " " + string(p.Arguments) + "\n")
					_ = f.Close()
				}
			}
			text, isError := "ok:"+p.Name, false
			if problems := validateAgainst(schemas, p.Name, p.Arguments); problems != "" {
				text, isError = "### Error\nInvalid arguments for tool \""+p.Name+"\":\n"+problems, true
			} else if p.Name == "browser_evaluate" {
				text = "### Result\n" + evalResult + "\n### Ran Playwright code\n```js\nawait page.evaluate('...');\n```"
			}
			_ = enc.Encode(map[string]any{
				"jsonrpc": "2.0", "id": req.ID,
				"result": map[string]any{
					"content": []map[string]any{{"type": "text", "text": text}},
					"isError": isError,
				},
			})
		}
	}
}

func newMockBrowserConfig(t *testing.T, allowEval bool, env ...string) Config {
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
		EnvOverride: append(append(os.Environ(), "BE_BROWSER_MOCK=1"), env...),
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
		WorkDir:        t.TempDir(),
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

const e2eSignupPage = `<!doctype html><html><head><title>Signup bench</title></head><body>
<form onsubmit="event.preventDefault(); setTimeout(function () {
  document.getElementById('out').textContent = 'Created ' + document.getElementById('name').value +
    ' / ' + document.getElementById('email').value + ' / ' + document.getElementById('plan').value; }, 300);">
<label for="name">Full name</label><input id="name" type="text">
<label for="email">Email</label><input id="email" type="text">
<label for="plan">Plan</label><select id="plan"><option value="free">Free</option><option value="pro">Pro</option><option value="team">Team</option></select>
<button id="go" type="submit">Create account</button></form><p id="out"></p></body></html>`

// Every tool that acts on a page, against the real pinned server. The navigate
// and snapshot test above passed the whole time six of the other tools were
// refused by the server; run this one whenever PlaywrightMCPPackage moves.
func TestBrowserE2E_FillsAndSubmitsAForm(t *testing.T) {
	skipIfNoBrowser(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(e2eSignupPage))
	}))
	t.Cleanup(srv.Close)

	root := t.TempDir()
	cli := browser.New(browser.Config{Headless: true, TimeoutMS: 60000, WorkDir: root})
	t.Cleanup(func() { _ = cli.Close() })
	cfg := Config{Browser: cli, AllowBrowserEval: true}
	ctx := context.Background()
	must := func(what string, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}

	_, err := BrowserNavigate(ctx, cfg, BrowserNavigateRequest{URL: srv.URL, WaitUntil: "networkidle"})
	must("navigate", err)
	snap, err := BrowserSnapshot(ctx, cfg, BrowserSnapshotRequest{})
	must("snapshot", err)
	m := regexp.MustCompile(`textbox "Full name" \[ref=([a-z0-9]+)\]`).FindStringSubmatch(snap.Snapshot)
	if m == nil {
		t.Fatalf("no ref for the name field in the snapshot:\n%s", snap.Snapshot)
	}

	_, err = BrowserType(ctx, cfg, BrowserTypeRequest{Ref: m[1], Text: "Ada Lovelace"})
	must("type by ref", err)
	_, err = BrowserFill(ctx, cfg, BrowserFillRequest{Fields: []BrowserFillField{{Element: "#email", Value: "ada@example.com"}}})
	must("fill by selector", err)
	_, err = BrowserSelect(ctx, cfg, BrowserSelectRequest{Element: "#plan", Value: "Team"})
	must("select by visible text", err)
	_, err = BrowserClick(ctx, cfg, BrowserClickRequest{Element: "#go"})
	must("click", err)
	_, err = BrowserWait(ctx, cfg, BrowserWaitRequest{Selector: "#out:not(:empty)", Text: "Created", TimeoutMS: 5000})
	must("wait for the result", err)
	got, err := BrowserEval(ctx, cfg, BrowserEvalRequest{Expression: "document.getElementById('out').textContent"})
	must("eval", err)
	if want := "Created Ada Lovelace / ada@example.com / team"; !strings.Contains(got.Result, want) {
		t.Errorf("page ended with %q, want %q", got.Result, want)
	}
	if _, err := BrowserWait(ctx, cfg, BrowserWaitRequest{Selector: "#never", TimeoutMS: 500}); err == nil {
		t.Error("waiting for an element that never appears succeeded")
	}
	shot, err := BrowserScreenshot(ctx, cfg, BrowserScreenshotRequest{FullPage: true})
	must("screenshot", err)
	if !strings.HasPrefix(shot.Image, "iVBOR") {
		t.Errorf("screenshot is not a base64 PNG: %.40q", shot.Image)
	}
	_, err = BrowserClose(ctx, cfg, BrowserCloseRequest{})
	must("close", err)

	if entries, _ := os.ReadDir(filepath.Join(root, ".orchestra", "browser")); len(entries) == 0 {
		t.Error("the server's files did not land under .orchestra/browser")
	}
}
