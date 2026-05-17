package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/orchestra/orchestra/internal/embed"
	"github.com/orchestra/orchestra/internal/protocol"
)

// SemanticSearchRequest is the input for the semantic_search tool.
type SemanticSearchRequest struct {
	Query   string `json:"query"`
	TopK    int    `json:"top_k,omitempty"`
	Snippet bool   `json:"snippet,omitempty"`
}

// SemanticSearchHit is a single search result.
type SemanticSearchHit struct {
	FQN       string  `json:"fqn"`
	Kind      string  `json:"kind"`
	Path      string  `json:"path"`
	LineStart int     `json:"line_start"`
	LineEnd   int     `json:"line_end"`
	Score     float32 `json:"score"`
	Snippet   string  `json:"snippet,omitempty"`
}

// SemanticSearchResponse is the tool response.
type SemanticSearchResponse struct {
	Model string              `json:"model"`
	Hits  []SemanticSearchHit `json:"hits"`
}

// SemanticSearch embeds the query and returns top-K nearest CKG nodes
// by cosine similarity. Requires embed.model configured and an indexed
// CKG store (orchestra ckg embed must have been run).
func (r *Runner) SemanticSearch(ctx context.Context, req SemanticSearchRequest) (*SemanticSearchResponse, error) {
	if r == nil {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "runner is nil", nil)
	}
	if strings.TrimSpace(req.Query) == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "query is empty", nil)
	}
	if r.embedCfg.Model == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "semantic_search disabled: embed.model not configured in .orchestra.yml", nil)
	}
	if r.ckgStore == nil {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "semantic_search disabled: no CKG store on this runner", nil)
	}
	topK := req.TopK
	if topK <= 0 {
		topK = 10
	}
	if topK > 50 {
		topK = 50
	}

	client := embed.New(r.embedCfg)
	vecs, err := client.Embed(ctx, []string{req.Query})
	if err != nil {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "embed query failed", map[string]any{"error": err.Error()})
	}
	if len(vecs) == 0 {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "embed returned no vectors", nil)
	}

	hits, err := r.ckgStore.SearchSimilar(ctx, client.Model(), vecs[0], topK)
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
			hit.Snippet = readSnippet(r.workspaceRoot, h.Path, h.Node.LineStart, h.Node.LineEnd, 40)
		}
		out.Hits = append(out.Hits, hit)
	}
	return out, nil
}

// readSnippet returns the lines [start..end] of <root>/<path>, truncated
// to at most maxLines lines from the start of the range.
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
