package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/llm"
)

// deadlineLLM records how long each model call was given, then finishes.
type deadlineLLM struct{ budget time.Duration }

func (d *deadlineLLM) Plan(context.Context, string) (string, error) { return "{}", nil }

func (d *deadlineLLM) Complete(ctx context.Context, _ llm.CompleteRequest) (*llm.CompleteResponse, error) {
	if dl, ok := ctx.Deadline(); ok {
		d.budget = time.Until(dl)
	}
	return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant,
		Content: `{"type":"final","final":{"patches":[]}}`}}, nil
}

// skill.invoke built its agent without llm.timeout_s, so every model step had
// the agent's 25-second default. Seen live: a browser skill on a local 9B model
// through core died on "LLM step timed out after 25s (llm.timeout_s=25; raise
// it and restart core)" with llm.timeout_s: 300 in the config — the advice
// named a setting the path never read. agent.run, workflow.run and children
// all pass it.
func TestSkillInvoke_GivesEachModelStepTheConfiguredTimeout(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultConfig(root)
	cfg.LLM.TimeoutS = 300
	if err := config.Save(filepath.Join(root, ".orchestra.yml"), cfg); err != nil {
		t.Fatal(err)
	}
	skillDir := filepath.Join(root, ".orchestra", "skills")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skill := "---\nname: slowpoke\ndescription: answers after thinking\n---\n\nAnswer the question.\n"
	if err := os.WriteFile(filepath.Join(skillDir, "slowpoke.md"), []byte(skill), 0o644); err != nil {
		t.Fatal(err)
	}

	client := &deadlineLLM{}
	c, err := New(root, Options{LLMClient: client})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	if _, err := c.SkillInvoke(context.Background(), SkillInvokeParams{Name: "slowpoke", Arguments: "why"}); err != nil {
		t.Fatalf("skill.invoke: %v", err)
	}
	if client.budget < 200*time.Second {
		t.Errorf("a model step inside skill.invoke was given %v; llm.timeout_s is 300s", client.budget.Round(time.Second))
	}
}
