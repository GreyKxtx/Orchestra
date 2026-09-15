// internal/tools/browser.go
package web

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/orchestra/orchestra/protocol"
)

// The arguments below are the ones browser.PlaywrightMCPPackage accepts, as
// recorded in testdata/playwright-mcp-tools.json. That server addresses an
// element by `target` — a ref from its snapshot or a selector — and treats
// `element` only as a human-readable description.

// errNoBrowser is returned when browser tools are called without --allow-browser.
func errNoBrowser() error {
	return protocol.NewError(protocol.ExecDenied,
		"browser tools require --allow-browser flag", nil)
}

// setTarget addresses an element: the snapshot ref when there is one, otherwise
// the element string, which the server resolves as a selector.
func setTarget(args map[string]any, element, ref string) {
	if ref != "" {
		args["target"] = ref
	} else {
		args["target"] = element
	}
	if element != "" {
		args["element"] = element
	}
}

const (
	pagePollInterval   = 250 * time.Millisecond
	defaultWaitTimeout = 5 * time.Second // the server's own action timeout
	maxWaitTimeout     = 2 * time.Minute
	networkIdleTimeout = 30 * time.Second
)

// evaluateResult is the value line of a browser_evaluate answer
// ("### Result\n<json value>\n### Ran Playwright code...").
func evaluateResult(text string) string {
	_, after, ok := strings.Cut(text, "### Result\n")
	if !ok {
		return ""
	}
	line, _, _ := strings.Cut(after, "\n")
	return strings.TrimSpace(line)
}

// waitForPage asks the page fn — fixed code of ours, not the model's, so the
// allow_eval gate does not apply — until it answers true or the timeout passes.
func waitForPage(ctx context.Context, cfg Config, fn string, timeout time.Duration, what string) error {
	deadline := time.Now().Add(timeout)
	for {
		res, err := cfg.Browser.Call(ctx, "browser_evaluate", map[string]any{"function": fn})
		if err != nil {
			return err
		}
		if evaluateResult(res.TextContent()) == "true" {
			return nil
		}
		if time.Now().After(deadline) {
			return protocol.NewError(protocol.ExecTimeout,
				fmt.Sprintf("waited %dms and %s did not happen", timeout.Milliseconds(), what), nil)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pagePollInterval):
		}
	}
}

// --- browser.navigate ---

type BrowserNavigateRequest struct {
	URL       string `json:"url"`
	WaitUntil string `json:"wait_until,omitempty"`
}

type BrowserNavigateResponse struct {
	Result string `json:"result"`
}

func BrowserNavigate(ctx context.Context, cfg Config, req BrowserNavigateRequest) (*BrowserNavigateResponse, error) {
	if cfg.Browser == nil {
		return nil, errNoBrowser()
	}
	if strings.TrimSpace(req.URL) == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "url is required", nil)
	}
	// The server's navigation already waits for the load event, which covers
	// "load" and "domcontentloaded"; "networkidle" is waited for here.
	res, err := cfg.Browser.Call(ctx, "browser_navigate", map[string]any{"url": req.URL})
	if err != nil {
		return nil, err
	}
	if req.WaitUntil == "networkidle" {
		const idle = `async () => { const n = performance.getEntriesByType('resource').length; ` +
			`await new Promise(r => setTimeout(r, 500)); ` +
			`return document.readyState === 'complete' && performance.getEntriesByType('resource').length === n; }`
		if err := waitForPage(ctx, cfg, idle, networkIdleTimeout, "the network going idle"); err != nil {
			return nil, err
		}
	}
	return &BrowserNavigateResponse{Result: res.TextContent()}, nil
}

// --- browser.snapshot ---

type BrowserSnapshotRequest struct{}

type BrowserSnapshotResponse struct {
	Snapshot string `json:"snapshot"`
}

func BrowserSnapshot(ctx context.Context, cfg Config, req BrowserSnapshotRequest) (*BrowserSnapshotResponse, error) {
	if cfg.Browser == nil {
		return nil, errNoBrowser()
	}
	res, err := cfg.Browser.Call(ctx, "browser_snapshot", map[string]any{})
	if err != nil {
		return nil, err
	}
	return &BrowserSnapshotResponse{Snapshot: res.TextContent()}, nil
}

