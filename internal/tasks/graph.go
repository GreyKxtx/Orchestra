package tasks

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/orchestra/orchestra/internal/agent"
)

// taskGraph is the turn's tree of tasks as data: every task started, in
// order; the live ones a waiter can still collect; the names depends_on
// resolves, per spawner; the concurrency slots per depth; the sequence the
// ids come from; the budget's wall clock. It is the one place the graph
// changes, under its own lock, and it runs nothing: the TaskRunner starts
// the children and asks the graph what may start (register), what a task
// waits for (its deps, its conflicts, its slot), and where everything
// stands (board, records). A test of the graph needs no child.
//
// The audit's 4.2 asked for the runner to become an adapter over the graph
// — the state used to be spread over the runner's fields and touched from
// four files under one lock, next to the messaging state it has nothing to
// do with.
type taskGraph struct {
	mu sync.Mutex
	// all keeps every task of the turn, collected ones included, in the
	// order they were started: task_board, depends_on on a task already
	// waited for, the checkpoint's records.
	all []*taskEntry
	// live are the tasks nobody has collected yet: what wait, cancel and
	// the contract check find by id, and what Close stops.
	live map[string]*taskEntry
	// byKey names tasks for depends_on, per spawner (keyOf): two Leads that
	// both call their first WorkOrder wo-1 each depend on their own.
	byKey map[string]*taskEntry
	// slots are the per-depth concurrency semaphores (Agency.MaxParallel).
	slots map[int]chan struct{}
	seq   int
	// closed is set by close. A spawn after it is refused.
	closed bool
	// firstSpawn starts the tree's wall clock (TurnBudget.MaxWall);
	// wallTimer cancels what is still running when it runs out.
	firstSpawn time.Time
	wallTimer  *time.Timer

	budget      TurnBudget
	tokens      tokenTotaler
	maxParallel int
}

// newTaskGraph is an empty graph under budget. usage is the turn's usage
// tracker, read for the token budget when it can count (tokenTotaler);
// maxParallel is the slots per depth, none when zero.
func newTaskGraph(budget TurnBudget, usage any, maxParallel int) *taskGraph {
	g := &taskGraph{
		live:        map[string]*taskEntry{},
		byKey:       map[string]*taskEntry{},
		slots:       map[int]chan struct{}{},
		budget:      budget,
		maxParallel: maxParallel,
	}
	if tt, ok := usage.(tokenTotaler); ok {
		g.tokens = tt
	}
	return g
}

// nextID is the next task id — or id itself, when a task a crash
// interrupted restarts under the id its spawner knows. A closed graph
// hands out none.
func (g *taskGraph) nextID(id string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return "", ErrRunnerClosed
	}
	g.seq++
	if id != "" {
		return id, nil
	}
	return fmt.Sprintf("task_%d_%d", g.seq, time.Now().UnixNano()%100000), nil
}

// bumpSeq moves the sequence past n, so a restored turn's new ids do not
// collide with the ones its checkpoint holds.
func (g *taskGraph) bumpSeq(n int) {
	g.mu.Lock()
	if g.seq < n {
		g.seq = n
	}
	g.mu.Unlock()
}

// register adds a task the spawner is starting and returns what it waits
// for: its dependencies (set on the entry too) and the unfinished tasks
// whose edit scope overlaps its own (spec §5.6). Admission, dependency
// resolution and conflict collection happen under one lock, so two
// overlapping spawns cannot both see a clear field, and each task waits only
// for tasks registered before it — the wait graph is acyclic by
// construction, and so is the depends_on graph: a dependency must already
// be registered. The first task starts the wall clock; expire runs when it
// runs out.
func (g *taskGraph) register(spawner string, e *taskEntry, key string, dependsOn []string, editPaths map[string]struct{}, expire func()) (deps, conflicts []*taskEntry, err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return nil, nil, ErrRunnerClosed
	}
	if err := g.admitLocked(e.fingerprint); err != nil {
		return nil, nil, err
	}
	deps, err = g.resolveDepsLocked(spawner, dependsOn)
	if err != nil {
		return nil, nil, err
	}
	e.deps = deps
	g.startWallClockLocked(expire)
	conflicts = g.conflictsLocked(editPaths)
	g.live[e.id] = e
	g.all = append(g.all, e)
	if key != "" {
		// A re-spawned WorkOrder takes its key over: later dependents mean
		// the attempt that is still to come, not the one that failed.
		g.byKey[keyOf(spawner, key)] = e
	}
	return deps, conflicts, nil
}

