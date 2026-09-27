package tasks

import (
	"strings"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/agent"
)

// The task graph on its own (audit 4.2): what may start, what a task waits
// for and where everything stands, with no child running.

func entryFor(id, spawner, key, fingerprint string, paths ...string) *taskEntry {
	e := &taskEntry{
		id: id, spawner: spawner, key: key, fingerprint: fingerprint,
		done: make(chan struct{}), cancel: func(error) {}, status: "queued", started: time.Now(),
		address: "dept-" + id, parent: "root", parentTaskID: spawner, depth: 1,
	}
	if len(paths) > 0 {
		e.editPaths = map[string]struct{}{}
		for _, p := range paths {
			e.editPaths[p] = struct{}{}
		}
	}
	return e
}

func mustRegister(t *testing.T, g *taskGraph, spawner string, e *taskEntry, dependsOn ...string) (deps, conflicts []*taskEntry) {
	t.Helper()
	deps, conflicts, err := g.register(spawner, e, e.key, dependsOn, e.editPaths, nil)
	if err != nil {
		t.Fatalf("register %s: %v", e.id, err)
	}
	return deps, conflicts
}

// endTask ends e as a task that ran: status from res, off the live set.
func endTask(g *taskGraph, e *taskEntry, res *agent.SubtaskResult) {
	g.setResult(e, res)
	g.markFinished(e)
	close(e.done)
}

func TestGraph_IdsAreSequentialAndStopAtClose(t *testing.T) {
	g := newTaskGraph(TurnBudget{}, nil, 0)
	a, _ := g.nextID("")
	b, _ := g.nextID("")
	if !strings.HasPrefix(a, "task_1_") || !strings.HasPrefix(b, "task_2_") {
		t.Fatalf("ids %s, %s", a, b)
	}
	if got, _ := g.nextID("task_7_1"); got != "task_7_1" {
		t.Fatalf("a restarted task keeps its id, got %s", got)
	}
	g.bumpSeq(10)
	if c, _ := g.nextID(""); !strings.HasPrefix(c, "task_11_") {
		t.Fatalf("after bumpSeq(10) the next id is %s", c)
	}
	g.close()
	if _, err := g.nextID(""); err != ErrRunnerClosed {
		t.Fatalf("a closed graph handed out an id: %v", err)
	}
	if _, _, err := g.register("", entryFor("x", "", "", "fp"), "", nil, nil, nil); err != ErrRunnerClosed {
		t.Fatalf("a closed graph registered a task: %v", err)
	}
}

func TestGraph_AnIdenticalTaskIsRefusedWhileRunningAndAfterTwoFailures(t *testing.T) {
	g := newTaskGraph(TurnBudget{}, nil, 0)
	first := entryFor("t1", "", "", "worker\x00fix it")
	mustRegister(t, g, "", first)
	_, _, err := g.register("", entryFor("t2", "", "", "worker\x00fix it"), "", nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "already running as t1") {
		t.Fatalf("a copy of a running task was admitted: %v", err)
	}
	endTask(g, first, &agent.SubtaskResult{TaskID: "t1", Status: "error"})
	second := entryFor("t2", "", "", "worker\x00fix it")
	mustRegister(t, g, "", second)
	endTask(g, second, &agent.SubtaskResult{TaskID: "t2", Status: "error"})
	_, _, err = g.register("", entryFor("t3", "", "", "worker\x00fix it"), "", nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "already failed 2 times") {
		t.Fatalf("a third try of a task that failed twice was admitted: %v", err)
	}
	// A different task is not held back by them.
	mustRegister(t, g, "", entryFor("t4", "", "", "worker\x00something else"))
}

func TestGraph_BudgetCapsTheTasksOfATurn(t *testing.T) {
	g := newTaskGraph(TurnBudget{MaxTasks: 2}, nil, 0)
	mustRegister(t, g, "", entryFor("a", "", "", "a"))
	mustRegister(t, g, "", entryFor("b", "", "", "b"))
	if _, _, err := g.register("", entryFor("c", "", "", "c"), "", nil, nil, nil); err == nil || !strings.Contains(err.Error(), "max_tasks") {
		t.Fatalf("a third task past max_tasks 2 was admitted: %v", err)
	}
}

