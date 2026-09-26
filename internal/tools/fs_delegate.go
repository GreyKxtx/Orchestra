package tools

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/orchestra/orchestra/internal/lsp"
	"github.com/orchestra/orchestra/internal/tools/fs"
	"github.com/orchestra/orchestra/internal/wsview"
	"github.com/orchestra/orchestra/patch/ops"
	"github.com/orchestra/orchestra/patch/patches"
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
		GoFileRedirect:       r.goFileRedirectHook,
		DiscoverInstructions: r.discoverInstructions,
		SymbolLineRange:      r.symbolLineRangeHook,
		SymbolFQNAtLine:      r.symbolFQNAtLineHook,
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

// EffectiveContent is the language servers' view of relPath: the content
// staged for it in any live turn — the default turn first — else the disk.
// Two turns staging one file show the server the first one's draft; the
// server holds one version of each document either way.
func (r *Runner) EffectiveContent(relPath string) (string, bool) {
	for _, t := range r.liveTurns() {
		if content, ok := t.EffectiveContent(relPath); ok {
			return content, true
		}
	}
	return "", false
}

// ListStagedPaths is every path staged in any live turn, for the language
// servers: those documents stay open when the rest are evicted.
func (r *Runner) ListStagedPaths() []string {
	seen := map[string]struct{}{}
	var out []string
	for _, t := range r.liveTurns() {
		for _, p := range t.ListStagedPaths() {
			if _, dup := seen[p]; dup {
				continue
			}
			seen[p] = struct{}{}
			out = append(out, p)
		}
	}
	return out
}

// overlayAt is the overlay the agent behind ctx stages into: its task's layer
// (WithLayer), else the turn's.
func (r *Runner) overlayAt(ctx context.Context) *fs.Overlay {
	if l := fs.OverlayFrom(ctx); l != nil {
		return l
	}
	if r == nil || r.turn == nil {
		return nil
	}
	return r.turn.overlay
}

// StagedOps returns the ops staged by the agent behind ctx: a task's own, or
// the turn's.
func (r *Runner) StagedOps(ctx context.Context) []ops.AnyOp {
	return r.overlayAt(ctx).StagedOps()
}

// StagedFileContent is every staged file the agent behind ctx sees: for a task,
// what its owners staged with its own changes on top — not its siblings'.
func (r *Runner) StagedFileContent(ctx context.Context) map[string]string {
	return r.overlayAt(ctx).StagedFileContent()
}

func (r *Runner) ApplyPatchesToStaged(ctx context.Context, patchList []patches.Patch) error {
	o := r.overlayAt(ctx)
	if o == nil {
		return fmt.Errorf("fs overlay unavailable")
	}
	return o.ApplyPatchesToStaged(r.fsTools, patchList)
}

// ForkLayer gives a task its own layer over the overlay of the agent behind ctx
// (its owner). Nil outside a dry run: writes then go to disk.
func (r *Runner) ForkLayer(ctx context.Context) *fs.Overlay {
	return r.overlayAt(ctx).Fork()
}

// View is the project as the agent behind ctx sees it — its task's layer, or
// the turn's overlay, over the disk — for the runtime's checks.
func (r *Runner) View(ctx context.Context) wsview.View {
	if o := r.overlayAt(ctx); o != nil {
		return o
	}
	return wsview.Disk(r.WorkspaceRoot())
}

// InLayer reports whether ctx belongs to a task that writes into its own
// layer: its changes become its owner's only when it commits.
func InLayer(ctx context.Context) bool {
	return fs.OverlayFrom(ctx).IsLayer()
}

// LayerContext is a context carrying ctx's task layer and nothing else, for
// staging work done outside the call's own lifetime.
func LayerContext(ctx context.Context) context.Context {
	return fs.WithOverlay(context.Background(), fs.OverlayFrom(ctx))
}

// DropLayer discards a task's layer and puts the language server back on the
// owner's view of the files the task changed. The server holds one version of
// each document, the last one written, so a discarded edit would otherwise
// keep answering diagnostics and navigation for everyone after it.
func (r *Runner) DropLayer(ownerCtx context.Context, layer *fs.Overlay) {
	r.closeShadowOf(layer)
	paths := layer.Discard()
	if len(paths) == 0 || r.lspManager == nil || r.lspManager.IsEmpty() {
		return
	}
	owner := r.overlayAt(ownerCtx)
	ctx := context.Background()
	for _, p := range paths {
		if content, ok := owner.CurrentContent(p); ok {
			_ = r.lspManager.SyncStaged(ctx, p, content)
			continue
		}
		r.lspManager.DidClose(ctx, p)
	}
}

// WithLayer attributes ctx to a task layer: tools called with it read and
// write there.
func WithLayer(ctx context.Context, layer *fs.Overlay) context.Context {
	return fs.WithOverlay(ctx, layer)
}

// StagedSnapshot returns the default turn's staged files for a checkpoint.
func (r *Runner) StagedSnapshot() []fs.StagedSnapshot {
	return r.turn.StagedSnapshot()
}

// RestoreStaged stages a checkpoint's files in the default turn's overlay.
func (r *Runner) RestoreStaged(files []fs.StagedSnapshot) {
	r.turn.RestoreStaged(files)
}

// ClearStaged drops the default turn's staged edits (see Turn.ClearStaged).
func (r *Runner) ClearStaged() {
	r.turn.ClearStaged()
}

// UnstagePath removes one path from the default turn's overlay after a
// successful disk commit.
func (r *Runner) UnstagePath(path string) {
	rel := strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
	r.turn.UnstagePath(rel)
}

// HasStagedChanges reports whether the default turn has staged edits.
func (r *Runner) HasStagedChanges() bool {
	return r.turn.HasStagedChanges()
}

func (r *Runner) CommitStagedPath(ctx context.Context, path string, backup bool) (*FSApplyOpsResponse, error) {
	return r.fsClient().CommitStagedPath(ctx, path, backup)
}
