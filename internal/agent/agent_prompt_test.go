package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/agent/working"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
)

// "What got injected on this turn" used to be answerable only by re-deriving
// budgets from config and guessing at file sizes. buildSystemPrompt must log
// a memory.inject event with the real breakdown so /memory refresh (TUI) has
// something to read back.
func TestBuildSystemPrompt_LogsMemoryInject(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ORCHESTRA.md"), []byte("PROJECT RULES"), 0o644); err != nil {
		t.Fatal(err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	a := &Agent{
		tools:   tr,
		opts:    Options{Mode: ModeBuild, SessionID: "s1", AgentLogger: llm.NewLogger(root)},
		working: working.New("wire the weather panel into the sidebar"),
	}

	got := a.buildSystemPrompt()
	if !strings.Contains(got, "PROJECT RULES") {
		t.Fatalf("prompt missing injected memory: %s", got)
	}

	data, err := os.ReadFile(filepath.Join(root, ".orchestra", "llm_log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var e llm.LLMLogEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		if e.Event != "memory.inject" {
			continue
		}
		found = true
		if !strings.Contains(e.Detail, "orchestra=") || !strings.Contains(e.Detail, "total=") {
			t.Errorf("detail = %q, want a per-layer breakdown", e.Detail)
		}
	}
	if !found {
		t.Fatal("no memory.inject event logged")
	}
}