func TestGraph_TokenBudgetStopsNewTasks(t *testing.T) {
	g := newTaskGraph(TurnBudget{MaxTokens: 100}, fixedTokens(100), 0)
	if _, _, err := g.register("", entryFor("a", "", "", "a"), "", nil, nil, nil); err == nil || !strings.Contains(err.Error(), "max_tokens") {
		t.Fatalf("a task past the token budget was admitted: %v", err)
	}
	g = newTaskGraph(TurnBudget{MaxTokens: 100}, fixedTokens(99), 0)
	mustRegister(t, g, "", entryFor("a", "", "", "a"))
}

func TestGraph_WallClockRunsOutOnce(t *testing.T) {
	expired := make(chan struct{})
	g := newTaskGraph(TurnBudget{MaxWall: 20 * time.Millisecond}, nil, 0)
	expire := func() { close(expired) }
	if _, _, err := g.register("", entryFor("a", "", "", "a"), "", nil, nil, expire); err != nil {
		t.Fatal(err)
	}
	select {
	case <-expired:
	case <-time.After(5 * time.Second):
		t.Fatal("the wall clock never ran out")
	}
	if _, _, err := g.register("", entryFor("b", "", "", "b"), "", nil, nil, expire); err == nil || !strings.Contains(err.Error(), "max_wall_s") {
		t.Fatalf("a task after the wall clock ran out was admitted: %v", err)
	}
}

func TestGraph_KeysLiveInTheSpawnersNamespace(t *testing.T) {
	g := newTaskGraph(TurnBudget{}, nil, 0)
	leadA := entryFor("leadA", "", "", "lead a")
	leadB := entryFor("leadB", "", "", "lead b")
	mustRegister(t, g, "", leadA)
	mustRegister(t, g, "", leadB)
	g.setStatus(leadA, "running")
	g.setStatus(leadB, "running")
	wa := entryFor("wa", "leadA", "wo-1", "worker a")
	wb := entryFor("wb", "leadB", "wo-1", "worker b")
	mustRegister(t, g, "leadA", wa)
	mustRegister(t, g, "leadB", wb)
	deps, _ := mustRegister(t, g, "leadA", entryFor("wa2", "leadA", "wo-2", "worker a2"), "wo-1")
	if len(deps) != 1 || deps[0] != wa {
		t.Fatalf("Lead A's wo-2 depends on %v, want its own wo-1", deps)
	}
	deps, _ = mustRegister(t, g, "leadB", entryFor("wb2", "leadB", "wo-2", "worker b2"), "wo-1")
	if len(deps) != 1 || deps[0] != wb {
		t.Fatalf("Lead B's wo-2 depends on %v, want its own wo-1", deps)
	}
	// A task id names any task of the turn — once it runs (see
	// TestGraph_DependenciesThatWouldWaitForeverAreRefused).
	g.setStatus(wa, "running")
	deps, _ = mustRegister(t, g, "leadB", entryFor("wb3", "leadB", "", "worker b3"), "wa")
	if len(deps) != 1 || deps[0] != wa {
		t.Fatalf("by id: %v", deps)
	}
}

