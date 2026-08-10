package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/protocol/schema"
	"github.com/orchestra/orchestra/internal/tools"
)

func TestAgent_PlanEnter_LegacyStub(t *testing.T) {
	root := t.TempDir()
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tr.Close() })

	llmClient := &scriptedLLM{
		steps: []string{
			`{"type":"tool_call","tool":{"name":"plan_enter","input":{}}}`,
			`{"type":"final","final":{"patches":[]}}`,
		},
	}
	ag, err := New(llmClient, v, tr, Options{MaxSteps: 5, Mode: ModeBuild})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = ag.Run(context.Background(), nil, "switch to plan")
	if err != nil {
		t.Fatal(err)
	}
	if llmClient.i < 2 {
		t.Fatalf("expected plan_enter + final steps, got %d", llmClient.i)
	}
}

func TestAgent_ModeReminder_Orchestra(t *testing.T) {
	ag := &Agent{opts: Options{Mode: ModeOrchestra}}
	got := ag.modeReminder()
	if !strings.Contains(got, "complex|focused|micro") {
		t.Fatalf("orchestra reminder must list tiers, got: %q", got)
	}
	if !strings.Contains(got, "Do not edit production code") {
		t.Fatalf("orchestra reminder must forbid production edits, got: %q", got)
	}
}
