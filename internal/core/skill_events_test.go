package core

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/protocol/wire"
)

// skill.invoke runs its skill through the same runner skill_invoke uses
// inside a turn (ARCH-7), and the client sees the skill as a child of the
// call: child_started, its stream, child_done — where it used to see nothing
// until the result came back.
func TestSkillInvoke_TheClientSeesTheSkillAsAChild(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, ".orchestra", "skills")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "greeter.md"), []byte("---\nname: greeter\ndescription: Says hello.\n---\n\nSay hello. Task: $ARGUMENTS\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig(root)
	if err := config.Save(filepath.Join(root, ".orchestra.yml"), cfg); err != nil {
		t.Fatal(err)
	}
	c, err := New(root, Options{LLMClient: &fixedLLM{steps: []string{`{"type":"final","final":{"summary":"hello from the skill","patches":[]}}`}}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	var mu sync.Mutex
	var seen []wire.AgentEvent
	res, err := c.SkillInvoke(context.Background(), SkillInvokeParams{
		Name:      "greeter",
		Arguments: "greet",
		OnEvent: func(method string, params any) {
			if method != wire.NotifyAgentEvent {
				return
			}
			if ev, ok := params.(wire.AgentEvent); ok {
				mu.Lock()
				seen = append(seen, ev)
				mu.Unlock()
			}
		},
	})
	if err != nil {
		t.Fatalf("skill.invoke: %v", err)
	}
	if res.Output != "hello from the skill" {
		t.Errorf("Output = %q, want the child's closing words, not the envelope around them", res.Output)
	}

	var started, done *wire.AgentEvent
	for i := range seen {
		switch seen[i].Type {
		case wire.EventChildStarted:
			started = &seen[i]
		case wire.EventChildDone:
			done = &seen[i]
		}
	}
	if started == nil || done == nil {
		t.Fatalf("the client saw no child lifecycle for the skill: %+v", seen)
	}
	if started.SubagentType != "skill:greeter" || started.TaskID == "" || started.TurnID == "" {
		t.Errorf("child_started = %+v", *started)
	}
	if done.TaskID != started.TaskID || done.Status != "done" || done.Content != "hello from the skill" {
		t.Errorf("child_done = %+v", *done)
	}
}