// admitLocked decides whether a new task may start: within the turn's
// budget, not a copy of one still running, and not a third try of one that
// already failed twice — the stall rule: a tree that keeps starting the
// same task is not making progress.
func (g *taskGraph) admitLocked(fingerprint string) error {
	b := g.budget
	if b.MaxTasks > 0 && len(g.all) >= b.MaxTasks {
		return fmt.Errorf("turn budget: this turn already started %d tasks (agent.turn_budget.max_tasks) — finish with the results you have, or report what is left", len(g.all))
	}
	if b.MaxWall > 0 && !g.firstSpawn.IsZero() && time.Since(g.firstSpawn) >= b.MaxWall {
		return fmt.Errorf("turn budget: the turn's tasks have run for %s (agent.turn_budget.max_wall_s) — finish with the results you have", b.MaxWall)
	}
	if b.MaxTokens > 0 && g.tokens != nil {
		if _, _, _, total, _ := g.tokens.Total(); total >= b.MaxTokens {
			return fmt.Errorf("turn budget: the turn has spent %d tokens (agent.turn_budget.max_tokens %d) — finish with the results you have", total, b.MaxTokens)
		}
	}
	var failed []string
	for _, e := range g.all {
		if e.fingerprint != fingerprint {
			continue
		}
		if e.finished.IsZero() {
			return fmt.Errorf("an identical task is already running as %s: collect it with task_wait instead of starting it again", e.id)
		}
		if e.status != "done" {
			failed = append(failed, e.id)
		}
	}
	if len(failed) >= maxIdenticalFailures {
		return fmt.Errorf("this exact task already failed %d times this turn (%s): change the goal or the WorkOrder, try another approach, or report the blocker — repeating it will fail the same way", len(failed), strings.Join(failed, ", "))
	}
	return nil
}

// startWallClockLocked starts the tree's wall clock at its first task.
func (g *taskGraph) startWallClockLocked(expire func()) {
	if !g.firstSpawn.IsZero() {
		return
	}
	g.firstSpawn = time.Now()
	if d := g.budget.MaxWall; d > 0 && expire != nil {
		g.wallTimer = time.AfterFunc(d, expire)
	}
}

// resolveDepsLocked maps depends_on names to registered tasks for a task
// that spawner is starting. A key names a task the same spawner started; a
// task ID names any task of the turn. A dependency must exist before its
// dependent is spawned, which keeps the dependency graph acyclic, and must
// be one this task can wait for without waiting for itself
// (dependencyRefusalLocked).
func (g *taskGraph) resolveDepsLocked(spawner string, names []string) ([]*taskEntry, error) {
	var out []*taskEntry
	seen := map[*taskEntry]bool{}
	for _, raw := range names {
		n := strings.TrimSpace(raw)
		if n == "" {
			continue
		}
		e := g.byKey[keyOf(spawner, n)]
		if e == nil {
			e = g.findLocked(n)
		}
		if e == nil {
			return nil, fmt.Errorf("depends_on: unknown task %q — spawn it first, then the tasks that depend on it; a key names a task you started, a task_id any task of the turn (yours: %s)", n, g.knownNamesLocked(spawner))
		}
		if err := g.dependencyRefusalLocked(spawner, n, e); err != nil {
			return nil, err
		}
		if !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	return out, nil
}

// dependencyRefusalLocked refuses a dependency the new task could wait for
// forever:
//   - one of its ancestors. They wait for this task to finish — a Lead for
//     its worker, a relaying Lead for its batch — so neither ever would.
//   - a task of another branch that has not started. It waits for a slot at
//     its depth, which one of this task's ancestors may hold while waiting
//     for this task, or for dependencies of its own that do (ORC-7). A task
//     already running holds its slot and waits only for its own children; a
//     finished one waits for nothing. A sibling cannot close such a cycle:
//     the tasks it waits for are older than this one.
func (g *taskGraph) dependencyRefusalLocked(spawner, name string, dep *taskEntry) error {
	for id := spawner; id != ""; {
		if dep.id == id {
			return fmt.Errorf("depends_on: %q is an ancestor of this task — it waits for this task to finish, so this task would wait forever; use its result from your own context instead", name)
		}
		up := g.findLocked(id)
		if up == nil {
			break
		}
		id = up.spawner
	}
	if dep.spawner == spawner {
		return nil
	}
	switch dep.status {
	case "queued", "waiting_deps":
		return fmt.Errorf("depends_on: %q (%s) belongs to another agent and has not started — waiting on it can deadlock the turn; depend on your own tasks, or on it once task_board shows it running or done", name, dep.address)
	}
	return nil
}

func (g *taskGraph) knownNamesLocked(spawner string) string {
	var names []string
	for _, e := range g.all {
		if e.spawner != spawner {
			continue
		}
		n := e.key
		if n == "" {
			n = e.id
		}
		names = append(names, n)
		if len(names) == 12 {
			names = append(names, "…")
			break
		}
	}
	if len(names) == 0 {
		return "none yet"
	}
	return strings.Join(names, ", ")
}

// conflictsLocked returns the unfinished live tasks whose edit scope
// overlaps paths.
func (g *taskGraph) conflictsLocked(paths map[string]struct{}) []*taskEntry {
	if len(paths) == 0 {
		return nil
	}
	var out []*taskEntry
	for _, e := range g.live {
		if len(e.editPaths) == 0 {
			continue
		}
		select {
		case <-e.done:
			continue
		default:
		}
		for p := range paths {
			if _, hit := e.editPaths[p]; hit {
				out = append(out, e)
				break
			}
		}
	}
	return out
}

func (g *taskGraph) findLocked(id string) *taskEntry {
	for _, e := range g.all {
		if e.id == id {
			return e
		}
	}
	return nil
}

// lookup is any task of the turn by id, collected or not; nil when unknown.
func (g *taskGraph) lookup(id string) *taskEntry {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.findLocked(id)
}

// liveEntry is a task nobody has collected yet.
func (g *taskGraph) liveEntry(id string) (*taskEntry, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	e, ok := g.live[id]
	return e, ok
}

// forget takes a collected task out of the live set; the board and the
// records keep it.
func (g *taskGraph) forget(id string) {
	g.mu.Lock()
	delete(g.live, id)
	g.mu.Unlock()
}

// liveTasks is a snapshot of the live set.
func (g *taskGraph) liveTasks() []*taskEntry {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]*taskEntry, 0, len(g.live))
	for _, e := range g.live {
		out = append(out, e)
	}
	return out
}

