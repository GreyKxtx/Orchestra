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
	"time"

	"github.com/orchestra/orchestra/internal/checkpoint"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/llm"
)

// resumeLLM plays the run: the root spawns A and B and waits for both; A
// edits a.txt; B edits b.txt — or, while hang is set, never answers, as a
// child does when its core is killed under it.
type resumeLLM struct {
	mu      sync.Mutex
	hang    bool
	calls   map[string]int
	bCalled chan struct{}
	once    sync.Once
	rootSaw []string
	// lastRootTools is the tool results the root last saw.
	lastRootTools string
}

var taskIDRe = regexp.MustCompile(`"task_id":"(task_[0-9_]+)"`)

func (s *resumeLLM) Plan(context.Context, string) (string, error) { return "{}", nil }

func (s *resumeLLM) Complete(ctx context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	var said strings.Builder
	steps := 0
	for _, m := range req.Messages {
		switch m.Role {
		case llm.RoleUser:
			said.WriteString(m.Content)
		case llm.RoleAssistant:
			steps++
		}
	}
	who := "root"
	switch {
	case strings.Contains(said.String(), "A-GOAL"):
		who = "A"
	case strings.Contains(said.String(), "B-GOAL"):
		who = "B"
	}
	s.mu.Lock()
	s.calls[who]++
	hang := s.hang
	if who == "root" {
		s.rootSaw = append(s.rootSaw, said.String())
	}
	s.mu.Unlock()

	switch who {
	case "A", "B":
		file, from, to := "a.txt", "a0", "a1"
		if who == "B" {
			file, from, to = "b.txt", "b0", "b1"
			if hang {
				s.once.Do(func() { close(s.bCalled) })
				<-ctx.Done()
				return nil, ctx.Err()
			}
		}
		switch steps {
		case 0:
			return toolCall("read", `{"path":"`+file+`"}`), nil
		case 1:
			return toolCall("edit", `{"path":"`+file+`","search":"`+from+`","replace":"`+to+`"}`), nil
		default:
			return toolCall("task_result", `{"content":"`+who+` done"}`), nil
		}
	}

	// The root: spawn, wait for what it spawned, finish.
	var ids []string
	seen := map[string]bool{}
	var tools strings.Builder
	for _, m := range req.Messages {
		if m.Role != llm.RoleTool {
			continue
		}
		tools.WriteString(m.Content)
		for _, match := range taskIDRe.FindAllStringSubmatch(m.Content, -1) {
			if !seen[match[1]] {
				seen[match[1]] = true
				ids = append(ids, match[1])
			}
		}
	}
	s.mu.Lock()
	s.lastRootTools = tools.String()
	s.mu.Unlock()
	switch steps {
	case 0:
		return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			{ID: "spawn-a", Type: "function", Function: llm.ToolCallFunc{Name: "task_spawn", Arguments: llm.ToolArguments(`{"subagent_type":"general","goal":"A-GOAL: change a0 to a1 in a.txt"}`)}},
			{ID: "spawn-b", Type: "function", Function: llm.ToolCallFunc{Name: "task_spawn", Arguments: llm.ToolArguments(`{"subagent_type":"general","goal":"B-GOAL: change b0 to b1 in b.txt"}`)}},
		}}}, nil
	case 1:
		raw, _ := json.Marshal(map[string]any{"task_ids": ids, "timeout_ms": 60_000})
		return toolCall("task_wait", string(raw)), nil
	default:
		return finalText("both files changed"), nil
	}
}

func resumeWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range map[string]string{"a.txt": "a0\n", "b.txt": "b0\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := config.Save(filepath.Join(root, ".orchestra.yml"), config.DefaultConfig(root)); err != nil {
		t.Fatal(err)
	}
	return root
}

