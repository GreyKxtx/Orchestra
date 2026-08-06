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
