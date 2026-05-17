package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/llm"
)

func TestToolSkillInvoke_SchemaHasEnum(t *testing.T) {
	def := ToolSkillInvoke([]string{"refactor", "review"})
	if def.Function.Name != "skill_invoke" {
		t.Fatalf("name: %q", def.Function.Name)
	}
	if def.Type != "function" {
		t.Errorf("type: %q", def.Type)
	}
	var schema map[string]any
	if err := json.Unmarshal(def.Function.Parameters, &schema); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties missing")
	}
	skillProp := props["skill"].(map[string]any)
	enum, ok := skillProp["enum"].([]any)
	if !ok {
		t.Fatal("enum missing for skill")
	}
	if len(enum) != 2 {
		t.Errorf("enum len: %d", len(enum))
	}
	if _, ok := props["task"]; !ok {
		t.Error("task property missing")
	}
}

func TestToolSkillInvoke_NoNamesNoEnum(t *testing.T) {
	def := ToolSkillInvoke(nil)
	if !strings.Contains(string(def.Function.Parameters), `"skill"`) {
		t.Error("skill prop missing")
	}
	if strings.Contains(string(def.Function.Parameters), `"enum"`) {
		t.Error("enum should be absent when no names supplied")
	}
}

func TestToolSkillInvoke_GetsMutatingFlag(t *testing.T) {
	defs := applyParallelFlags([]llm.ToolDef{ToolSkillInvoke([]string{"x"})})
	if !defs[0].Mutating {
		t.Error("skill_invoke should be marked Mutating")
	}
	if defs[0].ParallelSafe {
		t.Error("skill_invoke should not be ParallelSafe")
	}
}
