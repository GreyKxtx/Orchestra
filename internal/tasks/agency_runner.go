package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/patch/fsutil"
)

// scopedRunner is a child's view of the TaskRunner: the same registry, slots
// and board, seen from the child's place in the agency. Everything it spawns
// is checked against the flows from that place, and it may wait for or
// cancel only the tasks it started itself.
type scopedRunner struct {
	r *TaskRunner
	s agentScope
}

var (
	_ agent.SubtaskRunner = (*scopedRunner)(nil)
	_ agent.AgencyRunner  = (*scopedRunner)(nil)
	_ agent.AgencyRunner  = (*TaskRunner)(nil)
)

func (sr *scopedRunner) Spawn(ctx context.Context, req agent.SubtaskSpawnRequest) (string, error) {
	return sr.r.spawnFrom(ctx, sr.s, req, spawnExtra{})
}

func (sr *scopedRunner) Wait(ctx context.Context, taskID string, timeoutMS int) (*agent.SubtaskResult, error) {
	if err := sr.r.checkOwner(sr.s, taskID); err != nil {
		return nil, err
	}
	return sr.r.Wait(ctx, taskID, timeoutMS)
}

func (sr *scopedRunner) Cancel(ctx context.Context, taskID string) error {
	if err := sr.r.checkOwner(sr.s, taskID); err != nil {
		return err
	}
	return sr.r.Cancel(ctx, taskID)
}

// InvalidateStaleContractTasks forwards the contract-write hook: a Dept Lead
// that rewrites a contract artifact at stage 2.5 must stale the running
// workers exactly as the Orchestrator's own write would.
func (sr *scopedRunner) InvalidateStaleContractTasks(ctx context.Context) []string {
	return sr.r.InvalidateStaleContractTasks(ctx)
}

func (sr *scopedRunner) AgencyInfo() agent.AgencyInfo { return sr.r.agencyInfo(sr.s) }

func (sr *scopedRunner) SendMessage(ctx context.Context, req agent.AgentMessageRequest) (*agent.AgentMessageReply, error) {
	return sr.r.sendMessage(ctx, sr.s, req)
}

func (sr *scopedRunner) Post(_ context.Context, req agent.AgentPostRequest) (*agent.AgentPostReceipt, error) {
	return sr.r.post(sr.s, req)
}

func (sr *scopedRunner) Board() []agent.TaskBoardEntry { return sr.r.board() }

func (sr *scopedRunner) WaitMany(ctx context.Context, taskIDs []string, timeoutMS int) (*agent.WaitManyResult, error) {
	for _, id := range taskIDs {
		if err := sr.r.checkOwner(sr.s, id); err != nil {
			return nil, err
		}
	}
	return sr.r.waitMany(ctx, taskIDs, timeoutMS)
}

func (sr *scopedRunner) DrainInbox() []agent.InboxMessage { return sr.r.drainTaskInbox(sr.s.taskID) }

// The root agent's side of AgencyRunner: the TaskRunner itself is the hub.

// AgencyInfo implements agent.AgencyRunner for the top-level agent.
func (r *TaskRunner) AgencyInfo() agent.AgencyInfo { return r.agencyInfo(rootScope()) }

// SendMessage implements agent.AgencyRunner for the top-level agent.
func (r *TaskRunner) SendMessage(ctx context.Context, req agent.AgentMessageRequest) (*agent.AgentMessageReply, error) {
	return r.sendMessage(ctx, rootScope(), req)
}

// Post implements agent.AgencyRunner for the top-level agent.
func (r *TaskRunner) Post(_ context.Context, req agent.AgentPostRequest) (*agent.AgentPostReceipt, error) {
	return r.post(rootScope(), req)
}

// Board implements agent.AgencyRunner.
func (r *TaskRunner) Board() []agent.TaskBoardEntry { return r.board() }

// WaitMany implements agent.AgencyRunner for the top-level agent.
func (r *TaskRunner) WaitMany(ctx context.Context, taskIDs []string, timeoutMS int) (*agent.WaitManyResult, error) {
	return r.waitMany(ctx, taskIDs, timeoutMS)
}