// --- browser.screenshot ---

type BrowserScreenshotRequest struct {
	FullPage bool `json:"full_page,omitempty"`
}

type BrowserScreenshotResponse struct {
	Image string `json:"image"` // base64 PNG, or text if no image
}

func BrowserScreenshot(ctx context.Context, cfg Config, req BrowserScreenshotRequest) (*BrowserScreenshotResponse, error) {
	if cfg.Browser == nil {
		return nil, errNoBrowser()
	}
	res, err := cfg.Browser.Call(ctx, "browser_take_screenshot", map[string]any{
		"fullPage": req.FullPage,
		"scale":    "css",
	})
	if err != nil {
		return nil, err
	}
	img := res.ImageContent()
	if img == "" {
		img = res.TextContent()
	}
	return &BrowserScreenshotResponse{Image: img}, nil
}

// --- browser.click ---

type BrowserClickRequest struct {
	Element string `json:"element,omitempty"`
	Ref     string `json:"ref,omitempty"`
}

type BrowserClickResponse struct {
	Result string `json:"result"`
}

func BrowserClick(ctx context.Context, cfg Config, req BrowserClickRequest) (*BrowserClickResponse, error) {
	if cfg.Browser == nil {
		return nil, errNoBrowser()
	}
	if req.Element == "" && req.Ref == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "element or ref is required", nil)
	}
	args := map[string]any{}
	setTarget(args, req.Element, req.Ref)
	res, err := cfg.Browser.Call(ctx, "browser_click", args)
	if err != nil {
		return nil, err
	}
	return &BrowserClickResponse{Result: res.TextContent()}, nil
}

// --- browser.type ---

type BrowserTypeRequest struct {
	Element string `json:"element,omitempty"`
	Ref     string `json:"ref,omitempty"`
	Text    string `json:"text"`
	Clear   bool   `json:"clear,omitempty"`
}

type BrowserTypeResponse struct {
	Result string `json:"result"`
}

func BrowserType(ctx context.Context, cfg Config, req BrowserTypeRequest) (*BrowserTypeResponse, error) {
	if cfg.Browser == nil {
		return nil, errNoBrowser()
	}
	if req.Text == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "text is required", nil)
	}
	if req.Element == "" && req.Ref == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "element or ref is required", nil)
	}
	// The server fills the field, replacing what it held, so Clear is what
	// happens either way.
	args := map[string]any{"text": req.Text}
	setTarget(args, req.Element, req.Ref)
	res, err := cfg.Browser.Call(ctx, "browser_type", args)
	if err != nil {
		return nil, err
	}
	return &BrowserTypeResponse{Result: res.TextContent()}, nil
}

// --- browser.fill ---

type BrowserFillField struct {
	Element string `json:"element,omitempty"`
	Ref     string `json:"ref,omitempty"`
	Value   string `json:"value"`
}

type BrowserFillRequest struct {
	Fields []BrowserFillField `json:"fields"`
}

type BrowserFillResponse struct {
	Filled int `json:"filled"`
}

func BrowserFill(ctx context.Context, cfg Config, req BrowserFillRequest) (*BrowserFillResponse, error) {
	if cfg.Browser == nil {
		return nil, errNoBrowser()
	}
	if len(req.Fields) == 0 {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "fields array is required and must not be empty", nil)
	}
	mcpFields := make([]map[string]any, 0, len(req.Fields))
	for _, f := range req.Fields {
		if f.Element == "" && f.Ref == "" {
			return nil, protocol.NewError(protocol.InvalidLLMOutput,
				"each field requires element or ref", nil)
		}
		// The server wants a field name and kind; the tool fills text fields.
		mf := map[string]any{"value": f.Value, "type": "textbox"}
		setTarget(mf, f.Element, f.Ref)
		mf["name"] = mf["target"]
		mcpFields = append(mcpFields, mf)
	}
	_, err := cfg.Browser.Call(ctx, "browser_fill_form", map[string]any{"fields": mcpFields})
	if err != nil {
		return nil, err
	}
	return &BrowserFillResponse{Filled: len(req.Fields)}, nil
}

// --- browser.select ---

type BrowserSelectRequest struct {
	Element string `json:"element,omitempty"`
	Ref     string `json:"ref,omitempty"`
	Value   string `json:"value"`
}

