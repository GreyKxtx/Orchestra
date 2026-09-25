package tasks

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/llm"
)

// A turn's graph survives its core: a finished task keeps its result without
// running again, an interrupted one starts again under the id its spawner
// knows, and one nobody knows about, or whose spawner died with it, is left
// to be spawned anew.
func TestRestore_KeepsFinishedRestartsInterrupted(t *testing.T) {
	m := newHoldLLM()
	r, _ := newAgencyRunner(t, m, ChildAgentConfig{Agency: agencyOn()})
	t.Cleanup(func() { close(m.release) })

	a := spawnAndWait(t, r, agent.SubtaskSpawnRequest{Goal: "A-ONE", SubagentType: "general", Key: "a"})
	b, err := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{Goal: "HOLD-C", SubagentType: "general", TimeoutMS: 30_000})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, r, b, "running")

	records := r.Records()
	if len(records) != 2 || records[0].Status != "done" || records[1].Status != "running" {
		t.Fatalf("records: %+v", records)
	}
	// Through JSON, as a checkpoint stores them.
	raw, _ := json.Marshal(append(records,
		TaskRecord{ID: "task_99_1", Status: "running", Address: "general", Request: agent.SubtaskSpawnRequest{Goal: "UNKNOWN", SubagentType: "general"}},
		TaskRecord{ID: "task_98_1", Status: "running", Spawner: b, Address: "general", Request: agent.SubtaskSpawnRequest{Goal: "ORPHAN", SubagentType: "general"}},
	))
	var back []TaskRecord
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}

	m2 := &scriptLLM{reply: func(req llm.CompleteRequest) llm.Message { return finish("second life") }}
	r2, _ := newAgencyRunner(t, m2, ChildAgentConfig{Agency: agencyOn()})
	rep := r2.Restore(context.Background(), back, func(id string) bool { return id == b })
	if strings.Join(rep.Kept, ",") != a.TaskID || strings.Join(rep.Restarted, ",") != b || strings.Join(rep.Dropped, ",") != "task_99_1,task_98_1" {
		t.Fatalf("report: %+v", rep)
	}

	got, err := r2.Wait(context.Background(), a.TaskID, 5000)
	if err != nil || got.Status != "done" || got.Result != a.Result {
		t.Fatalf("the finished task keeps its result: %+v %v", got, err)
	}
	if len(m2.requests("A-ONE")) != 0 {
		t.Fatal("a finished task does not run again")
	}
	got, err = r2.Wait(context.Background(), b, 10_000)
	if err != nil || got.Status != "done" || !strings.Contains(got.Result, "second life") {
		t.Fatalf("the interrupted task ran again under its id: %+v %v", got, err)
	}
	if _, err := r2.Spawn(context.Background(), agent.SubtaskSpawnRequest{Goal: "LATE", SubagentType: "general", DependsOn: []string{"a"}, TimeoutMS: 30_000}); err != nil {
		t.Fatalf("the restored task's key still names it: %v", err)
	}
	// The kept task, the restarted one and the new one; the dropped are not
	// in the graph.
	if n := len(r2.Board()); n != 3 {
		t.Fatalf("the board shows the restored graph: %d rows", n)
	}
}
