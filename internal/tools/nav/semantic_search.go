package nav

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/ckg"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/embed"
	"github.com/orchestra/orchestra/internal/embedindex"
	"github.com/orchestra/orchestra/protocol"
)

type SemanticSearchRequest struct {
	Query   string `json:"query"`
	TopK    int    `json:"top_k,omitempty"`
	Snippet bool   `json:"snippet,omitempty"`
}

type SemanticSearchHit struct {
	FQN       string  `json:"fqn"`
	Kind      string  `json:"kind"`
	Path      string  `json:"path"`
	LineStart int     `json:"line_start"`
	LineEnd   int     `json:"line_end"`
	Score     float32 `json:"score"`
	Snippet   string  `json:"snippet,omitempty"`
}

type SemanticExploreSummary struct {
	FQN     string  `json:"fqn"`
	Score   float32 `json:"score"`
	Summary string  `json:"summary"`
}

type SemanticSearchResponse struct {
	Model            string                   `json:"model"`
	Hits             []SemanticSearchHit      `json:"hits"`
	ExploreSummaries []SemanticExploreSummary `json:"explore_summaries,omitempty"`
	NextStep         string                   `json:"next_step,omitempty"`
}

func (c *Client) SemanticSearch(ctx context.Context, req SemanticSearchRequest) (*SemanticSearchResponse, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "nav client is nil", nil)
	}
	if strings.TrimSpace(req.Query) == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "query is empty", nil)
	}
	if c.EmbedCfg.Model == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "semantic_search disabled: embed.model not configured in .orchestra.yml", nil)
	}

	snap, unlock := c.ckgSnap()
	defer unlock()
	if snap.Store == nil {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "semantic_search disabled: no CKG store on this runner", nil)
	}

	topK := req.TopK
	if topK <= 0 {
		topK = 10
	}
	if topK > 50 {
		topK = 50
	}

	client := embed.New(c.EmbedCfg)
	vecs, err := client.Embed(ctx, []string{req.Query})
	if err != nil {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "embed query failed", map[string]any{"error": err.Error()})
	}
	if len(vecs) == 0 {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "embed returned no vectors", nil)
	}

	hits, err := snap.Store.SearchSimilar(ctx, client.Model(), vecs[0], topK)
	if err != nil {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "search similar failed", map[string]any{"error": err.Error()})
	}

	out := &SemanticSearchResponse{Model: client.Model(), Hits: make([]SemanticSearchHit, 0, len(hits))}
	for _, h := range hits {
		hit := SemanticSearchHit{
			FQN:       h.Node.FQN,
			Kind:      h.Node.Kind,
			Path:      h.Path,
			LineStart: h.Node.LineStart,
			LineEnd:   h.Node.LineEnd,
			Score:     h.Score,
		}
		if req.Snippet {
			hit.Snippet = readSnippet(c.Root, h.Path, h.Node.LineStart, h.Node.LineEnd, 40)
		}
		out.Hits = append(out.Hits, hit)
	}
	if len(out.Hits) == 0 {
		// Zero hits reads as "nothing matches your query". When the index was
		// simply never built it means the opposite — nothing has been indexed
		// yet — and embeddings are only ever written by an explicit command,
		// so without this the tool looks permanently broken.
		if n, cErr := snap.Store.CountEmbeddings(ctx, client.Model()); cErr == nil && n == 0 {
			out.NextStep = fmt.Sprintf(
				"no embeddings indexed for model %q — run `orchestra ckg embed` in this workspace, then retry. Until then use grep / explore / repo_map.",
				client.Model())
		}
	}
	if c.EmbedCfg.ResolvedSemanticAutoExplore() {
		c.enrichSemanticSearchWithExplore(ctx, out)
	}
	return out, nil
}

func (c *Client) enrichSemanticSearchWithExplore(ctx context.Context, out *SemanticSearchResponse) {
	if out == nil || len(out.Hits) == 0 {
		return
	}
	topN := c.EmbedCfg.ResolvedSemanticAutoExploreTopK()
	seen := map[string]bool{}
	for _, hit := range out.Hits {
		if len(out.ExploreSummaries) >= topN {
			break
		}
		fqn := strings.TrimSpace(hit.FQN)
		if fqn == "" || seen[fqn] || !semanticHitExplorable(hit) {
			continue
		}
		seen[fqn] = true
		exp, err := c.ExploreCodebase(ctx, ExploreCodebaseRequest{SymbolName: fqn})
		if err != nil || exp == nil || strings.TrimSpace(exp.Content) == "" {
			continue
		}
		out.ExploreSummaries = append(out.ExploreSummaries, SemanticExploreSummary{
			FQN:     fqn,
			Score:   hit.Score,
			Summary: compactSemanticExploreSummary(exp.Content, 1200),
		})
	}
	if len(out.ExploreSummaries) > 0 {
		out.NextStep = "Review explore_summaries; call explore(symbol_name) for full code before read/patch."
	}
}

