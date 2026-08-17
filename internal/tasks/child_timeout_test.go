package tasks

import (
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
)

func TestChildTimeout_HungBashCancelsWithoutLeak(t *testing.T) {
	cmd := "sleep"
	args := []string{"60"}
	if runtime.GOOS == "windows" {
		cmd = "ping"
		args = []string{"127.0.0.1", "-n", "60"}
	}
	input, _ := json.Marshal(map[string]any{
		"command":    cmd,
		"args":       args,
		"timeout_ms": 60_000,
	})
	mock := &oneShotToolLLM{name: "bash", args: input}
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	tr, err := tools.NewRunner(t.TempDir(), tools.RunnerOptions{ExecTimeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	r := New(mock, v, tr, ChildAgentConfig{Caps: tools.Capabilities{Exec: true}})
	t.Cleanup(func() {
		r.Close()
		_ = tr.Close()
	})

	before := runtime.NumGoroutine()
	id, err := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{
		Goal:         "hang on bash",
		SubagentType: "explore",
		MaxSteps:     2,
		TimeoutMS:    200,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	res, err := r.Wait(context.Background(), id, 5_000)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if res.Status != "timeout" && res.Status != "cancelled" && res.Status != "error" {
		t.Fatalf("status=%q error=%q, want timeout/cancel after hung bash", res.Status, res.Error)
	}

	deadline := time.Now().Add(2 * time.Second)
	var after int
	for time.Now().Before(deadline) {
		after = runtime.NumGoroutine()
		if after <= before+8 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	if after > before+12 {
		t.Fatalf("goroutine leak: before=%d after=%d", before, after)
	}
}

func TestChildTimeout_ReapsZombieIgnoringContext(t *testing.T) {
	prev := childReapTimeout
	childReapTimeout = 80 * time.Millisecond
	t.Cleanup(func() { childReapTimeout = prev })

	mock := &ignoreCtxLLM{started: make(chan struct{})}
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	tr, err := tools.NewRunner(t.TempDir(), tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	r := New(mock, v, tr, ChildAgentConfig{})
	t.Cleanup(func() {
		r.Close()
		_ = tr.Close()
	})

	id, err := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{
		Goal:      "ignore cancel",
		MaxSteps:  1,
		TimeoutMS: 30,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	select {
	case <-mock.started:
	case <-time.After(2 * time.Second):
		t.Fatal("LLM never entered Complete")
	}

	start := time.Now()
	res, err := r.Wait(context.Background(), id, 40)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if elapsed > 1500*time.Millisecond {
		t.Fatalf("Wait blocked %s; reap timeout should bound zombie wait", elapsed)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	if res.Status == "done" {
		t.Fatalf("expected zombie/timeout result, got %+v", res)
	}
	if !strings.Contains(res.Error, "did not exit") && res.Status != "timeout" && res.Status != "error" {
		t.Fatalf("expected reap/timeout error, got %+v", res)
	}
}

type oneShotToolLLM struct {
	name string
	args json.RawMessage
	n    int
	mu   sync.Mutex
}

func (m *oneShotToolLLM) Plan(_ context.Context, _ string) (string, error) { return "", nil }

func (m *oneShotToolLLM) Complete(ctx context.Context, _ llm.CompleteRequest) (*llm.CompleteResponse, error) {
	m.mu.Lock()
	m.n++
	n := m.n
	m.mu.Unlock()
	if n > 1 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: `{"type":"final","final":{"patches":[]}}`}}, nil
	}
	return &llm.CompleteResponse{
		Message: llm.Message{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{{
				ID:       "hang",
				Type:     "function",
				Function: llm.ToolCallFunc{Name: m.name, Arguments: llm.ToolArguments(m.args)},
			}},
		},
	}, nil
}

type ignoreCtxLLM struct {
	started chan struct{}
	once    sync.Once
}

func (m *ignoreCtxLLM) Plan(_ context.Context, _ string) (string, error) { return "", nil }

func (m *ignoreCtxLLM) Complete(_ context.Context, _ llm.CompleteRequest) (*llm.CompleteResponse, error) {
	m.once.Do(func() { close(m.started) })
	time.Sleep(2 * time.Second)
	return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: `{"type":"final","final":{"patches":[]}}`}}, nil
}
