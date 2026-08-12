package tools

import (
	"context"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/lsp"
	"github.com/orchestra/orchestra/internal/tools/nav"
)

func (r *Runner) snapshotCKG() (nav.CKGAccess, func()) {
	r.ckgMu.RLock()
	return nav.CKGAccess{Store: r.ckgStore, Provider: r.ckgProvider}, r.ckgMu.RUnlock
}

func (r *Runner) navClient() *nav.Client {
	if r == nil {
		return nil
	}
	return nav.NewClient(
		r.workspaceRoot,
		r.excludeDirs,
		r.embedCfg,
		r.snapshotCKG,
		func() *lsp.Manager { return r.lspManager },
	)
}

func (r *Runner) ExploreCodebase(ctx context.Context, req ExploreCodebaseRequest) (*ExploreCodebaseResponse, error) {
	return r.navClient().ExploreCodebase(ctx, req)
}

func (r *Runner) CodeSymbols(ctx context.Context, req CodeSymbolsRequest) (*CodeSymbolsResponse, error) {
	return r.navClient().CodeSymbols(ctx, req)
}

func (r *Runner) SemanticSearch(ctx context.Context, req SemanticSearchRequest) (*SemanticSearchResponse, error) {
	return r.navClient().SemanticSearch(ctx, req)
}

func (r *Runner) RepoMap(ctx context.Context, req RepoMapRequest) (*RepoMapResponse, error) {
	return r.navClient().RepoMap(ctx, req)
}

func (r *Runner) CKGIndexStatus(ctx context.Context) (CKGIndexView, error) {
	return r.navClient().CKGIndexStatus(ctx)
}

func (r *Runner) RebuildCKG(ctx context.Context) error {
	return r.navClient().RebuildCKG(ctx)
}

func (r *Runner) RunCKGEmbed(ctx context.Context, rebuild bool, limit int) (*CKGEmbedResult, error) {
	return r.navClient().RunCKGEmbed(ctx, rebuild, limit)
}

func (r *Runner) SetIndexRuntime(excludeDirs []string, embedCfg config.EmbedConfig) {
	if r == nil {
		return
	}
	r.ckgMu.Lock()
	defer r.ckgMu.Unlock()
	r.excludeDirs = append([]string(nil), excludeDirs...)
	r.embedCfg = embedCfg
}

func (r *Runner) SeedCKGSymbolForTest(ctx context.Context, relPath, fileHash, symbol string, lineStart, lineEnd int) error {
	return r.navClient().SeedCKGSymbolForTest(ctx, relPath, fileHash, symbol, lineStart, lineEnd)
}

func (r *Runner) FetchCKGContext(ctx context.Context, query string) string {
	return r.navClient().FetchCKGContext(ctx, query)
}
