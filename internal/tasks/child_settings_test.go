package tasks

import (
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/app"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/llm"
)

// A permission rule is the project's, not the top-level agent's alone. Task
// children were built without the rules, so a subagent read the file a deny
// rule keeps from the turn.
func TestChild_PermissionRulesReachSubagents(t *testing.T) {
	var toolResult string
	mock := &scriptLLM{reply: func(req llm.CompleteRequest) llm.Message {
		for _, m := range req.Messages {
			if m.Role == llm.RoleTool {
				toolResult = m.Content
				return finish("done")
			}
		}
		return callTool("read", map[string]string{"path": "secrets.env"})
	}}
	settings := app.Settings{PermissionRules: []config.PermissionRule{{Tool: "read", Pattern: "secrets.env", Action: "deny"}}}
	r, _ := newAgencyRunner(t, mock, ChildAgentConfig{Settings: &settings})
	res := spawnAndWait(t, r, agent.SubtaskSpawnRequest{Goal: "read secrets.env", SubagentType: "explore"})
	if res.Status != "done" {
		t.Fatalf("child: %+v", res)
	}
	if !strings.Contains(toolResult, "denied by permission ruleset") {
		t.Errorf("the deny rule must reach the child, got %q", toolResult)
	}
}
