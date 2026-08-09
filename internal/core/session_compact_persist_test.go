package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/llm"
)

// TestSessionMessage_ReplaceHistoryAfterCompaction ensures a rewritten (shorter)
// outHistory is persisted — append-only would panic or drop the checkpoint.
func TestSessionMessage_ReplaceHistoryAfterCompaction(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}

	_, h := setupInitializedCore(t, root, &fixedLLM{
		steps: []string{`{"type":"final","final":{"patches":[]}}`},
	})

	startP, _ := json.Marshal(SessionStartParams{})
	res, err := h.Handle(context.Background(), "session.start", startP)
	if err != nil {
		t.Fatalf("session.start: %v", err)
	}
	sessionID := res.(*SessionStartResult).SessionID

	sess, err := h.core.sessions.Get(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	// Seed a fat history as if prior turns accumulated tool spam.
	fat := make([]llm.Message, 0, 8)
	for i := 0; i < 6; i++ {
		fat = append(fat, llm.Message{Role: llm.RoleUser, Content: strings.Repeat("x", 200)})
		fat = append(fat, llm.Message{Role: llm.RoleAssistant, Content: strings.Repeat("y", 200)})
	}
	sess.Lock()
	sess.ReplaceHistory(fat)
	sess.Unlock()

	// Simulate compaction rewrite that SessionMessage must persist.
	checkpoint := []llm.Message{
		{Role: llm.RoleUser, Content: "[Session checkpoint — structured summary]\n\nGoal: keep going"},
	}
	sess.Lock()
	inLen := len(sess.CopyHistory())
	sess.ReplaceHistory(checkpoint)
	out := sess.CopyHistory()
	sess.Unlock()
	if inLen <= len(out) {
		t.Fatalf("setup: expected shorter history after compact, in=%d out=%d", inLen, len(out))
	}
	if !strings.Contains(out[0].Content, "Session checkpoint") {
		t.Fatalf("checkpoint missing: %q", out[0].Content)
	}

	// Next turn seeds from CopyHistory — must see checkpoint, not fat prefix.
	msgP, _ := json.Marshal(SessionMessageParams{
		SessionID: sessionID,
		Content:   "continue",
		Apply:     false,
	})
	if _, err := h.Handle(context.Background(), "session.message", msgP); err != nil {
		t.Fatalf("session.message: %v", err)
	}

	histP, _ := json.Marshal(SessionHistoryParams{SessionID: sessionID})
	histRes, err := h.Handle(context.Background(), "session.history", histP)
	if err != nil {
		t.Fatalf("session.history: %v", err)
	}
	hr := histRes.(*SessionHistoryResult)
	if len(hr.Messages) == 0 {
		t.Fatal("empty history")
	}
	// Fat 12-message prefix must be gone; checkpoint or post-turn msgs remain.
	for _, m := range hr.Messages {
		if strings.Contains(m.Content, strings.Repeat("x", 50)) {
			t.Fatal("fat pre-compact history leaked into next turn")
		}
	}
}
