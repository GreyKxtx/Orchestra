package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
)

// readThenBadFinal alternates a read with a final whose patch cannot apply.
type readThenBadFinal struct{ calls int }

func (c *readThenBadFinal) Plan(context.Context, string) (string, error) { return "{}", nil }
func (c *readThenBadFinal) Complete(context.Context, llm.CompleteRequest) (*llm.CompleteResponse, error) {
	c.calls++
	if c.calls%2 == 1 {
		return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{toolCall("r", "read", `{"path":"a.go"}`)}}}, nil
	}
	return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: `{"patches":[` +
		`{"type":"file.search_replace","path":"a.go","search":"func Missing() {}","replace":"func Found() {}"}]}`}}, nil
}

// LLM-9: every successful call reset the failed-final counter, so a model
// that re-read the file between failed finals never tripped MaxFinalFailures
// and looped until MaxSteps. A read is not progress; it trips now.
func TestFinalBreaker_AReadDoesNotForgiveAFailedFinal(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	client := &readThenBadFinal{}
	ag, err := New(client, v, tr, Options{Mode: ModeBuild, MaxSteps: 40, MaxFinalFailures: 2})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = ag.Run(context.Background(), nil, "rename Missing")
	if err == nil || !strings.Contains(err.Error(), "repeatedly") {
		t.Fatalf("the loop was not stopped by the final breaker: %v", err)
	}
	if client.calls > 10 {
		t.Fatalf("%d model calls: the breaker should trip after three failed finals", client.calls)
	}
}

// alwaysPatchesCode is a lead that keeps finishing with a patch to
// production code.
type alwaysPatchesCode struct{ calls int }

func (c *alwaysPatchesCode) Plan(context.Context, string) (string, error) { return "{}", nil }
func (c *alwaysPatchesCode) Complete(context.Context, llm.CompleteRequest) (*llm.CompleteResponse, error) {
	c.calls++
	return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: `{"patches":[` +
		`{"type":"file.write_atomic","path":"main.go","content":"package main\n"}]}`}}, nil
}

// A refused final did not count at all: a lead that kept patching production
// code was refused on every step until MaxSteps.
func TestFinalBreaker_RefusedFinalsCount(t *testing.T) {
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	tr, err := tools.NewRunner(t.TempDir(), tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	client := &alwaysPatchesCode{}
	ag, err := New(client, v, tr, Options{Mode: ModeOrchestra, MaxSteps: 40, MaxFinalFailures: 2})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = ag.Run(context.Background(), nil, "ship it")
	if err == nil || !strings.Contains(err.Error(), "repeatedly") {
		t.Fatalf("the refused finals were not stopped by the breaker: %v (%d calls)", err, client.calls)
	}
	if client.calls > 5 {
		t.Fatalf("%d model calls for a lead refused every time", client.calls)
	}
}
