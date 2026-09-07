package session

import (
	"context"

	"github.com/orchestra/orchestra/internal/ckg"
	"github.com/orchestra/orchestra/internal/memory"
)

// ckgVectorStore adapts the CKG store to memory.VectorStore. The adapter lives
// here, not in internal/memory: memory is the lower layer and needs a cache,
// not a code graph, and this package already knows about both.
type ckgVectorStore struct{ store *ckg.Store }

func (c ckgVectorStore) Load(ctx context.Context, model string, hashes []string) (map[string][]float32, error) {
	return c.store.LoadMemoryEmbeddings(ctx, model, hashes)
}

func (c ckgVectorStore) Save(ctx context.Context, model string, vecs map[string][]float32) error {
	return c.store.SaveMemoryEmbeddings(ctx, model, vecs)
}

func (c ckgVectorStore) Prune(ctx context.Context, model string, keep []string) error {
	return c.store.PruneMemoryEmbeddings(ctx, model, keep)
}

// memoryVectorStore returns the vector cache for semantic memory search, or
// nil when there is no CKG database. Nil is a working configuration, not a
// failure: search still ranks, it just re-embeds each time.
func (c *Client) memoryVectorStore() memory.VectorStore {
	store := c.store()
	if store == nil {
		return nil
	}
	return ckgVectorStore{store: store}
}
