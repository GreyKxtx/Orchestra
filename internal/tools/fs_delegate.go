package tools

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/orchestra/orchestra/internal/lsp"
	"github.com/orchestra/orchestra/internal/tools/fs"
	"github.com/orchestra/orchestra/patch/patches"
	"github.com/orchestra/orchestra/patch/ops"
)

func (r *Runner) initFSClient(root string, exclude []string, dryRun, astGate bool) {
	overlay := fs.NewOverlay(root, fs.OverlayOptions{
		DryRun:  dryRun,
		ASTGate: astGate,
	})
	r.fsTools = fs.NewClient(root, exclude, overlay)
	r.wireFSHooks()
}

func (r *Runner) wireFSHooks() {
	if r == nil || r.fsTools == nil {
		return
	}
	r.fsTools.Hooks = fs.Hooks{
		OnStageSync: func(relSlash, content string) {
			if r.lspManager != nil && !r.lspManager.IsEmpty() {
				if err := r.lspManager.SyncStaged(context.Background(), relSlash, content); err != nil {
					fmt.Fprintf(os.Stderr, "tools: staging LSP sync %s: %v\n", relSlash, err)
				}
			}
		},
		Diagnose: func(ctx context.Context, relSlash, content string) ([]lsp.ToolDiagnostic, bool) {
			if r.lspManager == nil || r.lspManager.IsEmpty() {
				return nil, false
			}
			diags := r.lspManager.SyncAndDiagnose(ctx, relSlash, content)
			return diags, r.lspManager.EnsurePendingFor(relSlash)
		},
		ExtraDiagnostics: func(content string) []lsp.ToolDiagnostic {
			return r.extraTestDiagnostics(content)
		},
		GoFileRedirect:   r.goFileRedirectHook,
		DiscoverInstructions: r.discoverInstructions,
		SymbolLineRange:  r.symbolLineRangeHook,
		SymbolFQNAtLine:  r.symbolFQNAtLineHook,
		OnDidClose: func(ctx context.Context, relSlash string) {
			if r.lspManager != nil {
				r.lspManager.DidClose(ctx, relSlash)
			}
		},
	}
}

func (r *Runner) goFileRedirectHook(ctx context.Context, relSlash, hash string) string {
	if r == nil || r.ckgStore == nil {
		return ""
	}
	syms, err := r.ckgStore.SymbolsInFile(ctx, relSlash)
	if err != nil || len(syms) == 0 {
		return ""
	}
	out := make([]fs.GoSymbol, len(syms))
	for i, s := range syms {
		out[i] = fs.GoSymbol{
			ShortName: s.ShortName,
			Kind:      s.Kind,
			LineStart: s.LineStart,
			LineEnd:   s.LineEnd,
		}
	}
	return fs.FormatGoFileRedirect(relSlash, hash, out)
}

func (r *Runner) symbolLineRangeHook(ctx context.Context, relPath, symbol string) (start, end int, ok bool) {
	if r == nil || r.ckgStore == nil {
		return 0, 0, false
	}
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return 0, 0, false
	}
	syms, err := r.ckgStore.SymbolsInFile(ctx, relPath)
	if err != nil {
		return 0, 0, false
	}
	want := strings.ToLower(symbol)
	for _, sym := range syms {
		name := strings.ToLower(sym.ShortName)
		if name == want || strings.HasSuffix(name, "."+want) {
			return sym.LineStart, sym.LineEnd, true
		}
	}
	return 0, 0, false
}

func (r *Runner) symbolFQNAtLineHook(ctx context.Context, relPath string, line int) string {
	if r == nil || r.ckgStore == nil {
		return ""
	}
	fqn, err := r.ckgStore.FQNAtLine(ctx, relPath, line)
	if err != nil {
		return ""
	}
	return fqn
}

func (r *Runner) fsClient() *fs.Client {
	return r.fsTools
}

func (r *Runner) FSList(ctx context.Context, req FSListRequest) (*FSListResponse, error) {
	return r.fsClient().List(ctx, req)
}

