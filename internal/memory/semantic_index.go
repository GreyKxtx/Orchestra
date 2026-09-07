package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// semanticScoreFloor is the similarity below which a chunk is not a match.
// Same value the unindexed search used; kept as a name so both paths move
// together if it is ever retuned.
const semanticScoreFloor = 0.15

func trimmed(s string) string { return strings.TrimSpace(s) }

func errVectorCount(got, want int) error {
	return fmt.Errorf("embedding server returned %d vectors for %d inputs", got, want)
}

// VectorStore caches chunk embeddings between searches. Implemented by the CKG
// store; nil is a valid value and means "no cache", which is what a workspace
// without a CKG database gets.
//
// The interface lives here rather than memory importing internal/ckg: memory
// is the lower layer, and it needs a cache, not a graph.
type VectorStore interface {
	// Load returns the vectors known for these hashes under model. Hashes with
	// no vector are absent — that absence is the work list.
	Load(ctx context.Context, model string, hashes []string) (map[string][]float32, error)
	Save(ctx context.Context, model string, vecs []ScopedVector) error
	// Prune drops this model's vectors inside scopes whose hash is not in keep.
	// Scoping matters: a search sees the workspace's durable layers and its own
	// session's, never another session's, so an unscoped sweep would evict the
	// other sessions' vectors and both would thrash re-embedding.
	Prune(ctx context.Context, model string, scopes []string, keep []string) error
}

// ScopedVector is one chunk's vector together with the scope that owns it.
type ScopedVector struct {
	Hash   string
	Scope  string
	Vector []float32
}

// scopeWorkspace covers every layer shared by the whole workspace. The session
// layer is the only per-caller one, so it is the only thing that needs its own
// scope.
const scopeWorkspace = "ws"

func chunkScope(layer, sessionID string) string {
	if layer == layerSession {
		return "session:" + sessionID
	}
	return scopeWorkspace
}

// embedBatch caps how many chunks go into one Embed call. Only ever paid on
// chunks that are new or edited, so after the first pass it is rarely reached.
const embedBatch = 64

// chunkHash keys a chunk's vector by its exact content, layer included: the
// same sentence stored as a lesson and as a repo fact are different chunks,
// and an edited entry is simply a new key. Nothing has to detect an edit.
func chunkHash(layer, text string) string {
	sum := sha256.Sum256([]byte(layer + "\x00" + text))
	return hex.EncodeToString(sum[:])
}

