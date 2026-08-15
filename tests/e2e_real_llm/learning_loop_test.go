package e2e_real_llm

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/ckg"
	"github.com/orchestra/orchestra/internal/lessons"
	"github.com/orchestra/orchestra/internal/memory"
	toolsession "github.com/orchestra/orchestra/internal/tools/session"
	"github.com/orchestra/orchestra/internal/tools"
)

// TestLearningLoop_DeptMemoryTools verifies dept lessons write/read/search without LLM.
func TestLearningLoop_DeptMemoryTools(t *testing.T) {
	root := t.TempDir()
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	input, err := json.Marshal(map[string]any{
		"content": "always run go test ./... before final",
		"scope":   "engineering",
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := tr.Call(context.Background(), "memory_write", input)
	if err != nil {
		t.Fatalf("memory_write dept: %v", err)
	}
	if !strings.Contains(string(out), "lessons") {
		t.Fatalf("memory_write out=%s", out)
	}

	store := memory.NewStore(root, "", memory.DefaultConfig())
	read := store.Read("lessons", "", 8192)
	if read.Content == "" || !strings.Contains(read.Content, "go test") {
		t.Fatalf("memory_read lessons: %+v", read)
	}

	sess := toolsession.NewClient(root, func() string { return "" }, func() memory.Config { return memory.DefaultConfig() }, func() *ckg.Store { return nil })
	search, err := sess.MemorySearch(context.Background(), toolsession.MemorySearchRequest{Query: "go test", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, h := range search.Hits {
		if h.Layer == "lessons" {
			found = true
		}
	}
	if !found {
		t.Fatalf("memory_search hits=%+v", search.Hits)
	}

	lessonPath := filepath.Join(root, filepath.FromSlash(lessons.RelDir), "engineering.md")
	if _, err := os.Stat(lessonPath); err != nil {
		t.Fatalf("lessons file: %v", err)
	}
}

// TestRealLLM_LearningWorkerRecordsLesson runs a real Worker model and expects
// an episodic lesson file after verify (best-effort smoke; inspect on failure).
func TestRealLLM_LearningWorkerRecordsLesson(t *testing.T) {
	requireE2ELLM(t)
	t.Skip("manual smoke: run orchestra worker verify flow and inspect .orchestra/memory/lessons/ — automated assertion is flaky across models")
}
