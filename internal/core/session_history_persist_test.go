package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/sessionfile"
)

// TestSessionMessage_PersistsHistoryOnMaxSteps ensures a soft max_steps stop
// still snapshots agent history. Without this, TUI reopen showed the chat UI
// but the model started with an empty history and re-read the whole repo.
func TestSessionMessage_PersistsHistoryOnMaxSteps(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Always ask for a tool — never final — so the agent hits MaxSteps.
	toolStep := `{"type":"tool_call","tool":{"name":"ls","arguments":{}}}`
	_, h := setupInitializedCore(t, root, &fixedLLM{
		steps: []string{toolStep, toolStep, toolStep, toolStep},
	})

	startP, _ := json.Marshal(SessionStartParams{})
	res, err := h.Handle(context.Background(), "session.start", startP)
	if err != nil {
		t.Fatalf("session.start: %v", err)
	}
	sessionID := res.(*SessionStartResult).SessionID

	msgP, _ := json.Marshal(SessionMessageParams{
		SessionID: sessionID,
		Content:   "list files forever",
		MaxSteps:  2,
	})
	if _, err := h.Handle(context.Background(), "session.message", msgP); err != nil {
		t.Fatalf("session.message: %v", err)
	}

	snap, err := sessionfile.Load(root, sessionID)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if len(snap.History) == 0 {
		t.Fatal("expected agent history persisted after max_steps soft-stop")
	}
}