type BrowserSelectResponse struct {
	Result string `json:"result"`
}

func BrowserSelect(ctx context.Context, cfg Config, req BrowserSelectRequest) (*BrowserSelectResponse, error) {
	if cfg.Browser == nil {
		return nil, errNoBrowser()
	}
	if req.Element == "" && req.Ref == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "element or ref is required", nil)
	}
	if req.Value == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "value is required", nil)
	}
	args := map[string]any{"values": []string{req.Value}}
	setTarget(args, req.Element, req.Ref)
	res, err := cfg.Browser.Call(ctx, "browser_select_option", args)
	if err != nil {
		return nil, err
	}
	return &BrowserSelectResponse{Result: res.TextContent()}, nil
}

// --- browser.eval ---

type BrowserEvalRequest struct {
	Expression string `json:"expression"`
}

type BrowserEvalResponse struct {
	Result string `json:"result"`
}

func BrowserEval(ctx context.Context, cfg Config, req BrowserEvalRequest) (*BrowserEvalResponse, error) {
	if cfg.Browser == nil {
		return nil, errNoBrowser()
	}
	if !cfg.AllowBrowserEval {
		return nil, protocol.NewError(protocol.ExecDenied,
			"browser.eval requires allow_eval: true in browser config", nil)
	}
	if strings.TrimSpace(req.Expression) == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "expression is required", nil)
	}
	// The server takes a function or a bare expression, which it wraps.
	res, err := cfg.Browser.Call(ctx, "browser_evaluate", map[string]any{
		"function": req.Expression,
	})
	if err != nil {
		return nil, err
	}
	return &BrowserEvalResponse{Result: res.TextContent()}, nil
}

// --- browser.wait ---

type BrowserWaitRequest struct {
	URL       string `json:"url,omitempty"`
	Selector  string `json:"selector,omitempty"`
	Text      string `json:"text,omitempty"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`
}

type BrowserWaitResponse struct {
	Result string `json:"result"`
}

func BrowserWait(ctx context.Context, cfg Config, req BrowserWaitRequest) (*BrowserWaitResponse, error) {
	if cfg.Browser == nil {
		return nil, errNoBrowser()
	}
	if req.URL == "" && req.Selector == "" && req.Text == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput,
			"one of url, selector, or text is required", nil)
	}
	timeout := defaultWaitTimeout
	if req.TimeoutMS > 0 {
		timeout = min(time.Duration(req.TimeoutMS)*time.Millisecond, maxWaitTimeout)
	}
	// The server waits only for text or a fixed time, so every condition is
	// checked by asking the page; all given conditions must hold.
	cond, _ := json.Marshal(map[string]string{"url": req.URL, "selector": req.Selector, "text": req.Text})
	fn := "() => { const c = " + string(cond) + "; " +
		"if (c.url && !location.href.includes(c.url)) return false; " +
		"if (c.selector && !document.querySelector(c.selector)) return false; " +
		"if (c.text && !(document.body && document.body.innerText.includes(c.text))) return false; " +
		"return true; }"
	var what []string
	if req.URL != "" {
		what = append(what, fmt.Sprintf("a URL containing %q", req.URL))
	}
	if req.Selector != "" {
		what = append(what, fmt.Sprintf("an element matching %q", req.Selector))
	}
	if req.Text != "" {
		what = append(what, fmt.Sprintf("the text %q", req.Text))
	}
	desc := strings.Join(what, " and ")
	if err := waitForPage(ctx, cfg, fn, timeout, desc); err != nil {
		return nil, err
	}
	return &BrowserWaitResponse{Result: "found " + desc}, nil
}

// --- browser.close ---

type BrowserCloseRequest struct{}

type BrowserCloseResponse struct {
	Closed bool `json:"closed"`
}

func BrowserClose(ctx context.Context, cfg Config, req BrowserCloseRequest) (*BrowserCloseResponse, error) {
	if cfg.Browser == nil {
		return nil, errNoBrowser()
	}
	_, err := cfg.Browser.Call(ctx, "browser_close", map[string]any{})
	if err != nil {
		return nil, err
	}
	return &BrowserCloseResponse{Closed: true}, nil
}

