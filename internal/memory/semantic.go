package memory

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/orchestra/orchestra/internal/lessons"
)

// SearchHit is one memory_search result.
type SearchHit struct {
	Layer   string
	Snippet string
	Score   float32
}

type searchChunk struct {
	layer string
	text  string
}

// Embedder performs vector embedding for semantic memory search (implemented by internal/embed).
type Embedder interface {
	Embed(ctx context.Context, inputs []string) ([][]float32, error)
	Model() string
}

// SemanticSearch ranks memory chunks by embedding similarity when an embed client is configured.
// Returns nil when embedding is unavailable or fails (caller should fall back to substring search).
func SemanticSearch(ctx context.Context, store *Store, root, query string, limit int, emb Embedder) ([]SearchHit, error) {
	if store == nil || strings.TrimSpace(query) == "" || emb == nil || strings.TrimSpace(emb.Model()) == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 8
	}
	chunks := collectSearchChunks(store, root)
	if len(chunks) == 0 {
		return nil, nil
	}
	if len(chunks) > 48 {
		chunks = chunks[len(chunks)-48:]
	}
	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.text
	}
	client := emb
	allInputs := append([]string{query}, texts...)
	vecs, err := client.Embed(ctx, allInputs)
	if err != nil || len(vecs) != len(allInputs) {
		return nil, err
	}
	q := vecs[0]
	qMag := vectorMag32(q)
	type scored struct {
		idx   int
		score float32
	}
	scores := make([]scored, 0, len(chunks))
	for i, doc := range vecs[1:] {
		if len(doc) == 0 {
			continue
		}
		scores = append(scores, scored{idx: i, score: cosine32(q, doc, qMag)})
	}
	sort.Slice(scores, func(i, j int) bool { return scores[i].score > scores[j].score })
	var out []SearchHit
	for _, s := range scores {
		if len(out) >= limit {
			break
		}
		if s.score < 0.15 {
			continue
		}
		snip := chunks[s.idx].text
		if len(snip) > 400 {
			snip = snip[:400] + "…"
		}
		out = append(out, SearchHit{Layer: chunks[s.idx].layer, Snippet: snip, Score: s.score})
	}
	return out, nil
}

func collectSearchChunks(store *Store, root string) []searchChunk {
	var out []searchChunk
	appendEntries := func(layer, content string) {
		for _, e := range splitEntries(content) {
			if t := strings.TrimSpace(e); t != "" {
				out = append(out, searchChunk{layer: layer, text: t})
			}
		}
	}
	if res := store.Read(layerRepo, "", 256*1024); res.Content != "" {
		appendEntries("repo", res.Content)
	}
	if res := store.Read(layerSession, "", 128*1024); res.Content != "" {
		appendEntries("session", res.Content)
	}
	if res := store.Read(layerGlobal, "", 64*1024); res.Content != "" {
		appendEntries("global", res.Content)
	}
	if res := store.Read(layerOrchestra, "", 64*1024); res.Content != "" {
		if t := strings.TrimSpace(res.Content); t != "" {
			out = append(out, searchChunk{layer: "orchestra", text: t})
		}
	}
	if res := store.Read(layerLessons, "", 256*1024); res.Content != "" {
		for _, block := range strings.Split(res.Content, "\n## ") {
			block = strings.TrimSpace(block)
			if block != "" {
				out = append(out, searchChunk{layer: "lessons", text: block})
			}
		}
	}
	dir := filepath.Join(root, filepath.FromSlash(lessons.RelDir))
	if entries, err := os.ReadDir(dir); err == nil {
		for _, ent := range entries {
			if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".md") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, ent.Name()))
			if err != nil {
				continue
			}
			dept := strings.TrimSuffix(ent.Name(), ".md")
			for _, block := range strings.Split(string(data), "\n## ") {
				block = strings.TrimSpace(block)
				if block == "" {
					continue
				}
				out = append(out, searchChunk{layer: "lessons", text: "[" + dept + "] " + block})
			}
		}
	}
	return out
}

func vectorMag32(v []float32) float64 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	return math.Sqrt(sum)
}

func cosine32(query, doc []float32, queryMag float64) float32 {
	if queryMag == 0 || len(query) == 0 || len(doc) == 0 {
		return 0
	}
	n := len(query)
	if len(doc) < n {
		n = len(doc)
	}
	var dot float64
	var docMag float64
	for i := 0; i < n; i++ {
		dot += float64(query[i]) * float64(doc[i])
		docMag += float64(doc[i]) * float64(doc[i])
	}
	if docMag == 0 {
		return 0
	}
	return float32(dot / (queryMag * math.Sqrt(docMag)))
}
