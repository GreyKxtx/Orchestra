package skillrun

import (
	"context"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/skills"
)

// A skill and a custom agent may carry the same name, and then the role a
// project gets depends on which door it came through: `--skill reviewer` was
// refused (internal/cli/apply.go), while skill_invoke{skill: "reviewer"} and
// the RPC skill.invoke behind the TUI's /skill ran the skill without a word.
//
// The check belongs to the name, not the door.
func TestInvokeSkill_RefusesANameACustomAgentAlreadyHas(t *testing.T) {
	cfg := config.DefaultConfig(t.TempDir())
	cfg.Agents = []config.AgentDefinition{{Name: "reviewer", SystemPrompt: "you review"}}

	r := &Runner{
		cfg:    cfg,
		skills: []*skills.Skill{{Name: "reviewer", Description: "a skill by the same name", Body: "do the thing"}},
	}

	_, err := r.InvokeSkill(context.Background(), "reviewer", "review it")
	if err == nil {
		t.Fatal("a skill shadowing a custom agent ran without complaint")
	}
	if !strings.Contains(err.Error(), "reviewer") || !strings.Contains(err.Error(), "agents") {
		t.Errorf("err = %q, want it to name the skill and where the clash is", err.Error())
	}
}

// The built-in modes were already reserved on the CLI door only.
func TestInvokeSkill_RefusesABuiltInModeName(t *testing.T) {
	cfg := config.DefaultConfig(t.TempDir())
	r := &Runner{
		cfg:    cfg,
		skills: []*skills.Skill{{Name: "worker", Description: "shadows a child-only mode", Body: "do the thing"}},
	}

	_, err := r.InvokeSkill(context.Background(), "worker", "work")
	if err == nil {
		t.Fatal("a skill named after a built-in mode ran without complaint")
	}
	if !strings.Contains(err.Error(), "worker") || !strings.Contains(err.Error(), "built-in") {
		t.Errorf("err = %q, want it to name the skill and the clash", err.Error())
	}
}