func TestGraph_DependenciesThatWouldWaitForeverAreRefused(t *testing.T) {
	g := newTaskGraph(TurnBudget{}, nil, 0)
	lead := entryFor("lead", "", "", "lead")
	mustRegister(t, g, "", lead)
	g.setStatus(lead, "running")
	other := entryFor("other", "", "", "other")
	mustRegister(t, g, "", other)
	// The Lead's worker cannot depend on the Lead: the Lead waits for it.
	_, _, err := g.register("lead", entryFor("w", "lead", "", "w"), "", []string{"lead"}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "ancestor") {
		t.Fatalf("a dependency on an ancestor was accepted: %v", err)
	}
	// Nor on a queued task of another branch.
	_, _, err = g.register("lead", entryFor("w", "lead", "", "w"), "", []string{"other"}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "has not started") {
		t.Fatalf("a dependency on another branch's queued task was accepted: %v", err)
	}
	// Once it runs, it holds its slot and can be waited for.
	g.setStatus(other, "running")
	mustRegister(t, g, "lead", entryFor("w", "lead", "", "w"), "other")
	_, _, err = g.register("lead", entryFor("w2", "lead", "", "w2"), "", []string{"nobody"}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "unknown task") || !strings.Contains(err.Error(), "yours: w") {
		t.Fatalf("an unknown dependency: %v", err)
	}
}

func TestGraph_OverlappingScopesConflictUntilTheFirstEnds(t *testing.T) {
	g := newTaskGraph(TurnBudget{}, nil, 0)
	a := entryFor("a", "", "", "a", "x.go", "y.go")
	mustRegister(t, g, "", a)
	_, conflicts := mustRegister(t, g, "", entryFor("b", "", "", "b", "y.go"))
	if len(conflicts) != 1 || conflicts[0] != a {
		t.Fatalf("b conflicts with %v, want a", conflicts)
	}
	_, conflicts = mustRegister(t, g, "", entryFor("c", "", "", "c", "z.go"))
	if len(conflicts) != 0 {
		t.Fatalf("c conflicts with %v, want nothing", conflicts)
	}
	endTask(g, a, &agent.SubtaskResult{TaskID: "a", Status: "done"})
	_, conflicts = mustRegister(t, g, "", entryFor("d", "", "", "d", "x.go"))
	if len(conflicts) != 0 {
		t.Fatalf("d conflicts with a finished task: %v", conflicts)
	}
}

func TestGraph_SlotsArePerDepth(t *testing.T) {
	g := newTaskGraph(TurnBudget{}, nil, 2)
	first := g.slot(1)
	if first == nil || g.slot(1) != first || g.slot(2) == first || cap(first) != 2 {
		t.Fatal("slots: one semaphore of max_parallel per depth")
	}
	if newTaskGraph(TurnBudget{}, nil, 0).slot(1) != nil {
		t.Fatal("no cap, no semaphore")
	}
}

func TestGraph_LiveIsWhatAWaiterCanCollect(t *testing.T) {
	g := newTaskGraph(TurnBudget{}, nil, 0)
	a := entryFor("a", "", "wo-a", "a")
	mustRegister(t, g, "", a)
	if e, ok := g.liveEntry("a"); !ok || e != a {
		t.Fatal("a registered task is live")
	}
	endTask(g, a, &agent.SubtaskResult{TaskID: "a", Status: "done", Result: "ok"})
	g.forget("a")
	if _, ok := g.liveEntry("a"); ok {
		t.Fatal("a collected task is still live")
	}
	if g.lookup("a") != a || !g.isFinished(a) || g.resultOf(a).Result != "ok" {
		t.Fatal("a collected task is gone from the graph: the board and later dependents need it")
	}
	// A dependent of a collected task still finds it by key.
	deps, _ := mustRegister(t, g, "", entryFor("b", "", "", "b"), "wo-a")
	if len(deps) != 1 || deps[0] != a {
		t.Fatalf("deps on a collected task: %v", deps)
	}
	if err := g.checkOwner("", "a"); err != nil {
		t.Fatalf("the root owns a: %v", err)
	}
	if err := g.checkOwner("someone", "a"); err == nil || !strings.Contains(err.Error(), "not by you") {
		t.Fatalf("another agent owns a: %v", err)
	}
}

