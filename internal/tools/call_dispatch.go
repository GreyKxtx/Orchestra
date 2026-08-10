package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

type toolDispatchFn func(ctx context.Context, r *Runner, input json.RawMessage) (json.RawMessage, error)

func dispatchRunnerTool[Req any, Resp any](call func(*Runner, context.Context, Req) (*Resp, error)) toolDispatchFn {
	return func(ctx context.Context, r *Runner, input json.RawMessage) (json.RawMessage, error) {
		var req Req
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := call(r, ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)
	}
}

var toolDispatchTable = map[string]toolDispatchFn{
	"ls":              dispatchRunnerTool((*Runner).FSList),
	"read":            dispatchRunnerTool((*Runner).FSRead),
	"glob":            dispatchRunnerTool((*Runner).FSGlob),
	"write":           dispatchRunnerTool((*Runner).FSWrite),
	"edit":            dispatchRunnerTool((*Runner).FSEdit),
	"symbols":         dispatchRunnerTool((*Runner).CodeSymbols),
	"explore":         dispatchRunnerTool((*Runner).ExploreCodebase),
	"diff.preview":    dispatchRunnerTool((*Runner).FSPreview),
	"bash.output":     dispatchRunnerTool((*Runner).ExecBashOutput),
	"bash.kill":       dispatchRunnerTool((*Runner).ExecBashKill),
	"semantic_search": dispatchRunnerTool((*Runner).SemanticSearch),
	"repo_map":        dispatchRunnerTool((*Runner).RepoMap),
	"ast_rename":      dispatchRunnerTool((*Runner).ASTRename),
	"webfetch":        dispatchRunnerTool((*Runner).WebFetch),
	"websearch":       dispatchRunnerTool((*Runner).WebSearch),
	"memory_read":     dispatchRunnerTool((*Runner).MemoryRead),
	"memory_write":    dispatchRunnerTool((*Runner).MemoryWrite),
	"memory_search":   dispatchRunnerTool((*Runner).MemorySearch),
	"runtime_query":   dispatchRunnerTool((*Runner).RuntimeQuery),
	"lsp.definition":  dispatchRunnerTool((*Runner).LSPDefinition),
	"lsp.references":  dispatchRunnerTool((*Runner).LSPReferences),
	"lsp.hover":       dispatchRunnerTool((*Runner).LSPHover),
	"lsp.diagnostics": dispatchRunnerTool((*Runner).LSPDiagnostics),
	"lsp.rename":      dispatchRunnerTool((*Runner).LSPRename),
	"fs.delete":       dispatchRunnerTool((*Runner).FSDelete),
	"fs.rename":       dispatchRunnerTool((*Runner).FSRename),
	"git.status":      dispatchRunnerTool((*Runner).GitStatus),
	"git.log":         dispatchRunnerTool((*Runner).GitLog),
	"git.diff":        dispatchRunnerTool((*Runner).GitDiff),
	"git.commit":      dispatchRunnerTool((*Runner).GitCommit),
	"git.branch":      dispatchRunnerTool((*Runner).GitBranch),
	"git.checkout":    dispatchRunnerTool((*Runner).GitCheckout),
	"git.push":        dispatchRunnerTool((*Runner).GitPush),
	"browser.navigate":  dispatchRunnerTool((*Runner).BrowserNavigate),
	"browser.snapshot":  dispatchRunnerTool((*Runner).BrowserSnapshot),
	"browser.screenshot": dispatchRunnerTool((*Runner).BrowserScreenshot),
	"browser.click":     dispatchRunnerTool((*Runner).BrowserClick),
	"browser.type":      dispatchRunnerTool((*Runner).BrowserType),
	"browser.fill":      dispatchRunnerTool((*Runner).BrowserFill),
	"browser.select":    dispatchRunnerTool((*Runner).BrowserSelect),
	"browser.eval":      dispatchRunnerTool((*Runner).BrowserEval),
	"browser.wait":      dispatchRunnerTool((*Runner).BrowserWait),
	"browser.close":     dispatchRunnerTool((*Runner).BrowserClose),
	"gh.pr.list":    dispatchRunnerTool((*Runner).GHPRList),
	"gh.pr.create":  dispatchRunnerTool((*Runner).GHPRCreate),
	"gh.pr.view":    dispatchRunnerTool((*Runner).GHPRView),
	"gh.issue.list": dispatchRunnerTool((*Runner).GHIssueList),
	"gh.issue.view": dispatchRunnerTool((*Runner).GHIssueView),

	"fs.apply_ops": func(ctx context.Context, r *Runner, input json.RawMessage) (json.RawMessage, error) {
		normalizedInput := normalizeOpsJSON(input)
		var req FSApplyOpsRequest
		if err := decodeToolInput(normalizedInput, &req); err != nil {
			return nil, err
		}
		resp, err := r.FSApplyOps(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)
	},

	"grep": func(ctx context.Context, r *Runner, input json.RawMessage) (json.RawMessage, error) {
		var req SearchTextRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.SearchText(ctx, req)
		if err != nil {
			return nil, err
		}
		return json.RawMessage(formatSearchResults(req.Query, resp)), nil
	},

	"bash": func(ctx context.Context, r *Runner, input json.RawMessage) (json.RawMessage, error) {
		var req ExecRunRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		if req.RunInBackground {
			resp, err := r.ExecBashBackground(ctx, req)
			if err != nil {
				return nil, err
			}
			return mustJSON(resp)
		}
		resp, err := r.ExecRun(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)
	},
}

func (r *Runner) callDispatch(ctx context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	fn, ok := toolDispatchTable[name]
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
	return fn(ctx, r, input)
}
