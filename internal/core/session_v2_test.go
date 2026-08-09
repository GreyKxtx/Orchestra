package core

import (
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/patch/cache"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/core/session"
	"github.com/orchestra/orchestra/internal/llm"
	"github.com/orchestra/orchestra/protocol"
	"github.com/orchestra/orchestra/internal/sessionfile"
	"github.com/orchestra/orchestra/internal/tools"
)

func TestSessionStart_ReopenByID(t *testing.T) {
	root := t.TempDir()
	c := setupSessionV2Core(t, root)

	id := "20260805T150000-test"
	s := session.NewWithID(id)
	s.AppendHistory([]llm.Message{{Role: llm.RoleUser, Content: "remember"}})
	s.Lock()
	if err := s.Snapshot(root); err != nil {
		t.Fatal(err)
	}
	s.Unlock()

	res, err := c.SessionStart(SessionStartParams{SessionID: id})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Restored {
		t.Fatal("expected restored=true")
	}
	if res.SessionID != id {
		t.Fatalf("session_id=%q", res.SessionID)
	}
}

func TestSessionStart_RestoredWhenOnlyTodos(t *testing.T) {
	root := t.TempDir()
	c := setupSessionV2Core(t, root)

	id := "20260805T150001-todos"
	s := session.NewWithID(id)
	s.Lock()
	s.SetTodos([]tools.TodoItem{{Content: "ship fix", Status: "pending"}})
	s.SetPlanPath(".orchestra/plans/" + id + ".md")
	if err := s.Snapshot(root); err != nil {
		t.Fatal(err)
	}
	s.Unlock()

	res, err := c.SessionStart(SessionStartParams{SessionID: id})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Restored {
		t.Fatal("expected restored=true when only todos/plan_path present")
	}
	got, err := c.SessionGet(SessionGetParams{SessionID: id})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Todos) != 1 || got.Todos[0].Content != "ship fix" {
		t.Fatalf("todos=%+v", got.Todos)
	}
	if got.PlanPath == "" {
		t.Fatal("expected plan_path in SessionGet")
	}
}

func TestPersistSessionTodos_MidTurnSnapshot(t *testing.T) {
	root := t.TempDir()
	c := setupSessionV2Core(t, root)
	start, err := c.SessionStart(SessionStartParams{})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := c.sessions.GetOrLoad(root, start.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	payload := `[{"content":"mid-turn","status":"pending"}]`
	persistSessionTodos(root, sess, payload)

	got, err := c.SessionGet(SessionGetParams{SessionID: start.SessionID})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Todos) != 1 || got.Todos[0].Content != "mid-turn" {
		t.Fatalf("todos=%+v", got.Todos)
	}
}

func TestSessionUISync_PersistsUI(t *testing.T) {
	root := t.TempDir()
	c := setupSessionV2Core(t, root)

	start, err := c.SessionStart(SessionStartParams{})
	if err != nil {
		t.Fatal(err)
	}
	ui := []sessionfile.UIMessage{{Role: "user", Text: "hello ui"}}
	_, err = c.SessionUISync(SessionUISyncParams{
		SessionID:  start.SessionID,
		Title:      "t",
		Model:      "m",
		UIMessages: ui,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.SessionGet(SessionGetParams{SessionID: start.SessionID})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.UIMessages) != 1 || got.UIMessages[0].Text != "hello ui" {
		t.Fatalf("ui=%+v", got.UIMessages)
	}
}

func TestSessionList_ReturnsMeta(t *testing.T) {
	root := t.TempDir()
	c := setupSessionV2Core(t, root)
	start, _ := c.SessionStart(SessionStartParams{})
	_, _ = c.SessionUISync(SessionUISyncParams{
		SessionID:  start.SessionID,
		Title:      "listed",
		UIMessages: []sessionfile.UIMessage{{Role: "user", Text: "x"}},
	})
	list, err := c.SessionList(SessionListParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Sessions) != 1 {
		t.Fatalf("sessions=%d", len(list.Sessions))
	}
}

func setupSessionV2Core(t *testing.T, root string) *Core {
	t.Helper()
	cfg := config.DefaultConfig(root)
	if err := config.Save(filepath.Join(root, ".orchestra.yml"), cfg); err != nil {
		t.Fatal(err)
	}
	c, err := New(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	projectID, err := cache.ComputeProjectID(root)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Initialize(InitializeParams{
		ProjectRoot:     root,
		ProjectID:       projectID,
		ProtocolVersion: protocol.ProtocolVersion,
		OpsVersion:      protocol.OpsVersion,
		ToolsVersion:    protocol.ToolsVersion,
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}
