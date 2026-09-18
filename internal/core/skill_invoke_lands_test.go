package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/llm"
)

// Does a skill's edit go anywhere?
//
// skill_invoke is the last open gap in docs/coverage-map.md, and the eval task
// built for it (skill_does_the_work) has never exercised it: every model tried
// so far — a local 9B, a Gemini flash, an OpenRouter free model — makes the
// edit itself and never calls the tool, so the task goes red for a reason that
// says nothing about whether the machinery works.
//
// The machinery does not need a model that chooses well to be testable. This
// drives the real core with a scripted parent that does call skill_invoke, and
// a scripted child that behaves like the skill's body asks, then checks the
// file on disk. The parent emits no patches of its own, so a changed file is
// the child's work and nothing else.

const bumperSkillBody = `---
name: bumper
description: Raise a numeric constant in Go source to a requested value.
tools: [read, glob, grep, edit, write]
---

Raise the constant named in the task, change nothing else. Task: $ARGUMENTS
`

const limitsGo = `package main

// retryLimit is how many times we retry a failed call.
const retryLimit = 3
`

var hashPattern = regexp.MustCompile(`sha256:[0-9a-f]{64}`)

// skillScriptLLM answers as the parent or as the skill child, telling them
// apart by the tool list: only the parent is offered skill_invoke.
type skillScriptLLM struct {
	mu            sync.Mutex
	parentCalls   int
	childCalls    int
	parentPatched bool
	// What the parent read back from skill_invoke: the tool message in its
	// second request. This is the whole of what the parent knows about the
	// child's work, so the test reads it too.
	parentSaw string
}

func (s *skillScriptLLM) Plan(context.Context, string) (string, error) { return "{}", nil }

func toolCall(name, args string) *llm.CompleteResponse {
	return &llm.CompleteResponse{Message: llm.Message{
		Role: llm.RoleAssistant,
		ToolCalls: []llm.ToolCall{{
			ID: "call-" + name, Type: "function",
			Function: llm.ToolCallFunc{Name: name, Arguments: llm.ToolArguments(args)},
		}},
	}}
}

func finalText(text string) *llm.CompleteResponse {
	body, _ := json.Marshal(map[string]any{
		"type":  "final",
		"final": map[string]any{"summary": text, "patches": []any{}},
	})
	return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: string(body)}}
}

func offersSkillInvoke(req llm.CompleteRequest) bool {
	for _, d := range req.Tools {
		if d.Function.Name == "skill_invoke" {
			return true
		}
	}
	return false
}

// lastFileHash finds the hash the read tool reported, which edit must echo.
func lastFileHash(req llm.CompleteRequest) string {
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if m := hashPattern.FindString(req.Messages[i].Content); m != "" {
			return m
		}
	}
	return ""
}

func (s *skillScriptLLM) Complete(_ context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if offersSkillInvoke(req) {
		s.parentCalls++
		if s.parentCalls == 1 {
			return toolCall("skill_invoke", `{"skill":"bumper","task":"raise retryLimit to 5"}`), nil
		}
		for i := len(req.Messages) - 1; i >= 0; i-- {
			if req.Messages[i].Role == llm.RoleTool {
				s.parentSaw = req.Messages[i].Content
				break
			}
		}
		// The parent never edits: whatever is on disk afterwards is the child's.
		return finalText("the bumper skill raised retryLimit"), nil
	}

	s.childCalls++
	switch s.childCalls {
	case 1:
		return toolCall("read", `{"path":"limits.go"}`), nil
	case 2:
		hash := lastFileHash(req)
		args := `{"path":"limits.go","search":"const retryLimit = 3","replace":"const retryLimit = 5","file_hash":"` + hash + `"}`
		return toolCall("edit", args), nil
	default:
		return finalText("raised retryLimit to 5"), nil
	}
}

func TestSkillInvoke_TheChildsEditReachesTheFile(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, ".orchestra", "skills")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "bumper.md"), []byte(bumperSkillBody), 0o644); err != nil {
		t.Fatal(err)
	}
	limitsPath := filepath.Join(root, "limits.go")
	if err := os.WriteFile(limitsPath, []byte(limitsGo), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig(root)
	if err := config.Save(filepath.Join(root, ".orchestra.yml"), cfg); err != nil {
		t.Fatal(err)
	}

	client := &skillScriptLLM{}
	c, err := New(root, Options{LLMClient: client})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if _, err := c.AgentRun(context.Background(), AgentRunParams{
		Query: "use the bumper skill to raise retryLimit to 5",
		Mode:  "build",
		Apply: true,
	}); err != nil {
		t.Fatalf("agent.run: %v", err)
	}

	if client.childCalls == 0 {
		t.Fatal("skill_invoke never reached a child: the parent's tool call did not start one")
	}
	body, err := os.ReadFile(limitsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "retryLimit = 5") {
		t.Errorf("the child's edit never reached the file:\n%s", body)
	}

	// The parent has to be TOLD the edit happened. The child works through
	// edit and closes with prose, so it produces no final.patches; the report
	// used to say "completed with 0 patch(es)" for exactly this run, and a
	// real parent (the 27B, live) read that as "nothing happened" and invoked
	// the skill again — eight times, each bump compounding the last.
	saw := client.parentSaw
	if saw == "" {
		t.Fatal("the parent's second request carried no tool message from skill_invoke")
	}
	if strings.Contains(saw, "0 patch(es)") {
		t.Errorf("skill_invoke reported no work to the parent after the child edited the file:\n%s", saw)
	}
	if !strings.Contains(saw, "limits.go") {
		t.Errorf("skill_invoke's report does not name the file the child edited:\n%s", saw)
	}
	if !strings.Contains(saw, "raised retryLimit to 5") {
		t.Errorf("skill_invoke's report does not carry the child's closing words:\n%s", saw)
	}
}
