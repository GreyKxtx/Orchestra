package agent

import (
	"context"
	"testing"

	"github.com/orchestra/orchestra/llm"
)

// cacheAwareRecorder is a UsageRecorder that also implements
// PromptCacheRecorder, the way usage.Tracker does.
type cacheAwareRecorder struct {
	recordingTracker
	cached, write int
}

func (r *cacheAwareRecorder) RecordPromptCache(_, _ string, cached, write int) {
	r.cached += cached
	r.write += write
}

func TestAgent_ForwardsPromptCacheCountersToRecorder(t *testing.T) {
	client := &toolCallSequenceLLM{responses: []*llm.CompleteResponse{{
		Message: llm.Message{Role: llm.RoleAssistant, Content: `{"type":"final","final":{"patches":[]}}`},
		Usage: &llm.TokenUsage{
			PromptTokens:       20_000,
			CompletionTokens:   40,
			CachedPromptTokens: 18_000,
			CacheWriteTokens:   500,
		},
	}}}
	rec := &cacheAwareRecorder{}
	ag, _ := newTestAgent(t, client, Options{UsageTracker: rec})

	if _, _, err := ag.Run(context.Background(), nil, "hello"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// The gross count reached usage.jsonl before; the cache split stopped at
	// the TUI event. Both have to arrive at the recorder for the run to be
	// diagnosable afterwards.
	if rec.totalPrompt != 20_000 {
		t.Errorf("prompt tokens = %d, want 20000", rec.totalPrompt)
	}
	if rec.cached != 18_000 || rec.write != 500 {
		t.Errorf("cache counters = %d/%d, want 18000/500", rec.cached, rec.write)
	}
}

func TestAgent_PlainRecorderStillWorksWithoutCacheMethod(t *testing.T) {
	client := &toolCallSequenceLLM{responses: []*llm.CompleteResponse{{
		Message: llm.Message{Role: llm.RoleAssistant, Content: `{"type":"final","final":{"patches":[]}}`},
		Usage:   &llm.TokenUsage{PromptTokens: 100, CompletionTokens: 10, CachedPromptTokens: 90},
	}}}
	rec := &recordingTracker{}
	ag, _ := newTestAgent(t, client, Options{UsageTracker: rec})

	if _, _, err := ag.Run(context.Background(), nil, "hello"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rec.totalPrompt != 100 {
		t.Errorf("a recorder without the cache method must still get the gross count, got %d", rec.totalPrompt)
	}
}