func TestGraph_BoardAndRecordsShowEveryTask(t *testing.T) {
	g := newTaskGraph(TurnBudget{}, nil, 0)
	a := entryFor("a", "", "wo-a", "a")
	a.worker = true
	mustRegister(t, g, "", a)
	b := entryFor("b", "", "", "b")
	mustRegister(t, g, "", b, "wo-a")
	g.setStatus(b, "waiting_deps")
	// A worker that ended "done" with a failed verification shows as failed.
	endTask(g, a, &agent.SubtaskResult{TaskID: "a", Status: "done", Result: `{"status":"verification_failed"}`})
	g.recordEdited("a", []string{"x.go"})
	board := g.board(time.Now())
	if len(board) != 2 || board[0].TaskID != "a" || board[0].Status != "failed" || !board[0].Finished {
		t.Fatalf("board: %+v", board)
	}
	if board[1].Status != "waiting_deps" || len(board[1].DependsOn) != 1 || board[1].DependsOn[0] != "wo-a" {
		t.Fatalf("board row b: %+v", board[1])
	}
	recs := g.records()
	if len(recs) != 2 || recs[0].ID != "a" || recs[0].Status != "failed" || recs[0].Result == nil || len(recs[0].Edited) != 1 || !recs[0].finished() {
		t.Fatalf("records: %+v", recs)
	}
	if recs[1].finished() {
		t.Fatal("b has not finished")
	}
	workers, files := g.verifiedEdits([]*taskEntry{a, b})
	if workers != 0 || len(files) != 0 {
		t.Fatalf("a failed worker's edits count for integration: %d %v", workers, files)
	}
	c := entryFor("c", "", "", "c")
	c.worker = true
	mustRegister(t, g, "", c)
	endTask(g, c, &agent.SubtaskResult{TaskID: "c", Status: "done", Result: "ok"})
	g.recordEdited("c", []string{"y.go", "x.go"})
	if workers, files := g.verifiedEdits([]*taskEntry{a, c}); workers != 1 || strings.Join(files, ",") != "y.go,x.go" {
		t.Fatalf("integration set: %d %v", workers, files)
	}
}

func TestGraph_CloseReturnsTheLiveTasksOnce(t *testing.T) {
	g := newTaskGraph(TurnBudget{}, nil, 0)
	a := entryFor("a", "", "", "a")
	b := entryFor("b", "", "", "b")
	mustRegister(t, g, "", a)
	mustRegister(t, g, "", b)
	endTask(g, b, &agent.SubtaskResult{TaskID: "b", Status: "done"})
	g.forget("b")
	live := g.close()
	if len(live) != 1 || live[0] != a {
		t.Fatalf("close returned %v, want the one live task", live)
	}
	if len(g.close()) != 0 {
		t.Fatal("a second close returned tasks again")
	}
	if len(g.unfinished()) != 1 {
		t.Fatal("close does not end tasks: the runner cancels them")
	}
}

func TestGraph_InboxesLiveOnTheTasks(t *testing.T) {
	g := newTaskGraph(TurnBudget{}, nil, 0)
	a := entryFor("a", "", "", "a")
	a.address = "backend"
	b := entryFor("b", "", "", "b")
	b.address = "backend-2"
	c := entryFor("c", "", "", "c")
	c.address = "frontend"
	for _, e := range []*taskEntry{a, b, c} {
		mustRegister(t, g, "", e)
	}
	endTask(g, c, &agent.SubtaskResult{TaskID: "c", Status: "done"})
	deptOf := func(address string) string { return strings.SplitN(address, "-", 2)[0] }
	n := g.deliverLive("backend", "a", agent.InboxMessage{Message: "hi"}, deptOf)
	if n != 1 {
		t.Fatalf("delivered to %d, want b alone (a is the sender, c has finished)", n)
	}
	g.pushInbox("b", []agent.InboxMessage{{Message: "first"}})
	got := g.drainInbox("b")
	if len(got) != 2 || got[0].Message != "first" || got[1].Message != "hi" {
		t.Fatalf("b's inbox: %+v", got)
	}
	if g.drainInbox("b") != nil || g.drainInbox("nobody") != nil {
		t.Fatal("a drained or unknown inbox is empty")
	}
}
