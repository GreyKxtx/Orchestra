package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
)

// The eval task skill_does_the_work fails by the parent making the edit
// itself, and nothing said whether the model was even offered skill_invoke —
// the CLI (`orchestra apply`) and the core wire skills through different
// code, and the TUI, VS Code and the eval all run on the core.
//
// This pins the core side: a skill in the workspace reaches the turn's tool
// list, so a failing eval is the model's choice, not a missing tool.

const bumperSkill = `---
name: bumper
description: Raise a numeric constant in Go source to a requested value.
tools: [read, glob, grep, edit, write]
---

Raise the constant named in the task. Task: $ARGUMENTS
`

func newCoreWithSkill(t *testing.T, client *toolListLLM) *Core {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, ".orchestra", "skills")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bumper.md"), []byte(bumperSkill), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig(root)
	if err := config.Save(filepath.Join(root, ".orchestra.yml"), cfg); err != nil {
		t.Fatal(err)
	}
	c, err := New(root, Options{LLMClient: client})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestChat_ASkillInTheWorkspaceReachesTheTurnsToolList(t *testing.T) {
	client := &toolListLLM{}
	c := newCoreWithSkill(t, client)

	if _, err := c.AgentRun(context.Background(), AgentRunParams{Query: "raise retryLimit to 5", Mode: "build"}); err != nil {
		t.Fatal(err)
	}
	if !client.offers("skill_invoke") {
		t.Error("a skill is in the workspace and the turn was not offered skill_invoke")
	}
}

// session.message is the other door into the same turn — the one the TUI and
// VS Code actually use.
func TestSessionMessage_ASkillReachesTheTurnsToolList(t *testing.T) {
	client := &toolListLLM{}
	c := newCoreWithSkill(t, client)

	started, err := c.SessionStart(SessionStartParams{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.SessionMessage(context.Background(), SessionMessageParams{
		SessionID: started.SessionID, Content: "raise retryLimit to 5", Mode: "build",
	}); err != nil {
		t.Fatal(err)
	}
	if !client.offers("skill_invoke") {
		t.Error("session.message did not offer skill_invoke with a skill in the workspace")
	}
}

// Nothing to delegate to, nothing to offer: the tool would name skills that
// do not exist.
func TestChat_NoSkillsMeansNoSkillInvoke(t *testing.T) {
	client := &toolListLLM{}
	c := newChatCore(t, client)

	if _, err := c.AgentRun(context.Background(), AgentRunParams{Query: "raise retryLimit to 5", Mode: "build"}); err != nil {
		t.Fatal(err)
	}
	if client.offers("skill_invoke") {
		t.Error("skill_invoke offered in a workspace with no skills")
	}
}

// A read-only mode must not gain a writing child through the back door.
func TestChat_AskModeIsNotOfferedSkillInvoke(t *testing.T) {
	client := &toolListLLM{}
	c := newCoreWithSkill(t, client)

	if _, err := c.AgentRun(context.Background(), AgentRunParams{Query: "what is retryLimit?", Mode: "ask"}); err != nil {
		t.Fatal(err)
	}
	if client.offers("skill_invoke") {
		t.Error("ask mode was offered skill_invoke; a skill child can write")
	}
}
