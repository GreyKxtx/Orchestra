package core

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/trajectory"
	"github.com/orchestra/orchestra/llm"
)

// A run's journal is enough to rebuild what it delegated to whom.
//
// The root of an agent.run hands a goal to a general subagent, which hands a
// narrower one to an explore subagent. Afterwards nothing but the files the
// run left behind is read: .orchestra/runs/<turn_id>.events.jsonl, which says
// which task started which and at what depth, and llm_log.jsonl, where every
// line of the three agents says whose it is. Before the journal carried run
// and task ids, the three agents' lines interleaved in one anonymous stream,
// and agent.run kept no event log at all.

const (
	journalLevel1 = "LEVEL1-GOAL: survey the repository"
	journalLevel2 = "LEVEL2-GOAL: find the entry point"
)

// treeScriptLLM plays the three agents, telling them apart by the goal in
// their user messages.
type treeScriptLLM struct {
	mu    sync.Mutex
	calls map[string]int
}

func (s *treeScriptLLM) Plan(context.Context, string) (string, error) { return "{}", nil }

func (s *treeScriptLLM) who(req llm.CompleteRequest) string {
	var said strings.Builder
	for _, m := range req.Messages {
		if m.Role == llm.RoleUser {
			said.WriteString(m.Content)
		}
	}
	switch {
	case strings.Contains(said.String(), journalLevel2):
		return "explore"
	case strings.Contains(said.String(), journalLevel1):
		return "general"
	}
	return "root"
}

func (s *treeScriptLLM) Complete(_ context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	who := s.who(req)
	s.calls[who]++
	first := s.calls[who] == 1
	switch who {
	case "root":
		if first {
			return toolCall("task", `{"subagent_type":"general","goal":"`+journalLevel1+`"}`), nil
		}
		return finalText("the survey is done"), nil
	case "general":
		if first {
			return toolCall("task", `{"subagent_type":"explore","goal":"`+journalLevel2+`"}`), nil
		}
		return finalText("surveyed; the entry point is main.go"), nil
	default:
		return finalText("the entry point is main.go"), nil
	}
}

type journalTask struct {
	parent  string
	depth   int
	started int
	done    int
}

func TestRunJournal_RebuildsTheDelegationTree(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig(root)
	// A flow turns the agency on in build mode and lets general delegate on.
	cfg.Agency.Flows = []string{"general > explore"}
	if err := config.Save(filepath.Join(root, ".orchestra.yml"), cfg); err != nil {
		t.Fatal(err)
	}

	client := &treeScriptLLM{calls: map[string]int{}}
	c, err := New(root, Options{LLMClient: client})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.AgentRun(context.Background(), AgentRunParams{
		Query: "survey the repository, delegating as you see fit",
		Mode:  "build",
	}); err != nil {
		t.Fatalf("agent.run: %v", err)
	}
	if client.calls["general"] == 0 || client.calls["explore"] == 0 {
		t.Fatalf("the run did not delegate two levels deep: %v", client.calls)
	}

	// 1. The run's event log.
	logs, _ := filepath.Glob(filepath.Join(root, ".orchestra", "runs", "*.events.jsonl"))
	if len(logs) != 1 {
		t.Fatalf("agent.run must leave exactly one run log, found %v", logs)
	}
	runID := strings.TrimSuffix(filepath.Base(logs[0]), ".events.jsonl")
	events, recorded, err := trajectory.ReadRun(root, runID)
	if err != nil || !recorded {
		t.Fatalf("ReadRun: recorded=%v err=%v", recorded, err)
	}
	if len(events) < 2 || events[0].Type != trajectory.TypeTurnStart || events[len(events)-1].Type != trajectory.TypeTurnEnd {
		t.Fatalf("the log must open and close with the turn boundaries: %d events", len(events))
	}

	tasks := map[string]*journalTask{}
	for _, ev := range events {
		if ev.Type != "agent/event" {
			continue
		}
		var d struct {
			Type         string `json:"type"`
			TaskID       string `json:"task_id"`
			ParentTaskID string `json:"parent_task_id"`
			Depth        int    `json:"depth"`
			TurnID       string `json:"turn_id"`
		}
		if err := json.Unmarshal(ev.Data, &d); err != nil {
			t.Fatal(err)
		}
		if d.Type != "child_started" && d.Type != "child_done" {
			continue
		}
		if d.TurnID != runID {
			t.Errorf("%s for %s carries turn %q, want %q", d.Type, d.TaskID, d.TurnID, runID)
		}
		tk := tasks[d.TaskID]
		if tk == nil {
			tk = &journalTask{parent: d.ParentTaskID, depth: d.Depth}
			tasks[d.TaskID] = tk
		}
		if tk.parent != d.ParentTaskID || tk.depth != d.Depth {
			t.Errorf("%s: started and done disagree on the parent or depth", d.TaskID)
		}
		if d.Type == "child_started" {
			tk.started++
		} else {
			if tk.started == 0 {
				t.Errorf("%s: child_done before child_started", d.TaskID)
			}
			tk.done++
		}
	}
	if len(tasks) != 2 {
		t.Fatalf("want two tasks in the log, got %d", len(tasks))
	}
	var top, leaf string
	for id, tk := range tasks {
		if tk.started != 1 || tk.done != 1 {
			t.Errorf("%s: started %d times, done %d times; want once each", id, tk.started, tk.done)
		}
		switch tk.depth {
		case 1:
			top = id
		case 2:
			leaf = id
		}
	}
	if top == "" || leaf == "" {
		t.Fatalf("want depths 1 and 2, got %+v", tasks)
	}
	if tasks[top].parent != "" {
		t.Errorf("the depth-1 task was started by the root, not %q", tasks[top].parent)
	}
	if tasks[leaf].parent != top {
		t.Errorf("the depth-2 task was started by %q, want %q", tasks[leaf].parent, top)
	}

	// 2. llm_log.jsonl: every line of the run says which of the three wrote it.
	f, err := os.Open(filepath.Join(root, ".orchestra", "llm_log.jsonl"))
	if err != nil {
		t.Fatalf("the run left no llm_log: %v", err)
	}
	defer f.Close()
	byTask := map[string][]string{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var e llm.LLMLogEntry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatal(err)
		}
		if e.RunID != runID {
			t.Errorf("%s line (task %q) carries run %q, want %q", e.Event, e.TaskID, e.RunID, runID)
			continue
		}
		want := llm.Trace{RunID: runID}
		switch e.TaskID {
		case "":
		case top:
			want = llm.Trace{RunID: runID, TaskID: top, Depth: 1}
		case leaf:
			want = llm.Trace{RunID: runID, TaskID: leaf, ParentTaskID: top, Depth: 2}
		default:
			t.Errorf("%s line from a task the event log never started: %q", e.Event, e.TaskID)
			continue
		}
		if e.Trace != want {
			t.Errorf("%s line: trace %+v, want %+v", e.Event, e.Trace, want)
		}
		if e.Event == "tool_call" {
			byTask[e.TaskID] = append(byTask[e.TaskID], e.ToolName)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	for _, who := range []string{"", top} {
		calls := byTask[who]
		sort.Strings(calls)
		if len(calls) == 0 || calls[0] != "task" {
			t.Errorf("task %q: its delegation must be logged as its own tool_call, got %v", who, calls)
		}
	}
}
