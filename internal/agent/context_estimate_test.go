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

func TestDetectBytesPerToken(t *testing.T) {
	cases := []struct {
		name   string
		sample string
		want   int
	}{
		{"latin code", "func main() { fmt.Println(\"hello\") }", DefaultBytesPerContextToken},
		{"cyrillic prose", "Агент теряет контекст на длинных ходах и перечитывает файлы", nonLatinBytesPerContextToken},
		// A codebase with the odd Russian comment is still Latin-shaped overall.
		{"mostly code, one comment", "// счётчик\n" + strings.Repeat("for i := 0; i < 10; i++ { step(i) }\n", 4), DefaultBytesPerContextToken},
		{"empty", "", DefaultBytesPerContextToken},
	}
	for _, tc := range cases {
		if got := detectBytesPerToken(tc.sample); got != tc.want {
			t.Errorf("%s: got %d want %d", tc.name, got, tc.want)
		}
	}
}

func TestAgentBytesPerToken_PrefersMorePessimistic(t *testing.T) {
	a := &Agent{}
	if got := a.bytesPerToken(); got != DefaultBytesPerContextToken {
		t.Fatalf("default=%d", got)
	}
	a.detectedBytesPerToken = 3
	if got := a.bytesPerToken(); got != 3 {
		t.Fatalf("detected=%d want 3", got)
	}
	// Real usage always wins over the guess.
	a.calibratedBytesPerToken = 2
	if got := a.bytesPerToken(); got != 2 {
		t.Fatalf("calibrated=%d want 2", got)
	}
	// A configured value below the detection is respected too.
	b := &Agent{}
	b.opts.BytesPerContextToken = 2
	b.detectedBytesPerToken = 3
	if got := b.bytesPerToken(); got != 2 {
		t.Fatalf("configured=%d want 2", got)
	}
}
