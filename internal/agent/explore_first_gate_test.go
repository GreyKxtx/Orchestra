package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/lessons"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
)

func TestWorkerExploreFirstGate(t *testing.T) {
	a := &Agent{
		opts: Options{
			Mode:            ModeWorker,
			WorkerEditPaths: []string{"internal/foo.go"},
		},
	}
	if err := a.checkExploreFirstGate("edit", nil); err == nil {
		t.Fatal("expected gate before explore")
	}
	hist := []llm.Message{{
		Role: llm.RoleAssistant,
		ToolCalls: []llm.ToolCall{{
			Function: llm.ToolCallFunc{Name: "read", Arguments: llm.ToolArguments(`{"path":"internal/foo.go"}`)},
		}},
	}}
	if err := a.checkExploreFirstGate("edit", hist); err != nil {
		t.Fatalf("expected pass after target read: %v", err)
	}
}

func TestOrchestraExploreFirstGate(t *testing.T) {
	a := &Agent{opts: Options{Mode: ModeOrchestra}}
	if err := a.checkExploreFirstGate("write", nil); err == nil {
		t.Fatal("orchestra write must require explore first")
	}
	a.markExploreFirstSatisfied("explore")
	if err := a.checkExploreFirstGate("write", nil); err != nil {
		t.Fatalf("expected pass after explore: %v", err)
	}
}

func TestArchitectureExploreFirstGate(t *testing.T) {
	a := &Agent{opts: Options{Mode: ModeArchitecture}}
	if err := a.checkExploreFirstGate("edit", nil); err == nil {
		t.Fatal("architecture edit must require explore first")
	}
	a.markExploreFirstSatisfied("grep")
	if err := a.checkExploreFirstGate("write", nil); err != nil {
		t.Fatalf("expected pass after grep: %v", err)
	}
}

func TestHandleLessonPromote(t *testing.T) {
	root := t.TempDir()
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	a := &Agent{opts: Options{Mode: ModeArchitecture}, tools: tr}
	if err := lessons.Append(root, lessons.Entry{Dept: "eng", Kind: lessons.KindPattern, Task: "pattern x", Verify: "passed"}); err != nil {
		t.Fatal(err)
	}
	out, err := a.handleLessonPromote(context.Background(), json.RawMessage(`{"dept":"eng"}`))
	if err != nil {
		t.Fatalf("promote: %v", err)
	}
	if !strings.Contains(string(out), "draft") {
		t.Fatalf("out=%s", out)
	}
}