// DrainInbox returns and clears the notes addressed to the top-level agent.
func (r *TaskRunner) DrainInbox() []agent.InboxMessage {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.rootInbox
	r.rootInbox = nil
	return out
}

// ── registry helpers ─────────────────────────────────────────────────────────

func (r *TaskRunner) findEntryLocked(taskID string) *taskEntry {
	for _, e := range r.all {
		if e.id == taskID {
			return e
		}
	}
	return nil
}

// checkOwner refuses a child waiting for or cancelling another agent's task.
func (r *TaskRunner) checkOwner(s agentScope, taskID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	e := r.findEntryLocked(taskID)
	if e == nil {
		return fmt.Errorf("task %q not found", taskID)
	}
	if e.parentTaskID != s.taskID {
		return fmt.Errorf("task %s was started by %s, not by you; wait only for tasks you started", taskID, e.parent)
	}
	return nil
}

func (r *TaskRunner) setStatus(e *taskEntry, status string) {
	r.mu.Lock()
	e.status = status
	r.mu.Unlock()
}

// markFinished stamps the final board status. A worker whose task ended
// "done" but whose result is not a success (verification_failed, blocked)
// shows as failed: the board is where a Lead decides what to redo.
func (r *TaskRunner) markFinished(e *taskEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
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

func (r *TaskRunner) recordEdited(taskID string, paths []string) {
	if len(paths) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if e := r.findEntryLocked(taskID); e != nil {
		e.edited = append([]string(nil), paths...)
	}
}

// resolveDepsLocked maps depends_on names (keys or task IDs) to registered
// tasks. A dependency must exist before its dependent is spawned, which is
// what keeps the dependency graph acyclic. Caller holds r.mu.
func (r *TaskRunner) resolveDepsLocked(names []string) ([]*taskEntry, error) {
	var out []*taskEntry
	seen := map[*taskEntry]bool{}
	for _, raw := range names {
		n := strings.TrimSpace(raw)
		if n == "" {
			continue
		}
		e := r.byKey[n]
		if e == nil {
			e = r.findEntryLocked(n)
		}
		if e == nil {
			return nil, fmt.Errorf("depends_on: unknown task %q — spawn it first, then the tasks that depend on it (known: %s)", n, r.knownNamesLocked())
		}
		if !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	return out, nil
}

func (r *TaskRunner) knownNamesLocked() string {
	var names []string
	for _, e := range r.all {
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

// depSucceeded reports whether a finished dependency lets its dependents run.
func depSucceeded(d *taskEntry, res *agent.SubtaskResult) bool {
	if res == nil || res.Status != "done" {
		return false
	}
	return !d.worker || workerOutcomeSucceeded(res.Result)
}

// upstreamResultMaxBytes caps one dependency's result handed to a dependent.
const upstreamResultMaxBytes = 1500

// awaitDeps blocks until every dependency of e has finished. It returns the
// <upstream_results> block for the child, or a result that ends e without
// running it: a failed dependency means the dependent would build on
// something that is not there.
func (r *TaskRunner) awaitDeps(ctx context.Context, e *taskEntry) (string, *agent.SubtaskResult) {
	var b strings.Builder
	b.WriteString("<upstream_results>\nTasks this one depends on have finished:\n")
	for _, d := range e.deps {
		select {
		case <-d.done:
		case <-ctx.Done():
			return "", &agent.SubtaskResult{TaskID: e.id, Status: "timeout", Error: "cancelled while waiting for depends_on"}
		}
		r.mu.Lock()
		res := d.result
		name := d.key
		if name == "" {
			name = d.id
		}
		addr := d.address
		r.mu.Unlock()
		if !depSucceeded(d, res) {
			status := "without a result"
			if res != nil {
				status = res.Status
				if res.Status == "done" {
					status = "unsuccessfully"
				}
			}
			blocked, _ := json.Marshal(map[string]any{
				"status":         "blocked",
				"blocked_reason": "dependency_unmet",
				"dependency":     name,
			})
			return "", &agent.SubtaskResult{
				TaskID: e.id,
				Status: "error",
				Result: string(blocked),
				Error:  fmt.Sprintf("blocked_reason: dependency_unmet — %s (%s) finished %s; this task did not run", name, addr, status),
			}
		}
		text := res.Result
		if d.worker {
			text = agent.CompactWorkerResultForLead(text, upstreamResultMaxBytes)
		} else {
			text = clip(text, upstreamResultMaxBytes)
		}
		fmt.Fprintf(&b, "- %s (%s): %s\n", name, addr, strings.TrimSpace(text))
	}
	b.WriteString("</upstream_results>")
	return b.String(), nil
}

// acquireSlot takes one of the per-depth concurrency slots. Per depth, not
// one shared pool: a Lead waiting on its workers holds its own slot, and with
// one pool four Leads could hold all four while their workers queue forever.
func (r *TaskRunner) acquireSlot(ctx context.Context, e *taskEntry, parentToolCallID string) (func(), error) {
	n := r.child.Agency.MaxParallel
	if n <= 0 {
		return func() {}, nil
	}
	r.mu.Lock()
	ch := r.slots[e.depth]
	if ch == nil {
		ch = make(chan struct{}, n)
		r.slots[e.depth] = ch
	}
	r.mu.Unlock()
	release := func() { <-ch }
	select {
	case ch <- struct{}{}:
		return release, nil
	default:
	}
	if r.child.NotifyAgentEvent != nil {
		r.child.NotifyAgentEvent(map[string]any{
			"type":                "child_queued",
			"task_id":             e.id,
			"parent_tool_call_id": parentToolCallID,
			"reason":              fmt.Sprintf("%d agents already running at depth %d (agency.max_parallel)", n, e.depth),
		})
	}
	select {
	case ch <- struct{}{}:
		return release, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// ── board & wait-many ────────────────────────────────────────────────────────

func (r *TaskRunner) board() []agent.TaskBoardEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	out := make([]agent.TaskBoardEntry, 0, len(r.all))
	for _, e := range r.all {
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

// waitMany waits for every task in ids under one deadline, then verifies the
// workers' edits together when two or more of them changed files: each
// worker was verified alone, and nothing else checks that the pieces build
// and pass as one.
func (r *TaskRunner) waitMany(ctx context.Context, ids []string, timeoutMS int) (*agent.WaitManyResult, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("task_ids is empty")
	}
	entries := make([]*taskEntry, len(ids))
	r.mu.Lock()
	for i, id := range ids {
		entries[i] = r.findEntryLocked(strings.TrimSpace(id))
		if entries[i] == nil {
			r.mu.Unlock()
			return nil, fmt.Errorf("task %q not found", id)
		}
	}
	r.mu.Unlock()
	if timeoutMS > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeoutMS)*time.Millisecond)
		defer cancel()
	}
	out := &agent.WaitManyResult{Results: make([]*agent.SubtaskResult, len(ids))}
	for i, e := range entries {
		r.mu.Lock()
		_, registered := r.tasks[e.id]
		r.mu.Unlock()
		if !registered {
			// Already collected by an earlier wait: its result is kept.
			r.mu.Lock()
			res := e.result
			r.mu.Unlock()
			if res == nil {
				res = &agent.SubtaskResult{TaskID: e.id, Status: "error", Error: "task produced no result"}
			}
			out.Results[i] = res
			continue
		}
		res, err := r.Wait(ctx, e.id, 0)
		if err != nil {
			res = &agent.SubtaskResult{TaskID: e.id, Status: "error", Error: err.Error()}
		}
		out.Results[i] = res
	}
	out.Integration = r.integrationVerify(ctx, entries)
	return out, nil
}

// ── messaging ────────────────────────────────────────────────────────────────

var taskIDRe = regexp.MustCompile(`^task_\d+_\d+$`)

// takeMessage counts one send_message / agent_post against the turn budget.
func (r *TaskRunner) takeMessage() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	max := r.child.Agency.MaxMessages
	if max > 0 && r.messages >= max {
		return fmt.Errorf("message budget exhausted: %d send_message/agent_post this turn (agency.max_messages) — finish with what you have, or report back", max)
	}
	r.messages++
	return nil
}

// sendMessage is the agency's conversation: the recipient runs as the
// sender's child with the earlier turns of their conversation as history,
// and its reply comes back as the result.
func (r *TaskRunner) sendMessage(ctx context.Context, from agentScope, req agent.AgentMessageRequest) (*agent.AgentMessageReply, error) {
	if !r.child.Agency.Enabled {
		return nil, fmt.Errorf("send_message: the agency is off for this turn")
	}
	to := strings.ToLower(strings.TrimSpace(req.To))
	if to == "" || !config.ValidAgencyName(to) {
		return nil, fmt.Errorf("send_message: %q is not an agent address (backend, frontend@web, a custom agent or a role)", req.To)
	}
	msg := strings.TrimSpace(req.Message)
	if msg == "" {
		return nil, fmt.Errorf("send_message: message is empty")
	}
	if to == from.address {
		return nil, fmt.Errorf("send_message: you are %s", to)
	}
	if to == config.AgencyRootName {
		return nil, fmt.Errorf("send_message: %s is waiting on you — answer it with task_result, or leave a note with agent_post", to)
	}
	if from.inChain(to) {
		return nil, fmt.Errorf("send_message: %s is waiting on you (%s) — messaging it would deadlock; answer it with task_result", to, strings.Join(from.chain, " → "))
	}
	subagentType, dept := to, ""
	if !config.IsSpawnableRole(to) && r.findProfile(to) == nil {
		// A department: its Lead answers.
		subagentType = strings.ToLower(strings.TrimSpace(req.Role))
		if subagentType == "" {
			subagentType = "architecture"
		}
		dept = to
	}
	if subagentType == "worker" || (r.findProfile(subagentType) != nil && r.findProfile(subagentType).Base == "worker") {
		return nil, fmt.Errorf("send_message: workers take WorkOrders, not conversations — task_spawn a worker (subagent_type=worker) instead")
	}
	if !config.IsSpawnableRole(subagentType) && r.findProfile(subagentType) == nil {
		return nil, fmt.Errorf("send_message: role %q cannot answer a message", req.Role)
	}
	if err := r.takeMessage(); err != nil {
		return nil, err
	}

	var history []llmMessage
	if r.child.Agency.Threads {
		history = r.loadThread(from.address, to)
	}
	goal := fmt.Sprintf("Message from %s:\n\n%s", from.address, msg)
	timeoutMS := req.TimeoutMS
	if timeoutMS <= 0 {
		timeoutMS = DefaultTaskTimeoutMS
	}
	r.emitAgentMessage("send", from.address, to, "message", msg, "")
	id, err := r.spawnFrom(ctx, from, agent.SubtaskSpawnRequest{
		Goal:             goal,
		SubagentType:     subagentType,
		Dept:             dept,
		TimeoutMS:        timeoutMS,
		ParentToolCallID: req.ParentToolCallID,
	}, spawnExtra{history: toLLMMessages(history), verb: "message"})
	if err != nil {
		return nil, fmt.Errorf("send_message: %w", err)
	}
	res, err := r.Wait(ctx, id, timeoutMS)
	if err != nil {
		return nil, fmt.Errorf("send_message: %w", err)
	}
	reply := &agent.AgentMessageReply{To: to, TaskID: id, Status: res.Status, Reply: res.Result, Error: res.Error}
	if res.Status == "done" && r.child.Agency.Threads {
		history = append(history, llmMessage{Role: "user", Content: goal}, llmMessage{Role: "assistant", Content: res.Result})
		history = trimThread(history)
		reply.Turns = len(history) / 2
		if err := r.saveThread(from.address, to, history); err != nil {
			reply.Summary = "conversation not saved: " + err.Error()
		}
	}
	r.emitAgentMessage("reply", to, from.address, "message", res.Result+res.Error, id)
	return reply, nil
}

var postKinds = map[string]bool{"note": true, "question": true, "contract_change_request": true, "finding": true, "handoff": true}

// postMessageMaxBytes caps one note; longer material belongs in a file the
// note points at.
const postMessageMaxBytes = 4000

// post leaves a note: live for a running recipient, in its inbox otherwise.
// Posting needs no flow — the runtime relays it, the way the Question
// Barrier relays open_questions (spec §2.2: relay is code, not an LLM turn).
func (r *TaskRunner) post(from agentScope, req agent.AgentPostRequest) (*agent.AgentPostReceipt, error) {
	if !r.child.Agency.Enabled {
		return nil, fmt.Errorf("agent_post: the agency is off for this turn")
	}
	to := strings.ToLower(strings.TrimSpace(req.To))
	switch to {
	case "lead", "root", "parent":
		to = config.AgencyRootName
	}
	if to == "" || (!config.ValidAgencyName(to) && !taskIDRe.MatchString(to)) {
		return nil, fmt.Errorf("agent_post: %q is not an agent address or task_id", req.To)
	}
	if to == from.address {
		return nil, fmt.Errorf("agent_post: you are %s", to)
	}
	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	if kind == "" {
		kind = "note"
	}
	if !postKinds[kind] {
		return nil, fmt.Errorf("agent_post: kind %q (want note|question|contract_change_request|finding|handoff)", req.Kind)
	}
	text := strings.TrimSpace(req.Message)
	if text == "" {
		return nil, fmt.Errorf("agent_post: message is empty")
	}
	if len(text) > postMessageMaxBytes {
		return nil, fmt.Errorf("agent_post: message is %d bytes (max %d) — write the detail to a file and point at it", len(text), postMessageMaxBytes)
	}
	artifact := strings.TrimSpace(req.Artifact)
	if kind == "contract_change_request" && artifact == "" {
		return nil, fmt.Errorf("agent_post: contract_change_request needs artifact (the contract file the delta applies to)")
	}
	if err := r.takeMessage(); err != nil {
		return nil, err
	}
	m := agent.InboxMessage{From: from.address, To: to, Kind: kind, Message: text, Artifact: artifact, At: time.Now().UTC().Format(time.RFC3339)}
	receipt := &agent.AgentPostReceipt{To: to, Delivered: "live"}
	switch {
	case to == config.AgencyRootName:
		r.mu.Lock()
		r.rootInbox = append(r.rootInbox, m)
		r.mu.Unlock()
	case r.deliverLive(to, from.taskID, m) > 0:
	default:
		if taskIDRe.MatchString(to) {
			return nil, fmt.Errorf("agent_post: task %s is not running", to)
		}
		if err := r.appendInbox(to, m); err != nil {
			return nil, fmt.Errorf("agent_post: %w", err)
		}
		receipt.Delivered = "inbox"
		receipt.Note = to + " is not running; it reads the note when it is next started"
	}
	// The hub has to know the contract is being argued over: a change
	// request is copied to the Orchestrator whoever it was addressed to.
	if kind == "contract_change_request" && to != config.AgencyRootName && from.depth > 0 {
		r.mu.Lock()
		r.rootInbox = append(r.rootInbox, m)
		r.mu.Unlock()
	}
	r.emitAgentMessage("post", from.address, to, kind, text, "")
	return receipt, nil
}

// deliverLive appends m to the inbox of every unfinished task that answers to
// address (its task ID, its address, or its department type). Returns how many.
func (r *TaskRunner) deliverLive(address, exceptTaskID string, m agent.InboxMessage) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, e := range r.all {
		if e.id == exceptTaskID || !e.finished.IsZero() {
			continue
		}
		select {
		case <-e.done:
			continue
		default:
		}
		if e.id == address || e.address == address || config.AgencyType(e.address) == address {
			e.inbox = append(e.inbox, m)
			n++
		}
	}
	return n
}

func (r *TaskRunner) drainTaskInbox(taskID string) []agent.InboxMessage {
	r.mu.Lock()
	defer r.mu.Unlock()
	e := r.findEntryLocked(taskID)
	if e == nil || len(e.inbox) == 0 {
		return nil
	}
	out := e.inbox
	e.inbox = nil
	return out
}

// flushLiveInbox moves notes a child never read into the address's inbox.
func (r *TaskRunner) flushLiveInbox(taskID, address string) {
	pending := r.drainTaskInbox(taskID)
	for _, m := range pending {
		_ = r.appendInbox(address, m)
	}
}

func (r *TaskRunner) emitAgentMessage(channel, from, to, kind, content, taskID string) {
	if r.child.NotifyAgentEvent == nil {
		return
	}
	ev := map[string]any{
		"type":    "agent_message",
		"channel": channel, // send | reply | post
		"from":    from,
		"to":      to,
		"kind":    kind,
		"content": clip(content, 600),
	}
	if taskID != "" {
		ev["task_id"] = taskID
	}
	r.child.NotifyAgentEvent(ev)
}

// ── persistent inbox & threads ───────────────────────────────────────────────

// agencyDirRel is where the agency keeps what outlives a turn.
const agencyDirRel = ".orchestra/agency"

// inboxMaxMessages bounds one address's inbox; the oldest notes go first.
const inboxMaxMessages = 50

// agencyInboxInjectMaxBytes caps the notes prepended to a child's first message.
const agencyInboxInjectMaxBytes = 6000

type inboxFile struct {
	Messages []agent.InboxMessage `json:"messages"`
}

func (r *TaskRunner) agencyPath(parts ...string) string {
	return filepath.Join(append([]string{r.toolRunner.WorkspaceRoot(), filepath.FromSlash(agencyDirRel)}, parts...)...)
}

func (r *TaskRunner) inboxPath(address string) string {
	return r.agencyPath("inbox", address+".json")
}

func (r *TaskRunner) appendInbox(address string, m agent.InboxMessage) error {
	if !config.ValidAgencyName(address) {
		return fmt.Errorf("invalid inbox address %q", address)
	}
	r.storeMu.Lock()
	defer r.storeMu.Unlock()
	path := r.inboxPath(address)
	var f inboxFile
	_ = readJSONFile(path, &f)
	f.Messages = append(f.Messages, m)
	if over := len(f.Messages) - inboxMaxMessages; over > 0 {
		f.Messages = f.Messages[over:]
	}
	return writeJSONFile(path, f)
}

// takeInbox returns and removes the notes waiting for address — and for its
// department type, so a note to "frontend" reaches frontend@web.
func (r *TaskRunner) takeInbox(address string) []agent.InboxMessage {
	if !r.child.Agency.Enabled || !config.ValidAgencyName(address) {
		return nil
	}
	r.storeMu.Lock()
	defer r.storeMu.Unlock()
	names := []string{address}
	if t := config.AgencyType(address); t != address {
		names = append(names, t)
	}
	var out []agent.InboxMessage
	for _, n := range names {
		path := r.inboxPath(n)
		var f inboxFile
		if err := readJSONFile(path, &f); err != nil {
			continue
		}
		out = append(out, f.Messages...)
		_ = os.Remove(path)
	}
	return out
}

// llmMessage is one turn of a stored conversation — the exchanged messages
// only, not the recipient's tool calls: the reply already says what they
// found, and replaying them would cost the recipient its context window.
type llmMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type threadFile struct {
	From     string       `json:"from"`
	To       string       `json:"to"`
	Updated  string       `json:"updated"`
	Messages []llmMessage `json:"messages"`
}

// Thread bounds: the last few exchanges, and never more than a slice of a
// small model's window.
const (
	threadMaxMessages = 12
	threadMaxBytes    = 16 * 1024
)

func (r *TaskRunner) threadPath(from, to string) string {
	return r.agencyPath("threads", from+"__"+to+".json")
}

func (r *TaskRunner) loadThread(from, to string) []llmMessage {
	r.storeMu.Lock()
	defer r.storeMu.Unlock()
	var f threadFile
	if err := readJSONFile(r.threadPath(from, to), &f); err != nil {
		return nil
	}
	return f.Messages
}

func (r *TaskRunner) saveThread(from, to string, msgs []llmMessage) error {
	r.storeMu.Lock()
	defer r.storeMu.Unlock()
	return writeJSONFile(r.threadPath(from, to), threadFile{
		From:     from,
		To:       to,
		Updated:  time.Now().UTC().Format(time.RFC3339),
		Messages: msgs,
	})
}

// trimThread drops the oldest exchanges (user+assistant pairs) until the
// thread fits both bounds.
func trimThread(msgs []llmMessage) []llmMessage {
	size := func(ms []llmMessage) int {
		n := 0
		for _, m := range ms {
			n += len(m.Content)
		}
		return n
	}
	for len(msgs) > 2 && (len(msgs) > threadMaxMessages || size(msgs) > threadMaxBytes) {
		msgs = msgs[2:]
	}
	return msgs
}

func readJSONFile(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func writeJSONFile(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return atomicWrite(path, data)
}

// ── small helpers ────────────────────────────────────────────────────────────

func firstLine(s string, max int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return clip(s, max)
}

func clip(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	cut := s[:max]
	// Do not split a UTF-8 sequence.
	for len(cut) > 0 && cut[len(cut)-1]&0xC0 == 0x80 {
		cut = cut[:len(cut)-1]
	}
	if len(cut) > 0 && cut[len(cut)-1] >= 0xC0 {
		cut = cut[:len(cut)-1]
	}
	return cut + "…"
}

// withDefaultScratchpad points a WorkOrder without context.scratchpad at its
// department's scratchpad, so its dept lessons, playbook and brief gate apply.
func withDefaultScratchpad(goal, dept string) string {
	if dept == "" || !config.ValidAgencyName(dept) {
		return goal
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(goal), &m); err != nil {
		return goal
	}
	ctxMap, _ := m["context"].(map[string]any)
	if ctxMap == nil {
		ctxMap = map[string]any{}
	}
	if sp, _ := ctxMap["scratchpad"].(string); strings.TrimSpace(sp) != "" {
		return goal
	}
	ctxMap["scratchpad"] = agent.DeptScratchpadDir + "/" + dept + ".md"
	m["context"] = ctxMap
	out, err := json.Marshal(m)
	if err != nil {
		return goal
	}
	return string(out)
}

// deptScratchpadLeadMaxBytes caps the department scratchpad handed to a Lead.
const deptScratchpadLeadMaxBytes = 3000

// loadDeptScratchpadForLead returns the tail of .orchestra/depts/{dept}.md as
// a <dept_scratchpad> block: what the department's earlier Leads and workers
// recorded. Without it a Lead re-spawned for the next epic starts blind.
func loadDeptScratchpadForLead(root, dept string) string {
	if dept == "" || !config.ValidAgencyName(dept) {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(agent.DeptScratchpadDir), dept+".md"))
	if err != nil {
		return ""
	}
	text := strings.TrimSpace(string(data))
	if text == "" {
		return ""
	}
	if len(text) > deptScratchpadLeadMaxBytes {
		text = "…" + text[len(text)-deptScratchpadLeadMaxBytes:]
	}
	return fmt.Sprintf("<dept_scratchpad dept=%q>\n%s\n</dept_scratchpad>", dept, text)
}

// formatAgentRole hands a custom agent's instructions to its child run. The
// base role's system prompt stays: it carries the task_result contract and
// the write scope the parent depends on; the custom prompt says who the agent
// is within them.
func formatAgentRole(p *AgentProfile) string {
	return fmt.Sprintf("<agent_role name=%q base=%q>\n%s\n</agent_role>", p.Name, p.Base, p.SystemPrompt)
}

func atomicWrite(path string, data []byte) error {
	return fsutil.AtomicWriteFile(path, data, 0o644)
}

func toLLMMessages(msgs []llmMessage) []llm.Message {
	if len(msgs) == 0 {
		return nil
	}
	out := make([]llm.Message, 0, len(msgs))
	for _, m := range msgs {
		role := llm.RoleUser
		if m.Role == "assistant" {
			role = llm.RoleAssistant
		}
		out = append(out, llm.Message{Role: role, Content: m.Content})
	}
	return out
}
