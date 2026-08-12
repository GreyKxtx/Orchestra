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

	client := embed.New(c.EmbedCfg)
	model := client.Model()

	if rebuild {
		if _, err := snap.Store.DB().ExecContext(ctx, `DELETE FROM node_embeddings WHERE model = ?`, model); err != nil {
			return nil, fmt.Errorf("rebuild: clear embeddings: %w", err)
		}
	}

	pending, err := snap.Store.MissingEmbeddings(ctx, model, limit)
	if err != nil {
		return nil, fmt.Errorf("list missing: %w", err)
	}
	if len(pending) == 0 {
		total, _ := snap.Store.CountEmbeddings(ctx, model)
		return &CKGEmbedResult{Model: model, Total: total, Remaining: 0, Elapsed: "0s"}, nil
	}

	start := time.Now()
	batch := c.EmbedCfg.BatchSize
	if batch <= 0 {
		batch = 32
	}
	embedded := 0

	for i := 0; i < len(pending); i += batch {
		end := i + batch
		if end > len(pending) {
			end = len(pending)
		}
		chunk := pending[i:end]
		inputs := make([]string, 0, len(chunk))
		valid := make([]ckg.MissingEmbedding, 0, len(chunk))
		for _, m := range chunk {
			text, err := readNodeSourceForEmbed(c.Root, m)
			if err != nil {
				continue
			}
			if strings.TrimSpace(text) == "" {
				continue
			}
			inputs = append(inputs, text)
			valid = append(valid, m)
		}
		if len(inputs) == 0 {
			continue
		}
		vecs, err := client.Embed(ctx, inputs)
		if err != nil {
			return nil, fmt.Errorf("embed batch [%d:%d]: %w", i, end, err)
		}
		items := make([]ckg.EmbeddingItem, len(valid))
		for j, m := range valid {
			items[j] = ckg.EmbeddingItem{NodeID: m.NodeID, Vector: vecs[j]}
		}
		if err := snap.Store.SaveEmbeddings(ctx, model, items); err != nil {
			return nil, fmt.Errorf("save batch [%d:%d]: %w", i, end, err)
		}
		embedded += len(valid)
	}

	total, _ := snap.Store.CountEmbeddings(ctx, model)
	missing, _ := snap.Store.MissingEmbeddings(ctx, model, 0)
	return &CKGEmbedResult{
		Model:     model,
		Embedded:  embedded,
		Total:     total,
		Remaining: len(missing),
		Elapsed:   time.Since(start).Round(time.Millisecond).String(),
	}, nil
}

func readNodeSourceForEmbed(projectRoot string, m ckg.MissingEmbedding) (string, error) {
	full := filepath.Join(projectRoot, m.Path)
	data, err := os.ReadFile(full)
	if err != nil {
		return "", err
	}
	if m.LineStart <= 0 || m.LineEnd < m.LineStart {
		return fmt.Sprintf("%s (%s)\n", m.FQN, m.Path), nil
	}
	lines := strings.Split(string(data), "\n")
	start := m.LineStart - 1
	end := m.LineEnd
	if start >= len(lines) {
		return fmt.Sprintf("%s (%s)\n", m.FQN, m.Path), nil
	}
	if end > len(lines) {
		end = len(lines)
	}
	body := strings.Join(lines[start:end], "\n")
	return fmt.Sprintf("// %s\n%s\n", m.FQN, body), nil
}