// unfinished is every task that has not ended, in start order.
func (g *taskGraph) unfinished() []*taskEntry {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []*taskEntry
	for _, e := range g.all {
		if e.finished.IsZero() {
			out = append(out, e)
		}
	}
	return out
}

// close refuses further spawns, stops the wall clock and returns the live
// tasks for the caller to cancel; the live set is emptied.
func (g *taskGraph) close() []*taskEntry {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.closed = true
	if g.wallTimer != nil {
		g.wallTimer.Stop()
	}
	out := make([]*taskEntry, 0, len(g.live))
	for _, e := range g.live {
		out = append(out, e)
	}
	g.live = map[string]*taskEntry{}
	return out
}

// addFinished adds a task that ended before the graph held it: a
// checkpoint's finished task, or one that could not be started again.
func (g *taskGraph) addFinished(e *taskEntry) {
	g.mu.Lock()
	g.all = append(g.all, e)
	if e.key != "" {
		g.byKey[keyOf(e.spawner, e.key)] = e
	}
	g.mu.Unlock()
}

func (g *taskGraph) setStatus(e *taskEntry, status string) {
	g.mu.Lock()
	e.status = status
	g.mu.Unlock()
}

func (g *taskGraph) setResult(e *taskEntry, res *agent.SubtaskResult) {
	g.mu.Lock()
	e.result = res
	g.mu.Unlock()
}

// setResultIfNone records res unless the task already has a result.
func (g *taskGraph) setResultIfNone(e *taskEntry, res *agent.SubtaskResult) {
	g.mu.Lock()
	if e.result == nil {
		e.result = res
	}
	g.mu.Unlock()
}

// isFinished says the task has ended.
func (g *taskGraph) isFinished(e *taskEntry) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return !e.finished.IsZero()
}

func (g *taskGraph) resultOf(e *taskEntry) *agent.SubtaskResult {
	g.mu.Lock()
	defer g.mu.Unlock()
	return e.result
}

// markFinished stamps the final board status. A worker whose task ended
// "done" but whose result is not a success (verification_failed, blocked)
// shows as failed: the board is where a Lead decides what to redo.
func (g *taskGraph) markFinished(e *taskEntry) {
	g.mu.Lock()
	defer g.mu.Unlock()
	e.finished = time.Now()
	if e.result == nil {
		e.status = "error"
		return
	}
	e.status = e.result.Status
	if e.status == "done" && e.worker && !workerOutcomeSucceeded(e.result.Result) {
		e.status = "failed"
	}
}

func (g *taskGraph) recordEdited(taskID string, paths []string) {
	if len(paths) == 0 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if e := g.findLocked(taskID); e != nil {
		e.edited = append([]string(nil), paths...)
	}
}

// checkOwner refuses a child waiting for or cancelling another agent's task.
func (g *taskGraph) checkOwner(ownerTaskID, taskID string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	e := g.findLocked(taskID)
	if e == nil {
		return fmt.Errorf("task %q not found", taskID)
	}
	if e.parentTaskID != ownerTaskID {
		return fmt.Errorf("task %s was started by %s, not by you; wait only for tasks you started", taskID, e.parent)
	}
	return nil
}

