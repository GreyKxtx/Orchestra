package e2e_agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/tasks"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
)

// finishingWorkerLLM immediately finishes with an empty final (no patches).
type finishingWorkerLLM struct{}

func (l *finishingWorkerLLM) Plan(_ context.Context, _ string) (string, error) { return "{}", nil }

func (l *finishingWorkerLLM) Complete(_ context.Context, _ llm.CompleteRequest) (*llm.CompleteResponse, error) {
	return &llm.CompleteResponse{Message: llm.Message{
		Role:    llm.RoleAssistant,
		Content: `{"type":"final","final":{"patches":[]}}`,
	}}, nil
}

const tierRebindRoutingTemplate = `
version: 1
roles:
  L3:
    label: "Focused worker"
    provider: MODEL_PROVIDER
    model: MODEL_ID
routing:
  write_function: { required_tier: L3, subagent_type: worker, tier: focused }
`

func writeTierRebindRouting(t *testing.T, dir, provider, model string) *config.OrchestraRouting {
	t.Helper()
	body := strings.NewReplacer("MODEL_PROVIDER", provider, "MODEL_ID", model).
		Replace(tierRebindRoutingTemplate)
	if err := os.WriteFile(filepath.Join(dir, config.RoutingFileName), []byte(body), 0o600); err != nil {
		t.Fatalf("write routing: %v", err)
	}
	r, err := config.LoadOrchestraRouting(dir)
	if err != nil {
		t.Fatalf("LoadOrchestraRouting: %v", err)
	}
	if r == nil {
		t.Fatal("routing not loaded")
	}
	return r
}

// TestOrchestra_E2E_TierRebindWithoutWorkOrderChange verifies PR1 checklist
// item 6: swapping the L3 model binding in orchestra_routing.yaml redirects
// the worker to a different provider/model while the WorkOrder JSON stays
// byte-identical (agents address tiers, never model IDs).
func TestOrchestra_E2E_TierRebindWithoutWorkOrderChange(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc Foo() {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}

	woBytes, _ := json.Marshal(tasks.WorkOrder{
		Intent:             "Touch nothing; report done",
		TargetFile:         "main.go",
		AcceptanceCriteria: []string{"noop"},
	})
	workOrder := string(woBytes)

	// runOnce spawns the same WorkOrder through the runtime router wired
	// exactly like Core.buildChildAgentConfig and reports the resolved binding.
	runOnce := func(t *testing.T, routing *config.OrchestraRouting) (gotProvider, gotModel string) {
		t.Helper()
		cfg := &config.ProjectConfig{Routing: routing}

		tr, err := tools.NewRunner(root, tools.RunnerOptions{DryRun: true})
		if err != nil {
			t.Fatalf("NewRunner: %v", err)
		}
		defer tr.Close()

		child := tasks.ChildAgentConfig{
			ResolveTier: func(tier string) (string, string, bool) {
				return cfg.ResolveTierBinding(tier)
			},
			RouteTaskType: func(taskType string) (tasks.TaskTypeRoute, bool) {
				rule, ok := cfg.Routing.Route(taskType)
				if !ok {
					return tasks.TaskTypeRoute{}, false
				}
				route := tasks.TaskTypeRoute{SubagentType: rule.SubagentType, Tier: rule.Tier}
				if role, found := cfg.Routing.ResolveRole(rule.RequiredTier); found {
					route.Provider = role.Provider
					route.Model = role.Model
				}
				return route, true
			},
			ResolveClient: func(provider, model string) (llm.Client, string, string, error) {
				gotProvider, gotModel = provider, model
				return &finishingWorkerLLM{}, provider, model, nil
			},
		}
		taskRunner := tasks.New(&finishingWorkerLLM{}, v, tr, child)
		defer taskRunner.Close()

		// Only task_type is provided: subagent/tier/model all come from routing.
		id, err := taskRunner.Spawn(context.Background(), agent.SubtaskSpawnRequest{
			Goal:     workOrder,
			TaskType: "write_function",
		})
		if err != nil {
			t.Fatalf("Spawn: %v", err)
		}
		res, err := taskRunner.Wait(context.Background(), id, 10_000)
		if err != nil {
			t.Fatalf("Wait: %v", err)
		}
		if res.Status != "done" {
			t.Fatalf("worker status = %q (%s)", res.Status, res.Error)
		}
		return gotProvider, gotModel
	}

	dir := t.TempDir()

	routingA := writeTierRebindRouting(t, dir, "lmstudio", "qwen2.5-coder-32b")
	provA, modelA := runOnce(t, routingA)
	if provA != "lmstudio" || modelA != "qwen2.5-coder-32b" {
		t.Fatalf("binding A = %s/%s", provA, modelA)
	}

	// Rebind L3 to another provider/model; the WorkOrder string is untouched.
	routingB := writeTierRebindRouting(t, dir, "together", "deepseek-coder-v2")
	provB, modelB := runOnce(t, routingB)
	if provB != "together" || modelB != "deepseek-coder-v2" {
		t.Fatalf("binding B = %s/%s", provB, modelB)
	}

	if provA == provB && modelA == modelB {
		t.Fatal("rebinding must change the resolved provider/model")
	}
	if string(woBytes) != workOrder {
		t.Fatal("WorkOrder must stay byte-identical across rebinds")
	}
}