func semanticHitExplorable(hit SemanticSearchHit) bool {
	k := strings.ToLower(strings.TrimSpace(hit.Kind))
	switch k {
	case "", "file", "directory", "module":
		return false
	}
	return strings.TrimSpace(hit.FQN) != ""
}

func compactSemanticExploreSummary(content string, maxRunes int) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	lines := strings.Split(content, "\n")
	var kept []string
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "```") {
			continue
		}
		kept = append(kept, ln)
		if len(kept) >= 12 {
			break
		}
	}
	out := strings.Join(kept, " | ")
	if len(out) > maxRunes {
		out = out[:maxRunes-3] + "..."
	}
	return out
}

func readSnippet(root, path string, start, end, maxLines int) string {
	data, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	if start <= 0 || start > len(lines) {
		return ""
	}
	from := start - 1
	to := end
	if to > len(lines) {
		to = len(lines)
	}
	if to-from > maxLines {
		to = from + maxLines
	}
	return fmt.Sprintf("%d–%d:\n%s", start, to, strings.Join(lines[from:to], "\n"))
}

// CKGIndexView is a snapshot of the code knowledge graph index for settings UI.
type CKGIndexView struct {
	Available bool   `json:"available"`
	DBPath    string `json:"db_path,omitempty"`
	ckg.IndexStats
}

func (c *Client) CKGIndexStatus(ctx context.Context) (CKGIndexView, error) {
	out := CKGIndexView{}
	if c == nil {
		return out, nil
	}
	snap, unlock := c.ckgSnap()
	defer unlock()
	if snap.Store == nil {
		return out, nil
	}
	out.Available = true
	out.DBPath = filepath.Join(c.Root, ".orchestra", "ckg.db")
	model := strings.TrimSpace(c.EmbedCfg.Model)
	stats, err := snap.Store.IndexStats(ctx, model)
	if err != nil {
		return out, err
	}
	out.IndexStats = stats
	return out, nil
}

func (c *Client) RebuildCKG(ctx context.Context) error {
	if c == nil {
		return fmt.Errorf("nav client is nil")
	}
	return c.withCKG(func(snap CKGAccess) error {
		if snap.Store == nil {
			return fmt.Errorf("ckg store unavailable")
		}
		orch := ckg.NewOrchestratorWithIgnores(snap.Store, c.Root, c.ExcludeDirs)
		return orch.UpdateGraph(ctx)
	})
}

// SetEmbedCfg updates embed config used by semantic search / CKG admin.
func (c *Client) SetEmbedCfg(cfg config.EmbedConfig) {
	if c != nil {
		c.EmbedCfg = cfg
	}
}

// SetExcludeDirs updates exclude list for repo_map scans.
func (c *Client) SetExcludeDirs(exclude []string) {
	if c != nil {
		c.ExcludeDirs = append([]string(nil), exclude...)
	}
}

type CKGEmbedResult struct {
	Model     string `json:"model"`
	Embedded  int    `json:"embedded"`
	Total     int    `json:"total"`
	Remaining int    `json:"remaining"`
	Elapsed   string `json:"elapsed"`
}

func (c *Client) RunCKGEmbed(ctx context.Context, rebuild bool, limit int) (*CKGEmbedResult, error) {
	if c == nil {
		return nil, fmt.Errorf("nav client is nil")
	}
	snap, unlock := c.ckgSnap()
	defer unlock()
	if snap.Store == nil {
		return nil, fmt.Errorf("ckg store unavailable")
	}
	if strings.TrimSpace(c.EmbedCfg.Model) == "" {
		return nil, fmt.Errorf("embed.model is empty in .orchestra.yml")
	}

	start := time.Now()
	res, err := embedindex.Run(ctx, embedindex.Options{
		ProjectRoot: c.Root,
		Store:       snap.Store,
		Embed:       c.EmbedCfg,
		Limit:       limit,
		Rebuild:     rebuild,
	})
	if err != nil {
		return nil, err
	}
	total, _ := snap.Store.CountEmbeddings(ctx, res.Model)
	missing, _ := snap.Store.MissingEmbeddings(ctx, res.Model, 0)
	return &CKGEmbedResult{
		Model:     res.Model,
		Embedded:  res.Indexed,
		Total:     total,
		Remaining: len(missing),
		Elapsed:   time.Since(start).Round(time.Millisecond).String(),
	}, nil
}
