package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
)

// writeYAML writes a workflow YAML to <root>/.orchestra/workflows/<name>.
func writeYAML(t *testing.T, root, name, body string) {
	t.Helper()
	dir := filepath.Join(root, ".orchestra", "workflows")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeSkill writes a skill markdown file to <root>/.orchestra/skills/.
func writeSkill(t *testing.T, root, name, body string) {
	t.Helper()
	dir := filepath.Join(root, ".orchestra", "skills")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// isolateHome redirects os.UserHomeDir to a fresh tempdir so that the
// developer's populated ~/.orchestra/ doesn't bleed into these tests.
func isolateHome(t *testing.T) {
	t.Helper()
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
}

func TestRPC_WorkflowList_ReturnsDiscoveredWorkflows(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	writeSkill(t, root, "x.md", "---\nname: x\ndescription: X\n---\nbody\n")
	writeYAML(t, root, "demo.yaml",
		"name: demo\ndescription: Demo wf\nstages:\n  - id: a\n    skill: x\n  - id: b\n    skill: x\n    depends_on: [a]\n")

	_, h := setupInitializedCore(t, root, &fixedLLM{})
	out, err := h.Handle(context.Background(), "workflow.list", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("workflow.list: %v", err)
	}
	res, ok := out.(*WorkflowListResult)
	if !ok {
		t.Fatalf("unexpected result type %T", out)
	}
	if len(res.Workflows) != 1 || res.Workflows[0].Name != "demo" {
		t.Fatalf("got %+v", res.Workflows)
	}
	if len(res.Workflows[0].Stages) != 2 ||
		res.Workflows[0].Stages[0] != "a" ||
		res.Workflows[0].Stages[1] != "b" {
		t.Fatalf("stages: %v", res.Workflows[0].Stages)
	}
	if res.Workflows[0].Description != "Demo wf" {
		t.Fatalf("description: %q", res.Workflows[0].Description)
	}
}

func TestRPC_SkillList_ReturnsDiscoveredSkills(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	writeSkill(t, root, "alpha.md",
		"---\nname: alpha\ndescription: Alpha skill\ntools: [read, glob]\ncompletion_markers: [\"## DONE\"]\n---\nbody alpha\n")
	writeSkill(t, root, "beta.md",
		"---\nname: beta\ndescription: Beta skill\n---\nbody beta\n")

	_, h := setupInitializedCore(t, root, &fixedLLM{})
	out, err := h.Handle(context.Background(), "skill.list", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("skill.list: %v", err)
	}
	res, ok := out.(*SkillListResult)
	if !ok {
		t.Fatalf("unexpected result type %T", out)
	}
	if len(res.Skills) != 2 {
		t.Fatalf("expected 2 skills, got %d: %+v", len(res.Skills), res.Skills)
	}
	// Sorted alphabetically by name.
	if res.Skills[0].Name != "alpha" || res.Skills[1].Name != "beta" {
		t.Fatalf("order: %v / %v", res.Skills[0].Name, res.Skills[1].Name)
	}
	if len(res.Skills[0].Tools) != 2 || res.Skills[0].Tools[0] != "read" {
		t.Fatalf("tools: %v", res.Skills[0].Tools)
	}
	if len(res.Skills[0].CompletionMarkers) != 1 || res.Skills[0].CompletionMarkers[0] != "## DONE" {
		t.Fatalf("markers: %v", res.Skills[0].CompletionMarkers)
	}
}

func TestRPC_WorkflowRun_UnknownWorkflowReturnsError(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	_, h := setupInitializedCore(t, root, &fixedLLM{})
	p, _ := json.Marshal(WorkflowRunParams{Name: "does-not-exist", Arguments: "x"})
	_, err := h.Handle(context.Background(), "workflow.run", p)
	if err == nil {
		t.Fatal("expected error for unknown workflow")
	}
}

func TestRPC_SkillInvoke_UnknownSkillReturnsError(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	_, h := setupInitializedCore(t, root, &fixedLLM{})
	p, _ := json.Marshal(SkillInvokeParams{Name: "does-not-exist", Arguments: "x"})
	_, err := h.Handle(context.Background(), "skill.invoke", p)
	if err == nil {
		t.Fatal("expected error for unknown skill")
	}
}

func TestRPC_WorkflowList_RequiresInitialize(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	cfg := config.DefaultConfig(root)
	if err := config.Save(filepath.Join(root, ".orchestra.yml"), cfg); err != nil {
		t.Fatal(err)
	}
	c, err := New(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	h := NewRPCHandler(c)
	_, err = h.Handle(context.Background(), "workflow.list", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("workflow.list should require initialize")
	}
}
