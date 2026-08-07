package agent

import (
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/llm"
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
	// overhead ≈ 48KiB → need ~120KiB+ history to push estimate past 70% (42k tok)
	heavy := []llm.Message{{Role: llm.RoleUser, Content: strings.Repeat("a", 130000)}}
	if !shouldCompactHistory(heavy, maxBytes, 70) {
		est := estimatePromptTokens(heavy, maxBytes)
		t.Fatalf("expected compact at est=%d vs 70%% of %d", est, maxBytes/4)
	}
	tiny := []llm.Message{{Role: llm.RoleUser, Content: "hi"}}
	if shouldCompactHistory(tiny, maxBytes, 70) {
		t.Fatalf("tiny history should not compact (est=%d)", estimatePromptTokens(tiny, maxBytes))
	}
}

func TestShouldCompactHistoryEx_vLLMFitsGate(t *testing.T) {
	hist := []llm.Message{{Role: llm.RoleUser, Content: "hi"}}
	maxBytes := 64 * 1024
	// Real usage already leaves no room for 8192 completion inside 51200.
	if !shouldCompactHistoryEx(hist, maxBytes, 60, 45000, 51200, 8192, 4) {
		t.Fatal("expected compact when lastPrompt+max_tokens overflows max_model_len")
	}
	if shouldCompactHistoryEx(hist, maxBytes, 60, 5000, 51200, 8192, 4) {
		t.Fatal("small real usage should not force compact")
	}
}