// SemanticSearchIndexed ranks every memory chunk by embedding similarity,
// embedding only the chunks whose vectors are not already cached.
//
// The unindexed predecessor re-embedded up to 48 chunks on every single
// search and silently ignored everything older than that, so an old fact
// could not be found however well it matched, and a repeated query cost the
// same as the first one. With a cache there is no reason to do either: the
// window is gone, and a second search pays for the query alone.
func SemanticSearchIndexed(ctx context.Context, store *Store, root, query string, limit int, emb Embedder, vecStore VectorStore) ([]SearchHit, error) {
	if store == nil || emb == nil || trimmed(query) == "" || trimmed(emb.Model()) == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 8
	}
	chunks := collectSearchChunks(store, root)
	if len(chunks) == 0 {
		return nil, nil
	}

	model := emb.Model()
	hashes := make([]string, len(chunks))
	scopes := make([]string, len(chunks))
	scopeSet := map[string]bool{scopeWorkspace: true}
	for i, c := range chunks {
		hashes[i] = chunkHash(c.layer, c.text)
		scopes[i] = chunkScope(c.layer, store.sessionID)
		scopeSet[scopes[i]] = true
	}
	prunable := make([]string, 0, len(scopeSet))
	for sc := range scopeSet {
		prunable = append(prunable, sc)
	}
	sort.Strings(prunable)

	cached := map[string][]float32{}
	if vecStore != nil {
		var err error
		cached, err = vecStore.Load(ctx, model, hashes)
		if err != nil {
			return nil, err
		}
	}

	// Embed the query together with the first batch of misses so a cold cache
	// still costs one round trip, not two.
	missIdx := make([]int, 0, len(chunks))
	seen := map[string]bool{}
	for i, h := range hashes {
		if _, ok := cached[h]; ok || seen[h] {
			continue
		}
		seen[h] = true
		missIdx = append(missIdx, i)
	}

	queryVec, fresh, err := embedQueryAndMisses(ctx, emb, query, chunks, hashes, missIdx)
	if err != nil {
		return nil, err
	}
	if len(queryVec) == 0 {
		return nil, nil
	}
	if vecStore != nil && len(fresh) > 0 {
		scopeOf := make(map[string]string, len(hashes))
		for i, h := range hashes {
			scopeOf[h] = scopes[i]
		}
		items := make([]ScopedVector, 0, len(fresh))
		for h, v := range fresh {
			items = append(items, ScopedVector{Hash: h, Scope: scopeOf[h], Vector: v})
		}
		sort.Slice(items, func(i, j int) bool { return items[i].Hash < items[j].Hash })
		if err := vecStore.Save(ctx, model, items); err != nil {
			return nil, err
		}
	}
	for h, v := range fresh {
		cached[h] = v
	}
	if vecStore != nil {
		// Entries get edited and deleted; their vectors would otherwise pile up
		// forever under hashes no chunk claims any more.
		if err := vecStore.Prune(ctx, model, prunable, hashes); err != nil {
			return nil, err
		}
	}

	qMag := vectorMag32(queryVec)
	type scored struct {
		idx   int
		score float32
	}
	scores := make([]scored, 0, len(chunks))
	for i, h := range hashes {
		doc := cached[h]
		if len(doc) == 0 {
			continue
		}
		scores = append(scores, scored{idx: i, score: cosine32(queryVec, doc, qMag)})
	}
	sort.Slice(scores, func(i, j int) bool { return scores[i].score > scores[j].score })

	var out []SearchHit
	for _, s := range scores {
		if len(out) >= limit {
			break
		}
		if s.score < semanticScoreFloor {
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

// embedQueryAndMisses returns the query vector and the vectors for the chunks
// at missIdx, keyed by hash. A partial answer from the embedding server is an
// error rather than a silent gap: memory_search promises semantic ranking once
// embed.model is set, and half a ranking is indistinguishable from a bad one.
func embedQueryAndMisses(ctx context.Context, emb Embedder, query string, chunks []searchChunk, hashes []string, missIdx []int) ([]float32, map[string][]float32, error) {
	fresh := map[string][]float32{}

	first := missIdx
	if len(first) > embedBatch {
		first = first[:embedBatch]
	}
	inputs := make([]string, 0, len(first)+1)
	inputs = append(inputs, query)
	for _, i := range first {
		inputs = append(inputs, chunks[i].text)
	}
	vecs, err := emb.Embed(ctx, inputs)
	if err != nil {
		return nil, nil, err
	}
	if len(vecs) != len(inputs) {
		return nil, nil, errVectorCount(len(vecs), len(inputs))
	}
	queryVec := vecs[0]
	for n, i := range first {
		fresh[hashes[i]] = vecs[n+1]
	}

	for start := len(first); start < len(missIdx); start += embedBatch {
		end := start + embedBatch
		if end > len(missIdx) {
			end = len(missIdx)
		}
		part := missIdx[start:end]
		texts := make([]string, len(part))
		for n, i := range part {
			texts[n] = chunks[i].text
		}
		batchVecs, err := emb.Embed(ctx, texts)
		if err != nil {
			return nil, nil, err
		}
		if len(batchVecs) != len(texts) {
			return nil, nil, errVectorCount(len(batchVecs), len(texts))
		}
		for n, i := range part {
			fresh[hashes[i]] = batchVecs[n]
		}
	}
	return queryVec, fresh, nil
}
