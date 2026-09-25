package ckg

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// TestRefreshTimingOnThisRepository is the phase 7 acceptance measurement:
// an empty refresh of Orchestra's own tree, a one-file refresh, and explore.
// It indexes the whole repository, so it runs only on request:
//
//	ORCH_CKG_TIMING=1 go test ./internal/ckg -run TestRefreshTimingOnThisRepository -v
//
// It reports; it does not assert on the clock, which CI does not own.
func TestRefreshTimingOnThisRepository(t *testing.T) {
	if os.Getenv("ORCH_CKG_TIMING") == "" {
		t.Skip("set ORCH_CKG_TIMING=1 to index the repository and report timings")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(filepath.Join(t.TempDir(), "ckg.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	orch := NewOrchestratorWithIgnores(store, root, []string{"node_modules"})
	ctx := context.Background()

	start := time.Now()
	if err := orch.UpdateGraph(ctx); err != nil {
		t.Fatal(err)
	}
	st := store.LastRefresh()
	t.Logf("first index: %s (files=%d hashed=%d parsed=%d relink candidates=%d relinked=%d locked=%s)",
		time.Since(start).Round(time.Millisecond), st.Seen, st.Hashed, st.Parsed, st.Candidates, st.Relinked, st.Locked.Round(time.Millisecond))
	var nodes, edges, dangling, external int
	_ = store.db.QueryRow(`SELECT COUNT(*) FROM nodes`).Scan(&nodes)
	_ = store.db.QueryRow(`SELECT COUNT(*) FROM edges`).Scan(&edges)
	_ = store.db.QueryRow(`SELECT COUNT(*) FROM edges WHERE target_id IS NULL AND relation IN ('calls','instantiates')`).Scan(&dangling)
	_ = store.db.QueryRow(`SELECT COUNT(*) FROM edges WHERE target_id IS NULL AND relation IN ('calls','instantiates') AND is_external = 1`).Scan(&external)
	t.Logf("nodes=%d edges=%d dangling=%d (external %d)", nodes, edges, dangling, external)

	for i := 0; i < 3; i++ {
		start = time.Now()
		if err := orch.UpdateGraph(ctx); err != nil {
			t.Fatal(err)
		}
		st = store.LastRefresh()
		t.Logf("empty refresh %d: %s (hashed=%d parsed=%d candidates=%d locked=%s)",
			i+1, time.Since(start).Round(time.Millisecond), st.Hashed, st.Parsed, st.Candidates, st.Locked.Round(time.Millisecond))
	}

	// One file touched: the pass parses it, restamps nothing else, relinks
	// only what names its symbols.
	target := filepath.Join(root, "internal", "ckg", "resolve.go")
	src, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, append(src, []byte("\n// timing probe\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.WriteFile(target, src, 0o644) //nolint:errcheck // restore the tree
	start = time.Now()
	if err := orch.UpdateGraph(ctx); err != nil {
		t.Fatal(err)
	}
	st = store.LastRefresh()
	t.Logf("one-file refresh: %s (hashed=%d parsed=%d candidates=%d relinked=%d locked=%s)",
		time.Since(start).Round(time.Millisecond), st.Hashed, st.Parsed, st.Candidates, st.Relinked, st.Locked.Round(time.Millisecond))

	p := NewProvider(store, root)
	var durs []time.Duration
	for _, sym := range []string{"Agent.Run", "Core.Initialize", "NewStore", "RelinkUnresolvedEdges", "Runner.Call", "UpdateGraph", "SaveFileNodes", "ExploreSymbol", "TraverseBFS", "resolveEdgeTarget", "Client", "Store", "Options", "Scan", "hashFile", "NewRPCHandler", "Handle", "buildAgentOnEvent", "Spawn", "Negotiate"} {
		start = time.Now()
		if _, err := p.ExploreSymbol(ctx, sym, ExploreOptions{Depth: 2}); err != nil {
			t.Logf("explore %s: %v", sym, err)
		}
		durs = append(durs, time.Since(start))
	}
	sort.Slice(durs, func(i, j int) bool { return durs[i] < durs[j] })
	t.Logf("explore (%d symbols, depth 2): median=%s p95=%s max=%s",
		len(durs), durs[len(durs)/2].Round(time.Millisecond), durs[len(durs)*95/100].Round(time.Millisecond), durs[len(durs)-1].Round(time.Millisecond))
}
