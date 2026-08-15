package session

import (
	"context"
	"testing"

	"github.com/orchestra/orchestra/internal/lessons"
)

func TestMemorySearch_LessonsLayer(t *testing.T) {
	dir := t.TempDir()
	if err := lessons.Append(dir, lessons.Entry{
		Dept: "engineering",
		Kind: lessons.KindAgentNote,
		Note: "always mock redis in unit tests",
	}); err != nil {
		t.Fatal(err)
	}
	c := testMemoryClient(t, dir)
	resp, err := c.MemorySearch(context.Background(), MemorySearchRequest{Query: "redis", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, h := range resp.Hits {
		if h.Layer == "lessons" {
			found = true
		}
	}
	if !found {
		t.Fatalf("hits = %+v", resp.Hits)
	}
}
