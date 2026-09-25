package ckg

import (
	"context"
	"math"
	"testing"
)

// The semantic index (DATA-5): one in-memory matrix per model instead of a
// decode of every vector per query, rebuilt when a vector or a node changes.

func TestSearchSimilar_MatrixIsReusedAndFollowsChanges(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	idA, idR := seedNodes(t, s)
	if err := s.SaveEmbeddings(ctx, "m", []EmbeddingItem{
		{NodeID: idA, Vector: []float32{1, 0, 0}},
		{NodeID: idR, Vector: []float32{0, 1, 0}},
	}); err != nil {
		t.Fatal(err)
	}
	hits, err := s.SearchSimilar(ctx, "m", []float32{0, 1, 0}, 5)
	if err != nil || len(hits) != 2 || hits[0].Node.ID != idR {
		t.Fatalf("hits=%+v err=%v", hits, err)
	}
	if _, err := s.SearchSimilar(ctx, "m", []float32{1, 0, 0}, 5); err != nil {
		t.Fatal(err)
	}
	if n := s.embedLoads.Load(); n != 1 {
		t.Fatalf("two queries, one matrix: loads=%d", n)
	}

	// A new vector: the next query sees it.
	if err := s.SaveFileNodes(ctx, "b.go", "h", "go", "pkg", "pkg",
		[]Node{{FQN: "pkg.Beta", ShortName: "Beta", Kind: "func", LineStart: 1, LineEnd: 2}}, nil); err != nil {
		t.Fatal(err)
	}
	var idB int64
	if err := s.db.QueryRow(`SELECT id FROM nodes WHERE fqn = 'pkg.Beta'`).Scan(&idB); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveEmbeddings(ctx, "m", []EmbeddingItem{{NodeID: idB, Vector: []float32{0, 0, 1}}}); err != nil {
		t.Fatal(err)
	}
	hits, err = s.SearchSimilar(ctx, "m", []float32{0, 0, 1}, 1)
	if err != nil || len(hits) != 1 || hits[0].Node.ID != idB {
		t.Fatalf("after a save: hits=%+v err=%v", hits, err)
	}
	if n := s.embedLoads.Load(); n != 2 {
		t.Fatalf("a save rebuilds the matrix once: loads=%d", n)
	}

	// A file's nodes go, and their vectors with them.
	if err := s.DeleteFile(ctx, "agent.go"); err != nil {
		t.Fatal(err)
	}
	hits, err = s.SearchSimilar(ctx, "m", []float32{1, 0, 0}, 5)
	if err != nil || len(hits) != 1 || hits[0].Node.ID != idB {
		t.Fatalf("after a delete: hits=%+v err=%v", hits, err)
	}
	if n := s.embedLoads.Load(); n != 3 {
		t.Fatalf("a delete rebuilds the matrix: loads=%d", n)
	}
}

// The matrix answers what the streaming scan answers, in the same order.
func TestSearchSimilar_MatrixMatchesTheStreamingAnswer(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	var nodes []Node
	for i := 0; i < 7; i++ {
		nodes = append(nodes, Node{FQN: "ex.F" + string(rune('A'+i)), ShortName: "F" + string(rune('A'+i)), Kind: "func", LineStart: i + 1, LineEnd: i + 1})
	}
	seedGraph(t, s, nodes, nil)
	rows, err := s.db.Query(`SELECT id FROM nodes WHERE kind = 'func' ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	var items []EmbeddingItem
	for i := 0; rows.Next(); i++ {
		var id int64
		_ = rows.Scan(&id)
		f := float32(i)
		items = append(items, EmbeddingItem{NodeID: id, Vector: []float32{f, 7 - f, f * f, 1}})
	}
	rows.Close()
	if err := s.SaveEmbeddings(ctx, "m", items); err != nil {
		t.Fatal(err)
	}
	q := []float32{2, 3, 1, 0.5}
	fast, err := s.SearchSimilar(ctx, "m", q, 4)
	if err != nil {
		t.Fatal(err)
	}
	slow, err := s.searchSimilarStreaming(ctx, "m", q, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(fast) != 4 || len(slow) != 4 {
		t.Fatalf("fast=%d slow=%d", len(fast), len(slow))
	}
	for i := range fast {
		if fast[i].Node.ID != slow[i].Node.ID || math.Abs(float64(fast[i].Score-slow[i].Score)) > 1e-5 {
			t.Fatalf("rank %d: matrix %+v vs streaming %+v", i, fast[i], slow[i])
		}
	}
}

func TestCachedEmbeddings_ByModelAndHash(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	idA, idR := seedNodes(t, s)
	if err := s.SaveEmbeddings(ctx, "m", []EmbeddingItem{
		{NodeID: idA, Vector: []float32{1, 0, 0}, ContentHash: "h1"},
		{NodeID: idR, Vector: []float32{0, 1, 0}}, // no hash: not cached
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.CachedEmbeddings(ctx, "m", []string{"h1", "h2"})
	if err != nil || len(got) != 1 || got["h1"][0] != 1 {
		t.Fatalf("got=%v err=%v", got, err)
	}
	if other, _ := s.CachedEmbeddings(ctx, "other-model", []string{"h1"}); len(other) != 0 {
		t.Fatalf("the cache is per model: %v", other)
	}
	// Clearing the model's node vectors keeps the content cache.
	if err := s.ClearEmbeddings(ctx, "m"); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.CountEmbeddings(ctx, "m"); n != 0 {
		t.Fatalf("cleared: %d", n)
	}
	if got, _ = s.CachedEmbeddings(ctx, "m", []string{"h1"}); len(got) != 1 {
		t.Fatalf("the content cache survives a clear: %v", got)
	}
}
