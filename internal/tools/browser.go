// internal/tools/browser.go
package tools

import (
	"context"
	"strings"

	"github.com/orchestra/orchestra/internal/protocol"
)

// errNoBrowser is returned when browser tools are called without --allow-browser.
func errNoBrowser() error {
	return protocol.NewError(protocol.ExecDenied,
		"browser tools require --allow-browser flag", nil)
}

// --- browser.navigate ---

type BrowserNavigateRequest struct {
	URL       string `json:"url"`
	WaitUntil string `json:"wait_until,omitempty"`
}

type BrowserNavigateResponse struct {
	Result string `json:"result"`
}

func (r *Runner) BrowserNavigate(ctx context.Context, req BrowserNavigateRequest) (*BrowserNavigateResponse, error) {
	if r.browserClient == nil {
		return nil, errNoBrowser()
	}
	if strings.TrimSpace(req.URL) == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "url is required", nil)
	}
	args := map[string]any{"url": req.URL}
	if req.WaitUntil != "" {
		args["waitUntil"] = req.WaitUntil
	}
	res, err := r.browserClient.Call(ctx, "browser_navigate", args)
	if err != nil {
		return nil, err
	}
	return &BrowserNavigateResponse{Result: res.TextContent()}, nil
}

// --- browser.snapshot ---

type BrowserSnapshotRequest struct{}

type BrowserSnapshotResponse struct {
	Snapshot string `json:"snapshot"`
}

func (r *Runner) BrowserSnapshot(ctx context.Context, req BrowserSnapshotRequest) (*BrowserSnapshotResponse, error) {
	if r.browserClient == nil {
		return nil, errNoBrowser()
	}
	res, err := r.browserClient.Call(ctx, "browser_snapshot", map[string]any{})
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

func (r *Runner) BrowserScreenshot(ctx context.Context, req BrowserScreenshotRequest) (*BrowserScreenshotResponse, error) {
	if r.browserClient == nil {
		return nil, errNoBrowser()
	}
	res, err := r.browserClient.Call(ctx, "browser_take_screenshot", map[string]any{
		"fullPage": req.FullPage,
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

func (r *Runner) BrowserClick(ctx context.Context, req BrowserClickRequest) (*BrowserClickResponse, error) {
	if r.browserClient == nil {
		return nil, errNoBrowser()
	}
	if req.Element == "" && req.Ref == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "element or ref is required", nil)
	}
	args := map[string]any{}
	if req.Element != "" {
		args["element"] = req.Element
	}
	if req.Ref != "" {
		args["ref"] = req.Ref
	}
	res, err := r.browserClient.Call(ctx, "browser_click", args)
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

func (r *Runner) BrowserType(ctx context.Context, req BrowserTypeRequest) (*BrowserTypeResponse, error) {
	if r.browserClient == nil {
		return nil, errNoBrowser()
	}
	if req.Text == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "text is required", nil)
	}
	if req.Element == "" && req.Ref == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "element or ref is required", nil)
	}
	args := map[string]any{"text": req.Text}
	if req.Element != "" {
		args["element"] = req.Element
	}
	if req.Ref != "" {
		args["ref"] = req.Ref
	}
	if req.Clear {
		args["clear"] = true
	}
	res, err := r.browserClient.Call(ctx, "browser_type", args)
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

func (r *Runner) BrowserFill(ctx context.Context, req BrowserFillRequest) (*BrowserFillResponse, error) {
	if r.browserClient == nil {
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
		mf := map[string]any{"value": f.Value}
		if f.Ref != "" {
			mf["ref"] = f.Ref
		}
		if f.Element != "" {
			mf["element"] = f.Element
		}
		mcpFields = append(mcpFields, mf)
	}
	_, err := r.browserClient.Call(ctx, "browser_fill_form", map[string]any{"form": mcpFields})
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

func (r *Runner) BrowserSelect(ctx context.Context, req BrowserSelectRequest) (*BrowserSelectResponse, error) {
	if r.browserClient == nil {
		return nil, errNoBrowser()
	}
	if req.Element == "" && req.Ref == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "element or ref is required", nil)
	}
	if req.Value == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "value is required", nil)
	}
	args := map[string]any{"values": []string{req.Value}}
	if req.Element != "" {
		args["element"] = req.Element
	}
	if req.Ref != "" {
		args["ref"] = req.Ref
	}
	res, err := r.browserClient.Call(ctx, "browser_select_option", args)
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

func (r *Runner) BrowserEval(ctx context.Context, req BrowserEvalRequest) (*BrowserEvalResponse, error) {
	if r.browserClient == nil {
		return nil, errNoBrowser()
	}
	if !r.allowBrowserEval {
		return nil, protocol.NewError(protocol.ExecDenied,
			"browser.eval requires allow_eval: true in browser config", nil)
	}
	if strings.TrimSpace(req.Expression) == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "expression is required", nil)
	}
	res, err := r.browserClient.Call(ctx, "browser_evaluate", map[string]any{
		"expression": req.Expression,
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

func (r *Runner) BrowserWait(ctx context.Context, req BrowserWaitRequest) (*BrowserWaitResponse, error) {
	if r.browserClient == nil {
		return nil, errNoBrowser()
	}
	if req.URL == "" && req.Selector == "" && req.Text == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput,
			"one of url, selector, or text is required", nil)
	}
	args := map[string]any{}
	if req.URL != "" {
		args["url"] = req.URL
	}
	if req.Selector != "" {
		args["selector"] = req.Selector
	}
	if req.Text != "" {
		args["text"] = req.Text
	}
	if req.TimeoutMS > 0 {
		args["timeout"] = req.TimeoutMS
	}
	res, err := r.browserClient.Call(ctx, "browser_wait_for", args)
	if err != nil {
		return nil, err
	}
	return &BrowserWaitResponse{Result: res.TextContent()}, nil
}

// --- browser.close ---

type BrowserCloseRequest struct{}

type BrowserCloseResponse struct {
	Closed bool `json:"closed"`
}

func (r *Runner) BrowserClose(ctx context.Context, req BrowserCloseRequest) (*BrowserCloseResponse, error) {
	if r.browserClient == nil {
		return nil, errNoBrowser()
	}
	_, err := r.browserClient.Call(ctx, "browser_close", map[string]any{})
	if err != nil {
		return nil, err
	}
	return &BrowserCloseResponse{Closed: true}, nil
}
