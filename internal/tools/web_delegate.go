package tools

import (
	"context"

	"github.com/orchestra/orchestra/internal/tools/toolslsp"
	"github.com/orchestra/orchestra/internal/tools/web"
)

type (
	WebFetchRequest  = web.WebFetchRequest
	WebFetchResponse = web.WebFetchResponse
	WebSearchRequest = web.WebSearchRequest
	WebSearchResponse = web.WebSearchResponse
	WebSearchResult  = web.WebSearchResult
	BrowserNavigateRequest   = web.BrowserNavigateRequest
	BrowserNavigateResponse  = web.BrowserNavigateResponse
	BrowserSnapshotRequest   = web.BrowserSnapshotRequest
	BrowserSnapshotResponse  = web.BrowserSnapshotResponse
	BrowserScreenshotRequest = web.BrowserScreenshotRequest
	BrowserScreenshotResponse = web.BrowserScreenshotResponse
	BrowserClickRequest      = web.BrowserClickRequest
	BrowserClickResponse     = web.BrowserClickResponse
	BrowserTypeRequest       = web.BrowserTypeRequest
	BrowserTypeResponse      = web.BrowserTypeResponse
	BrowserFillRequest       = web.BrowserFillRequest
	BrowserFillResponse      = web.BrowserFillResponse
	BrowserFillField         = web.BrowserFillField
	BrowserSelectRequest     = web.BrowserSelectRequest
	BrowserSelectResponse    = web.BrowserSelectResponse
	BrowserEvalRequest       = web.BrowserEvalRequest
	BrowserEvalResponse      = web.BrowserEvalResponse
	BrowserWaitRequest       = web.BrowserWaitRequest
	BrowserWaitResponse      = web.BrowserWaitResponse
	BrowserCloseRequest      = web.BrowserCloseRequest
	BrowserCloseResponse     = web.BrowserCloseResponse
	LSPDefinitionRequest   = toolslsp.LSPDefinitionRequest
	LSPDefinitionResponse  = toolslsp.LSPDefinitionResponse
	LSPReferencesRequest   = toolslsp.LSPReferencesRequest
	LSPReferencesResponse  = toolslsp.LSPReferencesResponse
	LSPHoverRequest        = toolslsp.LSPHoverRequest
	LSPHoverResponse       = toolslsp.LSPHoverResponse
	LSPDiagnosticsRequest  = toolslsp.LSPDiagnosticsRequest
	LSPDiagnosticsResponse = toolslsp.LSPDiagnosticsResponse
	LSPRenameRequest       = toolslsp.LSPRenameRequest
	LSPRenameResponse      = toolslsp.LSPRenameResponse
)

func (r *Runner) webConfig() web.Config {
	return web.Config{
		FetchTimeout:     r.webFetchTimeout,
		MaxContentBytes:  r.webMaxContentBytes,
		Search:           r.webSearchCfg,
		TavilyEndpoint:   r.webSearchTavilyEndpoint,
		BraveEndpoint:    r.webSearchBraveEndpoint,
		Browser:          r.browserClient,
		AllowBrowserEval: r.allowBrowserEval,
	}
}

func (r *Runner) WebFetch(ctx context.Context, req WebFetchRequest) (*WebFetchResponse, error) {
	return web.WebFetch(ctx, r.webConfig(), req)
}

func (r *Runner) WebSearch(ctx context.Context, req WebSearchRequest) (*WebSearchResponse, error) {
	return web.WebSearch(ctx, r.webConfig(), req)
}

func (r *Runner) BrowserNavigate(ctx context.Context, req BrowserNavigateRequest) (*BrowserNavigateResponse, error) {
	return web.BrowserNavigate(ctx, r.webConfig(), req)
}

func (r *Runner) BrowserSnapshot(ctx context.Context, req BrowserSnapshotRequest) (*BrowserSnapshotResponse, error) {
	return web.BrowserSnapshot(ctx, r.webConfig(), req)
}

func (r *Runner) BrowserScreenshot(ctx context.Context, req BrowserScreenshotRequest) (*BrowserScreenshotResponse, error) {
	return web.BrowserScreenshot(ctx, r.webConfig(), req)
}

func (r *Runner) BrowserClick(ctx context.Context, req BrowserClickRequest) (*BrowserClickResponse, error) {
	return web.BrowserClick(ctx, r.webConfig(), req)
}

func (r *Runner) BrowserType(ctx context.Context, req BrowserTypeRequest) (*BrowserTypeResponse, error) {
	return web.BrowserType(ctx, r.webConfig(), req)
}

func (r *Runner) BrowserFill(ctx context.Context, req BrowserFillRequest) (*BrowserFillResponse, error) {
	return web.BrowserFill(ctx, r.webConfig(), req)
}

func (r *Runner) BrowserSelect(ctx context.Context, req BrowserSelectRequest) (*BrowserSelectResponse, error) {
	return web.BrowserSelect(ctx, r.webConfig(), req)
}

func (r *Runner) BrowserEval(ctx context.Context, req BrowserEvalRequest) (*BrowserEvalResponse, error) {
	return web.BrowserEval(ctx, r.webConfig(), req)
}

func (r *Runner) BrowserWait(ctx context.Context, req BrowserWaitRequest) (*BrowserWaitResponse, error) {
	return web.BrowserWait(ctx, r.webConfig(), req)
}

func (r *Runner) BrowserClose(ctx context.Context, req BrowserCloseRequest) (*BrowserCloseResponse, error) {
	return web.BrowserClose(ctx, r.webConfig(), req)
}

func (r *Runner) lspClient() *toolslsp.Client {
	if r == nil {
		return nil
	}
	return toolslsp.NewClient(r.workspaceRoot, r.lspManager)
}

func (r *Runner) LSPDefinition(ctx context.Context, req LSPDefinitionRequest) (*LSPDefinitionResponse, error) {
	return r.lspClient().LSPDefinition(ctx, req)
}

func (r *Runner) LSPReferences(ctx context.Context, req LSPReferencesRequest) (*LSPReferencesResponse, error) {
	return r.lspClient().LSPReferences(ctx, req)
}

func (r *Runner) LSPHover(ctx context.Context, req LSPHoverRequest) (*LSPHoverResponse, error) {
	return r.lspClient().LSPHover(ctx, req)
}

func (r *Runner) LSPDiagnostics(ctx context.Context, req LSPDiagnosticsRequest) (*LSPDiagnosticsResponse, error) {
	return r.lspClient().LSPDiagnostics(ctx, req)
}

func (r *Runner) LSPRename(ctx context.Context, req LSPRenameRequest) (*LSPRenameResponse, error) {
	return r.lspClient().LSPRename(ctx, req)
}
