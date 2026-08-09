package agent

import (
	"strings"
	"testing"

	"github.com/orchestra/orchestra/llm"
)

func TestEstimatePromptTokens_shrinksAfterCompactHistory(t *testing.T) {
	big := make([]llm.Message, 0, 40)
	for i := 0; i < 40; i++ {
		big = append(big, llm.Message{
			Role:    llm.RoleUser,
			Content: strings.Repeat("x", 4000),
		})
	}
	small := []llm.Message{
		{Role: llm.RoleUser, Content: "[Session checkpoint — structured summary]\n\nshort summary"},
	}
	max := 240000
	before := estimatePromptTokens(big, max)
	after := estimatePromptTokens(small, max)
	if after >= before {
		t.Fatalf("expected estimate to shrink: before=%d after=%d", before, after)
	}
}

func TestShouldCompactHistory_tokenBarAligned(t *testing.T) {
	maxBytes := 240000 // 60k token budget (matches num_ctx=60000)
	// Fixed overhead 32KiB → need ~140KiB history to push estimate past 70% (42k tok)
	heavy := []llm.Message{{Role: llm.RoleUser, Content: strings.Repeat("a", 140000)}}
	if !shouldCompactHistory(heavy, maxBytes, 70) {
		est := estimatePromptTokens(heavy, maxBytes)
		t.Fatalf("expected compact at est=%d vs 70%% of %d", est, maxBytes/4)
	}
	tiny := []llm.Message{{Role: llm.RoleUser, Content: "hi"}}
	if shouldCompactHistory(tiny, maxBytes, 70) {
		t.Fatalf("tiny history should not compact (est=%d)", estimatePromptTokens(tiny, maxBytes))
	}
}

func TestEstimatePromptTokens_doesNotScaleWithWindow(t *testing.T) {
	hist := []llm.Message{{Role: llm.RoleUser, Content: strings.Repeat("x", 40000)}}
	smallWin := estimatePromptTokens(hist, 64*1024)
	largeWin := estimatePromptTokens(hist, 450*1024)
	if smallWin != largeWin {
		t.Fatalf("overhead must not scale with MaxPromptBytes: small=%d large=%d", smallWin, largeWin)
	}
}

func TestShouldCompactHistoryEx_vLLMFitsGate(t *testing.T) {
	hist := []llm.Message{{Role: llm.RoleUser, Content: "hi"}}
	maxBytes := 64 * 1024
	// Real usage already exceeds PromptBudgetTokens(51200, 8192)=40960.
	if !shouldCompactHistoryEx(hist, maxBytes, 60, 45000, 51200, 8192, 4) {
		t.Fatal("expected compact when lastPrompt exceeds prompt budget")
	}
	if shouldCompactHistoryEx(hist, maxBytes, 60, 5000, 51200, 8192, 4) {
		t.Fatal("small real usage should not force compact")
	}
}

func TestShouldCompactHistoryEx_hugeMaxTokensDoesNotFalseTrigger(t *testing.T) {
	hist := []llm.Message{{Role: llm.RoleUser, Content: "hi"}}
	maxBytes := 256 * 1024
	// Settings mistake: max_tokens ≈ num_ctx. Old FitsContext gate compacted
	// every step; PromptBudgetTokens must keep a tiny prompt under threshold.
	if shouldCompactHistoryEx(hist, maxBytes, 70, 1000, 122880, 104192, 4) {
		t.Fatal("1k prompt must not compact just because max_tokens≈num_ctx")
	}
	// Same window: prompt past budget threshold still compacts.
	budget := llm.PromptBudgetTokens(122880, 104192)
	over := budget*70/100 + 1
	if !shouldCompactHistoryEx(hist, maxBytes, 70, over, 122880, 104192, 4) {
		t.Fatalf("expected compact when lastPrompt=%d > 70%% of budget=%d", over, budget)
	}
}