// depView is what a dependent reads of a finished dependency: its result,
// the name it goes by and its address.
func (g *taskGraph) depView(d *taskEntry) (res *agent.SubtaskResult, name, address string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	name = d.key
	if name == "" {
		name = d.id
	}
	return d.result, name, d.address
}

// slot is the concurrency semaphore of depth; nil when there is no cap.
// Per depth, not one shared pool: a Lead waiting on its workers holds its
// own slot, and with one pool four Leads could hold all four while their
// workers queue forever.
func (g *taskGraph) slot(depth int) chan struct{} {
	if g.maxParallel <= 0 {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	ch := g.slots[depth]
	if ch == nil {
		ch = make(chan struct{}, g.maxParallel)
		g.slots[depth] = ch
	}
	return ch
}

// board is every task of the turn as task_board shows it.
func (g *taskGraph) board(now time.Time) []agent.TaskBoardEntry {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]agent.TaskBoardEntry, 0, len(g.all))
	for _, e := range g.all {
		end := now
		if !e.finished.IsZero() {
			end = e.finished
		}
		row := agent.TaskBoardEntry{
			TaskID:   e.id,
			Key:      e.key,
			Agent:    e.address,
			Role:     e.role,
			Parent:   e.parent,
			Depth:    e.depth,
			Status:   e.status,
			Goal:     e.goal,
			ElapsedS: int(end.Sub(e.started).Seconds()),
			Finished: !e.finished.IsZero(),
		}
		for _, d := range e.deps {
			n := d.key
			if n == "" {
				n = d.id
			}
			row.DependsOn = append(row.DependsOn, n)
		}
		out = append(out, row)
	}
	return out
}

// records is every task of the turn as a checkpoint keeps it.
func (g *taskGraph) records() []TaskRecord {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]TaskRecord, 0, len(g.all))
	for _, e := range g.all {
		rec := TaskRecord{
			ID: e.id, Key: e.key, Spawner: e.spawner, Status: e.status,
			Address: e.address, Role: e.role, Parent: e.parent, ParentTaskID: e.parentTaskID,
			Depth: e.depth, Worker: e.worker, Goal: e.goal, Fingerprint: e.fingerprint,
			Edited: append([]string(nil), e.edited...), Started: e.started, Finished: e.finished,
			Request: e.spawned.req, From: scopeRecordOf(e.spawned.from),
			History: e.spawned.extra.history, Verb: e.spawned.extra.verb,
		}
		if e.result != nil {
			res := *e.result
			rec.Result = &res
		}
		if o := e.spawned.extra.owner; o != nil {
			sr := scopeRecordOf(*o)
			rec.Owner = &sr
		}
		out = append(out, rec)
	}
	return out
}

// verifiedEdits is what an integration check looks at: how many of entries
// are workers that finished with edits, and the files they changed, in
// first-seen order.
func (g *taskGraph) verifiedEdits(entries []*taskEntry) (workers int, files []string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	seen := map[string]bool{}
	for _, e := range entries {
		if e == nil || !e.worker || e.status != "done" || len(e.edited) == 0 {
			continue
		}
		workers++
		for _, p := range e.edited {
			if !seen[p] {
				seen[p] = true
				files = append(files, p)
			}
		}
	}
	return workers, files
}

// deliverLive appends m to the inbox of every unfinished task that answers to
// address (its task ID, its address, or its department type). Returns how many.
func (g *taskGraph) deliverLive(address, exceptTaskID string, m agent.InboxMessage, deptOf func(address string) string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := 0
	for _, e := range g.all {
		if e.id == exceptTaskID || !e.finished.IsZero() {
			continue
		}
		select {
		case <-e.done:
			continue
		default:
		}
		if e.id == address || e.address == address || deptOf(e.address) == address {
			e.inbox = append(e.inbox, m)
			n++
		}
	}
	return n
}

// drainInbox takes the live notes of a task.
func (g *taskGraph) drainInbox(taskID string) []agent.InboxMessage {
	g.mu.Lock()
	defer g.mu.Unlock()
	e := g.findLocked(taskID)
	if e == nil || len(e.inbox) == 0 {
		return nil
	}
	out := e.inbox
	e.inbox = nil
	return out
}

// pushInbox puts notes at the front of a task's live inbox: what did not
// fit into its first message is read on its first steps instead of never.
func (g *taskGraph) pushInbox(taskID string, notes []agent.InboxMessage) {
	if len(notes) == 0 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if e := g.findLocked(taskID); e != nil {
		e.inbox = append(append([]agent.InboxMessage(nil), notes...), e.inbox...)
	}
}
