package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
)

// Read-only calls in one assistant message run on the parallel path, which
// logged the call and not the result. So llm_log.jsonl said a read happened
// and never whether it answered, failed, or came back empty — and reads are
// most of a turn.
//
// That gap cost real time. Diagnosing the two_files data loss, the trace read:
//
//	{"event":"tool_call","tool_name":"read","input_bytes":18}
//	{"event":"tool_call","tool_name":"read","input_bytes":18}
//	{"event":"tool_call","tool_name":"write",…}
//
// Two reads, no results, no way to tell from the file what the model had been
// given. Recovering it needed a logging proxy in front of the model server.

// parallelLLM answers once with two tool calls in a single message, then
// finishes — the shape that reaches runParallelToolBatch.
type parallelLLM struct {
	calls []llm.ToolCall
	i     int
}

func (p *parallelLLM) Plan(ctx context.Context, prompt string) (string, error) {
	_, _ = ctx, prompt
	return "{}", nil
}

func (p *parallelLLM) Complete(ctx context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	_, _ = ctx, req
	p.i++
	if p.i == 1 {
		return &llm.CompleteResponse{Message: llm.Message{
			Role:      llm.RoleAssistant,
			ToolCalls: p.calls,
		}}, nil
	}
	return &llm.CompleteResponse{Message: llm.Message{
		Role:    llm.RoleAssistant,
		Content: `{"patches":[]}`,
	}}, nil
}

func TestParallelBatch_ResultsAndErrorsReachTheLog(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "util.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { tr.Close() })

	client := &parallelLLM{calls: []llm.ToolCall{
		{ID: "c1", Type: "function", Function: llm.ToolCallFunc{
			Name: "read", Arguments: llm.ToolArguments(`{"path":"util.go"}`)}},
		// A path that is not there, so the batch carries one success and one
		// failure — a log that records only the happy one is no better than
		// a log that records neither.
		{ID: "c2", Type: "function", Function: llm.ToolCallFunc{
			Name: "read", Arguments: llm.ToolArguments(`{"path":"missing.go"}`)}},
	}}

	ag, err := New(client, v, tr, Options{
		MaxSteps:    6,
		AgentLogger: llm.NewLogger(root),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, _, err := ag.Run(context.Background(), nil, "read the files"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, ".orchestra", "llm_log.jsonl"))
	if err != nil {
		t.Fatalf("the run wrote no log at all: %v", err)
	}

	var calls, okResults, errResults int
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var e struct {
			Event    string `json:"event"`
			ToolName string `json:"tool_name"`
			ErrorStr string `json:"error"`
		}
		if json.Unmarshal([]byte(line), &e) != nil || e.ToolName != "read" {
			continue
		}
		switch {
		case e.Event == "tool_call":
			calls++
		case e.Event == "tool_result" && e.ErrorStr == "":
			okResults++
		case e.Event == "tool_result":
			errResults++
		}
	}

	if calls != 2 {
		t.Fatalf("expected both parallel reads to be logged as calls, got %d", calls)
	}
	if okResults != 1 {
		t.Errorf("the read that succeeded left no result in the log, so a trace cannot "+
			"show what the model was given (got %d)", okResults)
	}
	if errResults != 1 {
		t.Errorf("the read that failed left no result in the log, so a trace cannot "+
			"show that the model was given nothing (got %d)", errResults)
	}
}
