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
	"unicode/utf8"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/patch/fsutil"
	"github.com/orchestra/orchestra/protocol/wire"
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
	_ agent.SubtaskPoller = (*TaskRunner)(nil)
	_ agent.SubtaskPoller = (*scopedRunner)(nil)
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

func (sr *scopedRunner) Poll(ctx context.Context, taskID string, timeoutMS int) (*agent.SubtaskResult, error) {
	if err := sr.r.checkOwner(sr.s, taskID); err != nil {
		return nil, err
	}
	return sr.r.Poll(ctx, taskID, timeoutMS)
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

func (sr *scopedRunner) Post(ctx context.Context, req agent.AgentPostRequest) (*agent.AgentPostReceipt, error) {
	return sr.r.post(ctx, sr.s, req)
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
func (r *TaskRunner) Post(ctx context.Context, req agent.AgentPostRequest) (*agent.AgentPostReceipt, error) {
	return r.post(ctx, rootScope(), req)
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

// checkOwner refuses a child waiting for or cancelling another agent's task.
func (r *TaskRunner) checkOwner(s agentScope, taskID string) error {
	return r.graph.checkOwner(s.taskID, taskID)
}

func (r *TaskRunner) setStatus(e *taskEntry, status string) {
	r.graph.setStatus(e, status)
}

// markFinished stamps the final board status (taskGraph.markFinished) and
// tells the checkpoint the graph moved.
func (r *TaskRunner) markFinished(e *taskEntry) {
	r.graph.markFinished(e)
	r.graphChanged()
}

func (r *TaskRunner) recordEdited(taskID string, paths []string) {
	r.graph.recordEdited(taskID, paths)
}

// keyOf is where a depends_on key lives: in the namespace of the task that
// spawned it. Keys used to be global, and a second Lead's wo-1 silently took
// the first one's over, so each got the other's upstream_results (ORC-7).
func keyOf(spawner, key string) string {
	return spawner + "\x00" + key
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
//
// tainted names the first dependency whose result came from untrusted text:
// its line is marked as such, and the task starts tainted (agent/taint.go).
func (r *TaskRunner) awaitDeps(ctx context.Context, e *taskEntry) (upstream, tainted string, _ *agent.SubtaskResult) {
	var b strings.Builder
	b.WriteString("<upstream_results>\nTasks this one depends on have finished:\n")
	for _, d := range e.deps {
		select {
		case <-d.done:
		case <-ctx.Done():
			return "", "", &agent.SubtaskResult{TaskID: e.id, Status: "timeout", Error: "cancelled while waiting for depends_on"}
		}
		res, name, addr := r.graph.depView(d)
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
			return "", "", &agent.SubtaskResult{
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
		text = strings.TrimSpace(text)
		if res.Tainted != "" {
			source := name + " (read " + res.Tainted + ")"
			if tainted == "" {
				tainted = source
			}
			text = agent.Spotlight(source, text)
		}
		fmt.Fprintf(&b, "- %s (%s): %s\n", name, addr, text)
	}
	b.WriteString("</upstream_results>")
	return b.String(), tainted, nil
}

// acquireSlot takes one of the per-depth concurrency slots. Per depth, not
// one shared pool: a Lead waiting on its workers holds its own slot, and with
// one pool four Leads could hold all four while their workers queue forever.
func (r *TaskRunner) acquireSlot(ctx context.Context, e *taskEntry, parentToolCallID string) (func(), error) {
	ch := r.graph.slot(e.depth)
	if ch == nil {
		return func() {}, nil
	}
	n := r.child.Agency.MaxParallel
	release := func() { <-ch }
	select {
	case ch <- struct{}{}:
		return release, nil
	default:
	}
	if r.child.NotifyAgentEvent != nil {
		r.child.NotifyAgentEvent(wire.AgentEvent{
			Type:             wire.EventChildQueued,
			TaskID:           e.id,
			ParentToolCallID: parentToolCallID,
			Reason:           fmt.Sprintf("%d agents already running at depth %d (agency.max_parallel)", n, e.depth),
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
	return r.graph.board(time.Now())
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
	for i, id := range ids {
		entries[i] = r.graph.lookup(strings.TrimSpace(id))
		if entries[i] == nil {
			return nil, fmt.Errorf("task %q not found", id)
		}
	}
	// One deadline for the whole set. A task still running when it passes is
	// reported still_running and keeps running, like a single task_wait: the
	// deadline used to cancel every task it caught (audit ORC-11).
	var deadline time.Time
	if timeoutMS > 0 {
		deadline = time.Now().Add(time.Duration(timeoutMS) * time.Millisecond)
	}
	out := &agent.WaitManyResult{Results: make([]*agent.SubtaskResult, len(ids))}
	var finished []*taskEntry
	for i, e := range entries {
		wait := 0
		if !deadline.IsZero() {
			// At least a millisecond: a task that is already done is
			// collected even when the deadline has passed.
			wait = max(int(time.Until(deadline).Milliseconds()), 1)
		}
		res, err := r.wait(ctx, e.id, wait, false)
		if err != nil {
			// Collected by a concurrent wait between the lookup above and
			// this one: the stored result is still the answer.
			if stored := r.graph.resultOf(e); stored != nil {
				res = stored
			} else {
				res = &agent.SubtaskResult{TaskID: e.id, Status: "error", Error: err.Error()}
			}
		}
		out.Results[i] = res
		if res.Status != "still_running" {
			finished = append(finished, e)
		}
	}
	// Built and tested together only when every task of the set is done:
	// half a set's edits prove nothing about the whole.
	if len(finished) == len(entries) {
		out.Integration = r.integrationVerify(ctx, entries)
	}
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
	dropped := 0
	if r.child.Agency.Threads {
		history, dropped = r.loadThread(from.address, to)
	}
	message := fmt.Sprintf("Message from %s:\n\n%s", from.address, msg)
	goal := message
	if dropped > 0 {
		// The recipient sees the last exchanges as history; say that there
		// were more, or it takes the trimmed thread for the whole of it.
		goal = fmt.Sprintf("(Your conversation with %s is longer: its %d earliest exchanges were trimmed; the last %d are above.)\n\n",
			from.address, dropped, len(history)/2) + goal
	}
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
	reply := &agent.AgentMessageReply{To: to, TaskID: id, Status: res.Status, Reply: res.Result, Error: res.Error, Tainted: res.Tainted}
	if res.Status == "done" && r.child.Agency.Threads {
		history = append(history, llmMessage{Role: "user", Content: message}, llmMessage{Role: "assistant", Content: res.Result})
		var trimmed int
		history, trimmed = trimThread(history)
		reply.Turns = len(history) / 2
		if err := r.saveThread(from.address, to, history, dropped+trimmed); err != nil {
			reply.Summary = "conversation not saved: " + err.Error()
		}
	}
	r.emitAgentMessage("reply", to, from.address, "message", res.Result+res.Error, id)
	return reply, nil
}

// post leaves a note: live for a running recipient, in its inbox otherwise,
// through the bus (bus.go). Posting needs no flow — the runtime relays it,
// the way the Question Barrier relays open_questions (spec §2.2: relay is
// code, not an LLM turn).
func (r *TaskRunner) post(ctx context.Context, from agentScope, req agent.AgentPostRequest) (*agent.AgentPostReceipt, error) {
	if !r.child.Agency.Enabled {
		return nil, fmt.Errorf("agent_post: the agency is off for this turn")
	}
	p, err := r.bus.parsePost(from, req)
	if err != nil {
		return nil, err
	}
	return r.bus.publish(ctx, p)
}

// leaveForRoot puts a note in the top-level agent's inbox.
func (r *TaskRunner) leaveForRoot(m agent.InboxMessage) {
	r.mu.Lock()
	r.rootInbox = append(r.rootInbox, m)
	r.mu.Unlock()
}

// deliverLive appends m to the inbox of every unfinished task that answers to
// address (its task ID, its address, or its department type). Returns how many.
func (r *TaskRunner) deliverLive(address, exceptTaskID string, m agent.InboxMessage) int {
	return r.graph.deliverLive(address, exceptTaskID, m, config.AgencyType)
}

func (r *TaskRunner) drainTaskInbox(taskID string) []agent.InboxMessage {
	return r.graph.drainInbox(taskID)
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
	r.child.NotifyAgentEvent(wire.AgentEvent{
		Type:    wire.EventAgentMessage,
		Channel: channel, // send | reply | post
		From:    from,
		To:      to,
		Kind:    kind,
		Content: clip(content, 600),
		TaskID:  taskID,
	})
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
	// Dropped counts the notes the inbox let go of when it was full; the
	// recipient is told, instead of reading a cut inbox as a complete one.
	Dropped int `json:"dropped,omitempty"`
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
		f.Dropped += over
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
		if f.Dropped > 0 {
			out = append(out, agent.InboxMessage{
				From:    "runtime",
				Kind:    "note",
				Message: fmt.Sprintf("%d older notes to %s were dropped: an inbox keeps the latest %d.", f.Dropped, n, inboxMaxMessages),
			})
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
	// Dropped counts the earlier exchanges trimThread let go of.
	Dropped int `json:"dropped,omitempty"`
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

func (r *TaskRunner) loadThread(from, to string) (msgs []llmMessage, dropped int) {
	if !config.ValidAgencyName(from) || !config.ValidAgencyName(to) {
		return nil, 0
	}
	r.storeMu.Lock()
	defer r.storeMu.Unlock()
	var f threadFile
	if err := readJSONFile(r.threadPath(from, to), &f); err != nil {
		return nil, 0
	}
	return f.Messages, f.Dropped
}

func (r *TaskRunner) saveThread(from, to string, msgs []llmMessage, dropped int) error {
	if !config.ValidAgencyName(from) || !config.ValidAgencyName(to) {
		return fmt.Errorf("invalid thread address %q → %q", from, to)
	}
	r.storeMu.Lock()
	defer r.storeMu.Unlock()
	return writeJSONFile(r.threadPath(from, to), threadFile{
		From:     from,
		To:       to,
		Updated:  time.Now().UTC().Format(time.RFC3339),
		Messages: msgs,
		Dropped:  dropped,
	})
}

// trimThread drops the oldest exchanges (user+assistant pairs) until the
// thread fits both bounds, and says how many it dropped.
func trimThread(msgs []llmMessage) ([]llmMessage, int) {
	size := func(ms []llmMessage) int {
		n := 0
		for _, m := range ms {
			n += len(m.Content)
		}
		return n
	}
	dropped := 0
	for len(msgs) > 2 && (len(msgs) > threadMaxMessages || size(msgs) > threadMaxBytes) {
		msgs = msgs[2:]
		dropped++
	}
	return msgs, dropped
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
		cut := len(text) - deptScratchpadLeadMaxBytes
		for cut < len(text) && !utf8.RuneStart(text[cut]) {
			cut++
		}
		text = fmt.Sprintf("…(%d earlier bytes not shown; read %s/%s.md)\n", cut, agent.DeptScratchpadDir, dept) + text[cut:]
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
