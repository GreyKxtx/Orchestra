package ckg

import (
	"context"
	"testing"
)

func TestIndexStats(t *testing.T) {
	ctx := context.Background()
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	st, err := s.IndexStats(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if st.Files != 0 || st.Nodes != 0 {
		t.Fatalf("empty store: %+v", st)
	}

	if err := s.SaveFileNodes(ctx, "a.go", "h1", "go", "ex", "pkg", []Node{{
		FQN: "ex.Foo", ShortName: "Foo", Kind: "func", LineStart: 1, LineEnd: 2,
	}}, nil); err != nil {
		t.Fatal(err)
	}
	st, err = s.IndexStats(ctx, "m1")
	if err != nil {
		t.Fatal(err)
	}
	if st.Files != 1 || st.Nodes != 1 {
		t.Fatalf("after save: %+v", st)
	}
}