func (r *Runner) FSRead(ctx context.Context, req FSReadRequest) (*FSReadResponse, error) {
	return r.fsClient().Read(ctx, req)
}

func (r *Runner) FSGlob(ctx context.Context, req FSGlobRequest) (*FSGlobResponse, error) {
	return r.fsClient().Glob(ctx, req)
}

func (r *Runner) FSWrite(ctx context.Context, req FSWriteRequest) (*FSWriteResponse, error) {
	return r.fsClient().Write(ctx, req)
}

func (r *Runner) FSEdit(ctx context.Context, req FSEditRequest) (*FSEditResponse, error) {
	return r.fsClient().Edit(ctx, req)
}

func (r *Runner) FSApplyOps(ctx context.Context, req FSApplyOpsRequest) (*FSApplyOpsResponse, error) {
	return r.fsClient().ApplyOps(ctx, req)
}

func (r *Runner) SearchText(ctx context.Context, req SearchTextRequest) (*SearchTextResponse, error) {
	return r.fsClient().SearchText(ctx, req)
}

func formatSearchResults(query string, resp *SearchTextResponse) string {
	return fs.FormatSearchResults(query, resp)
}

func (r *Runner) FSDelete(ctx context.Context, req FSDeleteRequest) (*FSDeleteResponse, error) {
	return r.fsClient().Delete(ctx, req)
}

func (r *Runner) FSRename(ctx context.Context, req FSRenameRequest) (*FSRenameResponse, error) {
	return r.fsClient().Rename(ctx, req)
}

func (r *Runner) FSPreview(ctx context.Context, req FSPreviewRequest) (*FSPreviewResponse, error) {
	return r.fsClient().Preview(ctx, req)
}

func (r *Runner) ASTRename(ctx context.Context, req ASTRenameRequest) (*ASTRenameResponse, error) {
	return r.fsClient().ASTRename(ctx, req)
}

func (r *Runner) EffectiveContent(relPath string) (string, bool) {
	if r.fsTools == nil || r.fsTools.Overlay == nil {
		return "", false
	}
	return r.fsTools.Overlay.EffectiveContent(relPath)
}

func (r *Runner) ListStagedPaths() []string {
	if r.fsTools == nil || r.fsTools.Overlay == nil {
		return nil
	}
	return r.fsTools.Overlay.ListStagedPaths()
}

func (r *Runner) StagedOps() []ops.AnyOp {
	if r.fsTools == nil || r.fsTools.Overlay == nil {
		return nil
	}
	return r.fsTools.Overlay.StagedOps()
}

func (r *Runner) StagedFileContent() map[string]string {
	if r.fsTools == nil || r.fsTools.Overlay == nil {
		return nil
	}
	return r.fsTools.Overlay.StagedFileContent()
}

func (r *Runner) ApplyPatchesToStaged(patchList []patches.Patch) error {
	if r.fsTools == nil || r.fsTools.Overlay == nil {
		return fmt.Errorf("fs overlay unavailable")
	}
	return r.fsTools.Overlay.ApplyPatchesToStaged(r.fsTools, patchList)
}

func (r *Runner) ClearStaged() {
	if r.fsTools != nil && r.fsTools.Overlay != nil {
		r.fsTools.Overlay.ClearStaged()
	}
}

func (r *Runner) HasStagedChanges() bool {
	if r.fsTools == nil || r.fsTools.Overlay == nil {
		return false
	}
	return r.fsTools.Overlay.HasStagedChanges()
}

func (r *Runner) CommitStagedPath(ctx context.Context, path string, backup bool) (*FSApplyOpsResponse, error) {
	return r.fsClient().CommitStagedPath(ctx, path, backup)
}

func (r *Runner) stagedContent(relSlash string) (content, hash string, ok bool) {
	if r.fsTools == nil || r.fsTools.Overlay == nil {
		return "", "", false
	}
	return r.fsTools.Overlay.StagedContent(relSlash)
}

func (r *Runner) currentHash(relSlash string) string {
	if r.fsTools == nil || r.fsTools.Overlay == nil {
		return ""
	}
	return r.fsTools.Overlay.CurrentHash(relSlash)
}
