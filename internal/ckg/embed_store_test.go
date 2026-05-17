package ckg

import (
	"context"
	"testing"
)

func seedNodes(t *testing.T, s *Store) (idAgent, idRun int64) {
	t.Helper()
	nodes := []Node{
		{FQN: "pkg.Agent", ShortName: "Agent", Kind: "struct", LineStart: 1, LineEnd: 3},
		{FQN: "pkg.Agent.Run", ShortName: "Agent.Run", Kind: "method", LineStart: 5, LineEnd: 10},
	}
	if err := s.SaveFileNodes(context.Background(), "agent.go", "h1", "go", "pkg", "pkg", nodes, nil); err != nil {
		t.Fatalf("SaveFileNodes: %v", err)
	}
	// Re-read assigned IDs.
	rows, err := s.DB().Query("SELECT id, fqn FROM nodes ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var fqn string
		_ = rows.Scan(&id, &fqn)
		switch fqn {
		case "pkg.Agent":
			idAgent = id
		case "pkg.Agent.Run":
			idRun = id
		}
	}
	return
}

func TestSaveEmbeddings_AndCount(t *testing.T) {
	s := newTestStore(t)
	idA, idR := seedNodes(t, s)
	err := s.SaveEmbeddings(context.Background(), "test-model", []EmbeddingItem{
		{NodeID: idA, Vector: []float32{1, 0, 0}},
		{NodeID: idR, Vector: []float32{0, 1, 0}},
	})
	if err != nil {
		t.Fatalf("SaveEmbeddings: %v", err)
	}
	n, _ := s.CountEmbeddings(context.Background(), "test-model")
	if n != 2 {
		t.Errorf("count: %d", n)
	}
	// Re-save (upsert) with different vector.
	err = s.SaveEmbeddings(context.Background(), "test-model", []EmbeddingItem{
		{NodeID: idA, Vector: []float32{0, 0, 1}},
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	n, _ = s.CountEmbeddings(context.Background(), "test-model")
	if n != 2 {
		t.Errorf("count after upsert: %d", n)
	}
}

func TestSaveEmbeddings_EmptyModel(t *testing.T) {
	s := newTestStore(t)
	err := s.SaveEmbeddings(context.Background(), "", []EmbeddingItem{{NodeID: 1, Vector: []float32{1}}})
	if err == nil {
		t.Fatal("expected model-empty error")
	}
}

func TestSaveEmbeddings_SkipsMismatchedDim(t *testing.T) {
	s := newTestStore(t)
	idA, idR := seedNodes(t, s)
	err := s.SaveEmbeddings(context.Background(), "m", []EmbeddingItem{
		{NodeID: idA, Vector: []float32{1, 2, 3}},   // sets dim=3
		{NodeID: idR, Vector: []float32{1, 2, 3, 4}}, // skipped
	})
	if err != nil {
		t.Fatal(err)
	}
	n, _ := s.CountEmbeddings(context.Background(), "m")
	if n != 1 {
		t.Errorf("count: %d (mismatched should have been skipped)", n)
	}
}

func TestSearchSimilar_RanksByCosine(t *testing.T) {
	s := newTestStore(t)
	idA, idR := seedNodes(t, s)
	_ = s.SaveEmbeddings(context.Background(), "m", []EmbeddingItem{
		{NodeID: idA, Vector: []float32{1, 0, 0}},
		{NodeID: idR, Vector: []float32{0, 1, 0}},
	})
	hits, err := s.SearchSimilar(context.Background(), "m", []float32{1, 0, 0}, 5)
	if err != nil {
		t.Fatalf("SearchSimilar: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("hits: %d", len(hits))
	}
	if hits[0].Node.FQN != "pkg.Agent" {
		t.Errorf("top hit: %q (want pkg.Agent)", hits[0].Node.FQN)
	}
	if hits[0].Score < hits[1].Score {
		t.Errorf("scores not descending: %v %v", hits[0].Score, hits[1].Score)
	}
}

func TestSearchSimilar_TopKLimit(t *testing.T) {
	s := newTestStore(t)
	idA, idR := seedNodes(t, s)
	_ = s.SaveEmbeddings(context.Background(), "m", []EmbeddingItem{
		{NodeID: idA, Vector: []float32{1, 0}},
		{NodeID: idR, Vector: []float32{0, 1}},
	})
	hits, _ := s.SearchSimilar(context.Background(), "m", []float32{1, 1}, 1)
	if len(hits) != 1 {
		t.Errorf("top-1 returned %d", len(hits))
	}
}

func TestSearchSimilar_DimMismatchSkipped(t *testing.T) {
	s := newTestStore(t)
	idA, _ := seedNodes(t, s)
	_ = s.SaveEmbeddings(context.Background(), "m", []EmbeddingItem{
		{NodeID: idA, Vector: []float32{1, 0, 0}},
	})
	hits, err := s.SearchSimilar(context.Background(), "m", []float32{1, 0}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Errorf("expected zero hits on dim mismatch, got %d", len(hits))
	}
}

func TestMissingEmbeddings(t *testing.T) {
	s := newTestStore(t)
	idA, idR := seedNodes(t, s)
	missing, err := s.MissingEmbeddings(context.Background(), "m", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 2 {
		t.Errorf("initial missing: %d", len(missing))
	}
	_ = s.SaveEmbeddings(context.Background(), "m", []EmbeddingItem{
		{NodeID: idA, Vector: []float32{1, 0}},
	})
	missing, _ = s.MissingEmbeddings(context.Background(), "m", 0)
	if len(missing) != 1 || missing[0].NodeID != idR {
		t.Errorf("after partial save: %v", missing)
	}
}

func TestPackUnpackVector(t *testing.T) {
	in := []float32{0, 0.5, -1.25, 1e6}
	b := PackVector(in)
	out := UnpackVector(b, len(in))
	if len(out) != len(in) {
		t.Fatalf("dim: %d", len(out))
	}
	for i := range in {
		if out[i] != in[i] {
			t.Errorf("[%d] got %v want %v", i, out[i], in[i])
		}
	}
	if UnpackVector(b, 1) != nil {
		t.Error("expected nil on wrong dim")
	}
}
