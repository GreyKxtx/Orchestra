package core

import (
	"testing"

	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/internal/sessionfile"
)

func TestTruncateHistoryForUIPrefix_keepsThroughNthUser(t *testing.T) {
	ui := []sessionfile.UIMessage{
		{Role: "user", Text: "first"},
		{Role: "assistant", Text: "a1"},
		{Role: "user", Text: "second"},
		{Role: "assistant", Text: "a2"},
	}
	hist := []llm.Message{
		{Role: llm.RoleUser, Content: "first"},
		{Role: llm.RoleAssistant, Content: "a1"},
		{Role: llm.RoleUser, Content: "second"},
		{Role: llm.RoleAssistant, Content: "a2"},
	}
	got := truncateHistoryForUIPrefix(hist, ui[:3]) // through second user msg header only — ui[2] is user "second"
	if len(got) != 3 {
		t.Fatalf("want 3 history msgs (through 2nd user), got %d", len(got))
	}
	if got[2].Content != "second" {
		t.Fatalf("last kept msg: %q", got[2].Content)
	}
}

func TestTruncateHistoryForUIPrefix_firstUserOnly(t *testing.T) {
	ui := []sessionfile.UIMessage{{Role: "user", Text: "hi"}}
	hist := []llm.Message{
		{Role: llm.RoleUser, Content: "hi"},
		{Role: llm.RoleAssistant, Content: "hey"},
	}
	got := truncateHistoryForUIPrefix(hist, ui)
	if len(got) != 1 || got[0].Content != "hi" {
		t.Fatalf("want only user prefix, got %#v", got)
	}
}

func TestSessionRewind_truncatesUIAndHistory(t *testing.T) {
	root := t.TempDir()
	c := setupSessionV2Core(t, root)
	start, err := c.SessionStart(SessionStartParams{})
	if err != nil {
		t.Fatal(err)
	}
	sid := start.SessionID

	_, err = c.SessionUISync(SessionUISyncParams{
		SessionID: sid,
		UIMessages: []sessionfile.UIMessage{
			{Role: "user", Text: "one"},
			{Role: "assistant", Text: "reply1"},
			{Role: "user", Text: "two"},
			{Role: "assistant", Text: "reply2"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	sess, err := c.sessions.GetOrLoad(root, sid)
	if err != nil {
		t.Fatal(err)
	}
	sess.Lock()
	sess.ReplaceHistory([]llm.Message{
		{Role: llm.RoleUser, Content: "one"},
		{Role: llm.RoleAssistant, Content: "reply1"},
		{Role: llm.RoleUser, Content: "two"},
		{Role: llm.RoleAssistant, Content: "reply2"},
	})
	sess.Unlock()

	res, err := c.SessionRewind(SessionRewindParams{SessionID: sid, UIMessageIndex: 2})
	if err != nil {
		t.Fatal(err)
	}
	if res.UIMessages != 3 || res.HistoryMessages != 3 {
		t.Fatalf("unexpected counts: ui=%d hist=%d", res.UIMessages, res.HistoryMessages)
	}

	got, err := c.SessionGet(SessionGetParams{SessionID: sid})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.UIMessages) != 3 {
		t.Fatalf("ui len=%d", len(got.UIMessages))
	}
	if got.UIMessages[2].Text != "two" {
		t.Fatalf("last ui: %q", got.UIMessages[2].Text)
	}
	hist, err := c.SessionHistory(SessionHistoryParams{SessionID: sid})
	if err != nil {
		t.Fatal(err)
	}
	if len(hist.Messages) != 3 {
		t.Fatalf("hist len=%d", len(hist.Messages))
	}
}

func TestSessionRewind_rejectsNonUserIndex(t *testing.T) {
	root := t.TempDir()
	c := setupSessionV2Core(t, root)
	start, err := c.SessionStart(SessionStartParams{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.SessionUISync(SessionUISyncParams{
		SessionID: start.SessionID,
		UIMessages: []sessionfile.UIMessage{
			{Role: "user", Text: "hi"},
			{Role: "assistant", Text: "hey"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.SessionRewind(SessionRewindParams{SessionID: start.SessionID, UIMessageIndex: 1})
	if err == nil {
		t.Fatal("expected error rewinding to assistant message")
	}
}
