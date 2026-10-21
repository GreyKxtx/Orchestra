package core

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/sessionfile"
	"github.com/orchestra/orchestra/llm"
)

// stagingScriptLLM writes one file and finishes.
type stagingScriptLLM struct {
	mu    sync.Mutex
	calls int
	path  string
}

func (s *stagingScriptLLM) Plan(context.Context, string) (string, error) { return "{}", nil }

func (s *stagingScriptLLM) Complete(context.Context, llm.CompleteRequest) (*llm.CompleteResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.calls == 1 {
		return toolCall("write", `{"path":`+jsonString(s.path)+`,"content":"hello\n"}`), nil
	}
	return finalText("done"), nil
}

// Seen live: the user pressed Accept three times, "changes applied" showed
// three times, and the bar came back — the pending ops were cleared in memory
// and never in the session file, which the chat reloads them from. Settling
// them has to reach the file.
func TestSessionPending_SettlingReachesTheSessionFile(t *testing.T) {
	for _, apply := range []bool{true, false} {
		name := "discard"
		if apply {
			name = "apply"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			cfg := config.DefaultConfig(root)
			off := false
			cfg.LSP.Enabled = &off
			cfg.LSP.AutoInstall = "false"
			if err := config.Save(filepath.Join(root, ".orchestra.yml"), cfg); err != nil {
				t.Fatal(err)
			}
			c, err := New(root, Options{LLMClient: &stagingScriptLLM{path: "a.txt"}})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			start, err := c.SessionStart(SessionStartParams{})
			if err != nil {
				t.Fatal(err)
			}
			sid := start.SessionID
			if _, err := c.SessionMessage(context.Background(), SessionMessageParams{SessionID: sid, Content: "create a.txt", Mode: "build"}); err != nil {
				t.Fatalf("session.message: %v", err)
			}
			snap, err := sessionfile.Load(root, sid)
			if err != nil {
				t.Fatal(err)
			}
			if len(snap.PendingOps) == 0 {
				t.Fatal("the preview turn left nothing pending — the test proves nothing")
			}

			if apply {
				res, err := c.SessionApplyPending(context.Background(), SessionApplyPendingParams{SessionID: sid})
				if err != nil || res == nil || !res.Applied {
					t.Fatalf("apply_pending: %+v %v", res, err)
				}
				if _, err := os.Stat(filepath.Join(root, "a.txt")); err != nil {
					t.Fatalf("a.txt was not written: %v", err)
				}
			} else if _, err := c.SessionDiscardPending(SessionDiscardPendingParams{SessionID: sid}); err != nil {
				t.Fatalf("discard_pending: %v", err)
			}

			snap, err = sessionfile.Load(root, sid)
			if err != nil {
				t.Fatal(err)
			}
			if len(snap.PendingOps) != 0 {
				t.Fatalf("the session file still holds %d pending op(s) after %s", len(snap.PendingOps), name)
			}
		})
	}
}
