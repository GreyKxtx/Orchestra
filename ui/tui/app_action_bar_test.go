package tui

import (
	"strings"
	"testing"

	"github.com/orchestra/orchestra/ui/tui/state"
)

func TestActionBarActive_requiresPendingOps(t *testing.T) {
	a := testChromeApp(t)
	if a.actionBarActive() {
		t.Fatal("expected inactive without pending ops")
	}
	a.pendingReview = true
	a.pendingOps = []map[string]any{{"op": "file.write_atomic", "path": "a.go"}}
	if !a.actionBarActive() {
		t.Fatal("expected active with pending ops")
	}
	a.input.SetValue("typing")
	if a.actionBarActive() {
		t.Fatal("input must block action bar hotkeys")
	}
}

func TestDiscardPendingOps_clearsState(t *testing.T) {
	a := testChromeApp(t)
	a.pendingReview = true
	a.pendingOps = []map[string]any{{"op": "file.write_atomic", "path": "a.go"}}
	a.session.AddDiffFiles([]state.DiffFile{{Path: "a.go", Before: "a", After: "b"}})
	a.discardPendingOps()
	if len(a.pendingOps) != 0 || a.pendingReview {
		t.Fatalf("pending not cleared: ops=%d review=%v", len(a.pendingOps), a.pendingReview)
	}
	if a.session.HasDiff() {
		t.Fatal("diff should be removed")
	}
}

func TestSyncActionBar_rendersInChat(t *testing.T) {
	a := testChromeApp(t)
	a.pendingReview = true
	a.pendingOps = []map[string]any{{"op": "file.write_atomic", "path": "a.go"}}
	a.session.AddDiffFiles([]state.DiffFile{{Path: "a.go", Before: "a", After: "b"}})
	a.syncActionBar()
	a.chat.SetMessages(a.session.Messages)
	out := a.chat.View()
	if out == "" {
		t.Fatal("expected chat output")
	}
	// Action bar text is appended after diff (ANSI stripped check).
	plain := stripANSIForTest(out)
	for _, want := range []string{"pending", "pply", "discard"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("action bar missing %q in chat: %s", want, plain)
		}
	}
}

func stripANSIForTest(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		if s[i] == '\x1b' {
			for i+1 < len(s) && s[i+1] != 'm' {
				i++
			}
			continue
		}
		b = append(b, s[i])
	}
	return string(b)
}
