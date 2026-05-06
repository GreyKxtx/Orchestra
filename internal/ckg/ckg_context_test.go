package ckg

import (
	"context"
	"strings"
	"testing"
)

// ---- tokenizeQuery ----

func TestTokenizeQuery(t *testing.T) {
	tests := []struct {
		q    string
		want []string
	}{
		{"Handler agent runtime", []string{"handler", "agent", "runtime"}},
		{"the and for with", nil}, // all stopwords
		{"ab", nil},               // too short
		{"agent.Run loop", []string{"agent", "loop"}},    // "run" is stopword
		{"Agent Agent agent", []string{"agent"}},          // dedup
		{"RuntimeQuery trace_id", []string{"runtimequery", "trace"}}, // split on _
	}
	for _, tc := range tests {
		got := tokenizeQuery(tc.q)
		if len(got) != len(tc.want) {
			t.Errorf("tokenizeQuery(%q) = %v, want %v", tc.q, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("tokenizeQuery(%q)[%d] = %q, want %q", tc.q, i, got[i], tc.want[i])
			}
		}
	}
}

// ---- FindRelevantNodes ----

func TestFindRelevantNodes_Basic(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	nodes := []Node{
		{FQN: "ex/agent.Agent", ShortName: "Agent", Kind: "struct", LineStart: 1, LineEnd: 50},
		{FQN: "ex/agent.Agent.Run", ShortName: "Run", Kind: "method", LineStart: 51, LineEnd: 100},
		{FQN: "ex/tools.Runner", ShortName: "Runner", Kind: "struct", LineStart: 1, LineEnd: 30},
		{FQN: "ex/ckg.Store", ShortName: "Store", Kind: "struct", LineStart: 1, LineEnd: 20},
	}
	for _, n := range nodes[:2] {
		if err := s.SaveFileNodes(ctx, "agent.go", "h1", "go", "ex", "agent", []Node{n}, nil); err != nil {
			t.Fatalf("SaveFileNodes: %v", err)
		}
	}
	if err := s.SaveFileNodes(ctx, "tools.go", "h2", "go", "ex", "tools", nodes[2:3], nil); err != nil {
		t.Fatalf("SaveFileNodes tools: %v", err)
	}
	if err := s.SaveFileNodes(ctx, "cache.go", "h3", "go", "ex", "ckg", nodes[3:4], nil); err != nil {
		t.Fatalf("SaveFileNodes store: %v", err)
	}

	// Query matching "agent" should return Agent nodes ranked first.
	results, err := s.FindRelevantNodes(ctx, "agent loop handling", 10)
	if err != nil {
		t.Fatalf("FindRelevantNodes: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result for 'agent'")
	}
	// Agent nodes should appear (FQN contains "agent").
	found := false
	for _, r := range results {
		if strings.Contains(strings.ToLower(r.FQN), "agent") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no agent nodes in results: %v", results)
	}
}

func TestFindRelevantNodes_Empty(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Stopwords only — no tokens → empty result.
	results, err := s.FindRelevantNodes(ctx, "the and for", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results for stopwords-only query, got %d", len(results))
	}
}

func TestFindRelevantNodes_Limit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Insert 5 nodes all matching "handler".
	nodes := []Node{
		{FQN: "ex/pkg.Handler1", ShortName: "Handler1", Kind: "func", LineStart: 1, LineEnd: 5},
		{FQN: "ex/pkg.Handler2", ShortName: "Handler2", Kind: "func", LineStart: 6, LineEnd: 10},
		{FQN: "ex/pkg.Handler3", ShortName: "Handler3", Kind: "func", LineStart: 11, LineEnd: 15},
		{FQN: "ex/pkg.Handler4", ShortName: "Handler4", Kind: "func", LineStart: 16, LineEnd: 20},
		{FQN: "ex/pkg.Handler5", ShortName: "Handler5", Kind: "func", LineStart: 21, LineEnd: 25},
	}
	if err := s.SaveFileNodes(ctx, "handlers.go", "h1", "go", "ex", "pkg", nodes, nil); err != nil {
		t.Fatalf("SaveFileNodes: %v", err)
	}

	results, err := s.FindRelevantNodes(ctx, "handler request", 3)
	if err != nil {
		t.Fatalf("FindRelevantNodes: %v", err)
	}
	if len(results) > 3 {
		t.Errorf("expected at most 3 results, got %d", len(results))
	}
}

// ---- FormatNodesForPrompt ----

func TestFormatNodesForPrompt_Empty(t *testing.T) {
	out := FormatNodesForPrompt(nil, 800)
	if out != "" {
		t.Errorf("expected empty string for nil nodes, got %q", out)
	}
}

func TestFormatNodesForPrompt_Basic(t *testing.T) {
	nodes := []Node{
		{FQN: "ex/agent.Agent.Run", Kind: "method", LineStart: 187, LineEnd: 215},
		{FQN: "ex/tools.Runner", Kind: "struct", LineStart: 1, LineEnd: 50},
	}
	out := FormatNodesForPrompt(nodes, 800)
	if !strings.HasPrefix(out, "<ckg_context>") {
		t.Errorf("missing <ckg_context> prefix: %q", out)
	}
	if !strings.HasSuffix(out, "</ckg_context>") {
		t.Errorf("missing </ckg_context> suffix: %q", out)
	}
	if !strings.Contains(out, "ex/agent.Agent.Run") {
		t.Errorf("missing FQN in output: %q", out)
	}
	if !strings.Contains(out, "L187-215") {
		t.Errorf("missing line range in output: %q", out)
	}
}

func TestFormatNodesForPrompt_ByteBudget(t *testing.T) {
	// Very small budget forces truncation.
	nodes := []Node{
		{FQN: "ex/agent.Agent.Run", Kind: "method", LineStart: 187, LineEnd: 215},
		{FQN: "ex/tools.Runner", Kind: "struct", LineStart: 1, LineEnd: 50},
		{FQN: "ex/ckg.Store", Kind: "struct", LineStart: 1, LineEnd: 30},
	}
	// Budget that fits only the header+footer+first node.
	out := FormatNodesForPrompt(nodes, 80)
	if len(out) > 80 {
		t.Errorf("output %d bytes exceeds budget of 80: %q", len(out), out)
	}
	if !strings.HasSuffix(out, "</ckg_context>") {
		t.Errorf("output not properly closed: %q", out)
	}
}