// The crash point: the root has spawned both tasks and is waiting, A has
// finished and committed its edit, B is still running. Returns the
// checkpoint as it was on disk then.
func awaitCrashPoint(t *testing.T, root string, client *resumeLLM) (string, []byte) {
	t.Helper()
	select {
	case <-client.bCalled:
	case <-time.After(20 * time.Second):
		t.Fatal("B never started")
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		cp, err := checkpoint.Latest(root)
		if err == nil && len(cp.History) >= 2 && strings.Contains(string(cp.Tasks), `"A done"`) && len(cp.Staged) == 1 {
			data, err := os.ReadFile(checkpoint.Path(root, cp.RunID))
			if err == nil {
				return cp.RunID, data
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the checkpoint never showed A finished while B ran")
	return "", nil
}

// kill -9 of the core in the middle of a turn, then resume from the same
// graph (phase 4 acceptance). The first core is abandoned at the crash point
// and the checkpoint is put back as it was on disk at that moment — nothing
// the dying run did after it counts. A second core resumes the run: the
// staged edit of the task that had finished comes back, that task does not
// run again, the interrupted one starts again under the id the root is
// waiting on, and the turn ends with both edits.
func TestResume_AfterACrashTheTurnGoesOnFromTheSameGraph(t *testing.T) {
	root := resumeWorkspace(t)

	first := &resumeLLM{hang: true, calls: map[string]int{}, bCalled: make(chan struct{})}
	c1, err := New(root, Options{LLMClient: first})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := c1.AgentRun(ctx, AgentRunParams{Query: "change both files", Mode: "build"})
		done <- err
	}()
	runID, atCrash := awaitCrashPoint(t, root, first)

	// The process dies here. What it would still have done is undone by
	// putting back the checkpoint as it was.
	cancel()
	<-done
	c1.Close()
	if err := os.WriteFile(checkpoint.Path(root, runID), atCrash, 0o600); err != nil {
		t.Fatal(err)
	}

	second := &resumeLLM{calls: map[string]int{}, bCalled: make(chan struct{})}
	c2, err := New(root, Options{LLMClient: second})
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	res, err := c2.AgentRun(context.Background(), AgentRunParams{Resume: "last"})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if res.RunID != runID {
		t.Fatalf("the resumed run is the same run: %s, want %s", res.RunID, runID)
	}
	if second.calls["A"] != 0 {
		t.Fatalf("A had finished; it must not run again (%d calls)", second.calls["A"])
	}
	if second.calls["B"] == 0 {
		t.Fatal("B was interrupted; it must run again")
	}
	if len(second.rootSaw) == 0 || !strings.Contains(second.rootSaw[0], "<resume_notice>") {
		t.Fatalf("the root is told the turn was resumed: %q", second.rootSaw)
	}
	if !strings.Contains(second.lastRootTools, "A done") || !strings.Contains(second.lastRootTools, "B done") {
		t.Fatalf("the root's task_wait gets A's kept result and B's new one: %s", second.lastRootTools)
	}
	got := map[string]bool{}
	for _, op := range res.Ops {
		if op.WriteAtomic != nil {
			got[op.Path+"="+op.WriteAtomic.Content] = true
		}
	}
	if !got["a.txt=a1\n"] || !got["b.txt=b1\n"] {
		t.Fatalf("the turn ends with both edits — A's restored, B's redone: %v", got)
	}
	cp, err := checkpoint.Load(root, runID)
	if err != nil || cp.Status != checkpoint.StatusDone || cp.Resumes != 1 {
		t.Fatalf("the checkpoint says the run finished after one resume: %+v %v", cp, err)
	}
	if _, err := c2.AgentRun(context.Background(), AgentRunParams{Resume: runID}); err == nil {
		t.Fatal("a finished run cannot be resumed")
	}
}

// A resumed run takes its query and mode from the checkpoint, and its consent
// only from the request: a checkpoint is a file in the project.
func TestResume_ConsentComesFromTheRequest(t *testing.T) {
	p := resumableParams{Query: "q", Mode: "build", Apply: true}
	got := p.applyTo(AgentRunParams{Query: "other", AllowExec: false, AllowWeb: true, Attachments: []MessageAttachment{{Path: "x.png"}}})
	if got.Query != "q" || got.Mode != "build" || !got.Apply || got.AllowExec || !got.AllowWeb || got.Attachments != nil {
		t.Fatalf("applyTo: %+v", got)
	}
}
