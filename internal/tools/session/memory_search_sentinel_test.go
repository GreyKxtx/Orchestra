package session

import (
	"context"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
)

// memory.Store.Read reports its failure modes as CONTENT rather than as
// errors: asked for the session layer with no active session, it answers with
// the literal string "no active session_id". memory_search fed that straight
// into its substring pass, so a model asking about sessions got back a hit that
// looked exactly like a remembered fact, attributed to a layer that does not
// exist yet.
func TestMemorySearch_DoesNotReturnTheEmptySessionSentinel(t *testing.T) {
	// newMemoryClient wires sessionID to "" — the state this is about.
	c, _ := newMemoryClient(t, config.EmbedConfig{})

	for _, q := range []string{"no active session_id", "active session", "session_id"} {
		res, err := c.MemorySearch(context.Background(), MemorySearchRequest{Query: q})
		if err != nil {
			t.Fatalf("query %q: %v", q, err)
		}
		for _, h := range res.Hits {
			if strings.Contains(h.Snippet, "no active session_id") {
				t.Errorf("query %q returned the store's own placeholder as a hit: %+v", q, h)
			}
		}
	}
}
