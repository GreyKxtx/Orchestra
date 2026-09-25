package tasks

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
)

// scriptLLM answers every child from one function, keyed on what the child
// was asked: agents in these tests are told apart by a marker in their goal.
type scriptLLM struct {
	mu    sync.Mutex
	seen  []llm.CompleteRequest
	reply func(req llm.CompleteRequest) llm.Message
}

func (m *scriptLLM) Complete(_ context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	m.mu.Lock()
	m.seen = append(m.seen, req)
	m.mu.Unlock()
	return &llm.CompleteResponse{Message: m.reply(req)}, nil
}

func (m *scriptLLM) Plan(context.Context, string) (string, error) { return "", nil }

// requests returns the requests whose conversation contains marker.
func (m *scriptLLM) requests(marker string) []llm.CompleteRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []llm.CompleteRequest
	for _, r := range m.seen {
		if strings.Contains(conversation(r), marker) {
			out = append(out, r)
		}
	}
	return out
}

func conversation(req llm.CompleteRequest) string {
	var b strings.Builder
	for _, msg := range req.Messages {
		if msg.Role == llm.RoleSystem {
			continue
		}
		b.WriteString(msg.Content)
		b.WriteString("\n")
	}
	return b.String()
}

func systemPrompt(req llm.CompleteRequest) string {
	for _, msg := range req.Messages {
		if msg.Role == llm.RoleSystem {
			return msg.Content
		}
	}
	return ""
}

// steps is how many tool exchanges the child has had so far.
func steps(req llm.CompleteRequest) int {
	n := 0
	for _, msg := range req.Messages {
		if msg.Role == llm.RoleTool {
			n++
		}
	}
	return n
}

func toolNames(req llm.CompleteRequest) map[string]bool {
	out := map[string]bool{}
	for _, d := range req.Tools {
		out[d.Function.Name] = true
	}
	return out
}

func callTool(name string, args any) llm.Message {
	raw, _ := json.Marshal(args)
	return llm.Message{
		Role: llm.RoleAssistant,
		ToolCalls: []llm.ToolCall{{
			ID: "call_" + name, Type: "function",
			Function: llm.ToolCallFunc{Name: name, Arguments: llm.ToolArguments(raw)},
		}},
	}
}

func finish(content string) llm.Message {
	return callTool("task_result", map[string]string{"content": content})
}

func newAgencyRunner(t *testing.T, client llm.Client, cfg ChildAgentConfig) (*TaskRunner, string) {
	t.Helper()
	root := t.TempDir()
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("schema.NewValidator: %v", err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("tools.NewRunner: %v", err)
	}
	tr.SetDryRun(true)
	r := New(client, v, tr, cfg)
	t.Cleanup(func() {
		r.Close()
		_ = tr.Close()
	})
	return r, root
}

func agencyOn() AgencySettings {
	return AgencySettings{
		Enabled:         true,
		Flows:           config.DefaultAgencyFlows(),
		MaxDepth:        2,
		MaxParallel:     4,
		MaxMessages:     64,
		Threads:         true,
		RelayWorkOrders: true,
	}
}

func spawnAndWait(t *testing.T, r agent.SubtaskRunner, req agent.SubtaskSpawnRequest) *agent.SubtaskResult {
	t.Helper()
	if req.TimeoutMS == 0 {
		req.TimeoutMS = 30_000
	}
	if req.MaxSteps == 0 {
		req.MaxSteps = 6
	}
	id, err := r.Spawn(context.Background(), req)
	if err != nil {
		t.Fatalf("Spawn(%s): %v", req.SubagentType, err)
	}
	res, err := r.Wait(context.Background(), id, 30_000)
	if err != nil {
		t.Fatalf("Wait(%s): %v", id, err)
	}
	return res
}

// ── delegation along flows ──────────────────────────────────────────────────

// The spec's shape — Orchestrator → Dept Lead → Scout — was promised by the
// architecture prompt ("you may use task(subagent_type=explore)") and refused
// by the runtime, which gave no child a spawn tool at all.
func TestAgency_LeadDelegatesAlongFlows(t *testing.T) {
	m := &scriptLLM{}
	m.reply = func(req llm.CompleteRequest) llm.Message {
		text := conversation(req)
		switch {
		case strings.Contains(text, "LEAD-GOAL"):
			if steps(req) == 0 {
				return callTool("task", map[string]any{"subagent_type": "explore", "prompt": "SCOUT-GOAL"})
			}
			return finish("brief written")
		case strings.Contains(text, "SCOUT-GOAL"):
			return finish("found it in billing.go")
		}
		return finish("?")
	}
	r, _ := newAgencyRunner(t, m, ChildAgentConfig{Agency: agencyOn()})

	res := spawnAndWait(t, r, agent.SubtaskSpawnRequest{Goal: "LEAD-GOAL", SubagentType: "architecture", Dept: "backend"})
	if res.Status != "done" || !strings.Contains(res.Result, "brief written") {
		t.Fatalf("lead result = %+v", res)
	}

	lead := m.requests("LEAD-GOAL")[0]
	if !toolNames(lead)["task"] {
		t.Fatalf("a Dept Lead with flows to explore must get the task tool; tools = %v", toolNames(lead))
	}
	if !strings.Contains(systemPrompt(lead), `<available_agents self="backend" depth=1>`) {
		t.Fatalf("lead system prompt lacks its agency block:\n%s", systemPrompt(lead))
	}
	scout := m.requests("SCOUT-GOAL")[0]
	if toolNames(scout)["task"] {
		t.Fatal("depth 2 is the limit: the scout must not be able to delegate further")
	}

	board := r.Board()
	if len(board) != 2 {
		t.Fatalf("board = %+v, want the lead and its scout", board)
	}
	if b := board[1]; b.Role != "explore" || b.Depth != 2 || b.Parent != "backend" || b.Status != "done" {
		t.Fatalf("scout row = %+v", b)
	}
}

func TestAgency_ChildWithoutFlowIsRefused(t *testing.T) {
	m := &scriptLLM{reply: func(llm.CompleteRequest) llm.Message { return finish("ok") }}
	r, _ := newAgencyRunner(t, m, ChildAgentConfig{Agency: agencyOn()})

	explore := &scopedRunner{r: r, s: rootScope().child("explore", "explore", "explore", "task_x")}
	_, err := explore.Spawn(context.Background(), agent.SubtaskSpawnRequest{Goal: `{"intent":"x"}`, SubagentType: "worker"})
	if err == nil || !strings.Contains(err.Error(), "no flow explore > worker") {
		t.Fatalf("explore → worker must be refused by the flows, got %v", err)
	}
	if info := explore.AgencyInfo(); len(info.Delegates) != 0 {
		t.Fatalf("explore has no outgoing flows, but AgencyInfo offers %+v", info.Delegates)
	}
}

func TestAgency_MaxDepthStopsNesting(t *testing.T) {
	m := &scriptLLM{reply: func(llm.CompleteRequest) llm.Message { return finish("ok") }}
	settings := agencyOn()
	settings.MaxDepth = 1
	r, _ := newAgencyRunner(t, m, ChildAgentConfig{Agency: settings})

	res := spawnAndWait(t, r, agent.SubtaskSpawnRequest{Goal: "LEAD-GOAL", SubagentType: "architecture"})
	if res.Status != "done" {
		t.Fatalf("lead: %+v", res)
	}
	if toolNames(m.requests("LEAD-GOAL")[0])["task"] {
		t.Fatal("max_depth=1: a child must not get spawn tools")
	}
	lead := &scopedRunner{r: r, s: rootScope().child("architecture", "architecture", "architecture", "task_y")}
	if _, err := lead.Spawn(context.Background(), agent.SubtaskSpawnRequest{Goal: "x", SubagentType: "explore"}); err == nil || !strings.Contains(err.Error(), "max_depth=1") {
		t.Fatalf("spawn past max_depth must be refused, got %v", err)
	}
}

func TestAgency_OffKeepsTheOneLevelRunner(t *testing.T) {
	m := &scriptLLM{reply: func(llm.CompleteRequest) llm.Message { return finish("ok") }}
	r, _ := newAgencyRunner(t, m, ChildAgentConfig{})

	spawnAndWait(t, r, agent.SubtaskSpawnRequest{Goal: "LEAD-GOAL", SubagentType: "architecture"})
	names := toolNames(m.requests("LEAD-GOAL")[0])
	for _, n := range []string{"task", "send_message", "agent_post", "task_board"} {
		if names[n] {
			t.Fatalf("agency off: child must not get %q", n)
		}
	}
	if _, err := r.Post(context.Background(), agent.AgentPostRequest{To: "qa", Message: "hi"}); err == nil {
		t.Fatal("agent_post must be refused with the agency off")
	}
}

// ── custom agents ───────────────────────────────────────────────────────────

func TestAgency_CustomAgentRunsAsItsBase(t *testing.T) {
	m := &scriptLLM{reply: func(llm.CompleteRequest) llm.Message { return finish("reviewed") }}
	var guarded []string
	r, _ := newAgencyRunner(t, m, ChildAgentConfig{
		Agency: agencyOn(),
		Agents: []AgentProfile{{
			Name:         "billing-lead",
			Description:  "owns the billing API",
			Base:         "architecture",
			SystemPrompt: "You own the billing service. Money is integer cents.",
			Tools:        []string{"read", "grep"},
		}},
		GuardSpawn: func(role string) error {
			guarded = append(guarded, role)
			return nil
		},
	})

	res := spawnAndWait(t, r, agent.SubtaskSpawnRequest{Goal: "CUSTOM-GOAL", SubagentType: "billing-lead"})
	if res.Status != "done" {
		t.Fatalf("custom agent: %+v", res)
	}
	if len(guarded) != 1 || guarded[0] != "architecture" {
		t.Fatalf("phase guard must judge a custom agent by its base, got %v", guarded)
	}
	req := m.requests("CUSTOM-GOAL")[0]
	if !strings.Contains(conversation(req), `<agent_role name="billing-lead" base="architecture">`) ||
		!strings.Contains(conversation(req), "integer cents") {
		t.Fatalf("custom agent's instructions missing from its goal:\n%s", conversation(req))
	}
	names := toolNames(req)
	if !names["read"] || !names["grep"] || !names["task_result"] || names["write"] {
		t.Fatalf("custom agent tools must be its own list plus task_result, got %v", names)
	}
	if !names["task"] {
		t.Fatal("a custom agent inherits its base's flows (architecture → explore/worker)")
	}
}

// ── dependencies & concurrency ──────────────────────────────────────────────

func TestAgency_DependsOnHandsOverUpstreamResults(t *testing.T) {
	m := &scriptLLM{reply: func(req llm.CompleteRequest) llm.Message {
		switch text := conversation(req); {
		case strings.Contains(text, "B-GOAL"):
			return finish("b done")
		case strings.Contains(text, "A-GOAL"):
			return finish("alpha: the schema is in db/schema.sql")
		}
		return finish("?")
	}}
	r, _ := newAgencyRunner(t, m, ChildAgentConfig{Agency: agencyOn()})

	aID, err := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{Goal: "A-GOAL", SubagentType: "general", Key: "a", TimeoutMS: 30_000})
	if err != nil {
		t.Fatal(err)
	}
	res := spawnAndWait(t, r, agent.SubtaskSpawnRequest{Goal: "B-GOAL", SubagentType: "general", DependsOn: []string{"a"}})
	if res.Status != "done" {
		t.Fatalf("B: %+v", res)
	}
	b := m.requests("B-GOAL")[0]
	if !strings.Contains(conversation(b), "<upstream_results>") || !strings.Contains(conversation(b), "db/schema.sql") {
		t.Fatalf("B did not get A's result:\n%s", conversation(b))
	}
	if _, err := r.Wait(context.Background(), aID, 1000); err != nil {
		t.Fatalf("A: %v", err)
	}

	if _, err := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{Goal: "x", SubagentType: "general", DependsOn: []string{"nope"}}); err == nil || !strings.Contains(err.Error(), `unknown task "nope"`) {
		t.Fatalf("an unknown dependency must be refused at spawn, got %v", err)
	}
}

func TestAgency_FailedDependencyBlocksTheDependent(t *testing.T) {
	m := &scriptLLM{reply: func(req llm.CompleteRequest) llm.Message {
		// The worker claims success without editing anything: no_changes,
		// which is not a success its dependents can build on.
		return finish(`{"status":"success","summary":"did it"}`)
	}}
	r, _ := newAgencyRunner(t, m, ChildAgentConfig{Agency: agencyOn()})

	if _, err := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{
		Goal: `{"task_id":"schema","intent":"add the table","target_files":["db.sql"]}`, SubagentType: "worker", MaxSteps: 3, TimeoutMS: 30_000,
	}); err != nil {
		t.Fatal(err)
	}
	res := spawnAndWait(t, r, agent.SubtaskSpawnRequest{
		Goal: `{"task_id":"api","intent":"DEPENDENT-GOAL","target_files":["api.go"],"depends_on":["schema"]}`, SubagentType: "worker",
	})
	if res.Status != "error" || !strings.Contains(res.Error, "dependency_unmet") || !strings.Contains(res.Result, `"blocked_reason":"dependency_unmet"`) {
		t.Fatalf("dependent of a failed WorkOrder must be blocked, got %+v", res)
	}
	if len(m.requests("DEPENDENT-GOAL")) != 0 {
		t.Fatal("the dependent must not run at all")
	}
}

func TestAgency_MaxParallelQueuesPerDepth(t *testing.T) {
	mock := &gatedWorkerLLM{release: make(chan struct{})}
	settings := agencyOn()
	settings.MaxParallel = 1
	r, _ := newAgencyRunner(t, mock, ChildAgentConfig{Agency: settings})
	released := false
	t.Cleanup(func() {
		if !released {
			close(mock.release)
		}
	})

	var ids []string
	for _, g := range []string{"one", "two"} {
		id, err := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{Goal: g, SubagentType: "general", MaxSteps: 2, TimeoutMS: 30_000})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	waitForCalls(t, mock, 1)
	time.Sleep(150 * time.Millisecond)
	if got := mock.callCount(); got != 1 {
		t.Fatalf("max_parallel=1: %d children running, want 1", got)
	}
	board := r.Board()
	queued := 0
	for _, row := range board {
		if row.Status == "queued" {
			queued++
		}
	}
	if queued != 1 {
		t.Fatalf("board must show one queued child: %+v", board)
	}
	close(mock.release)
	released = true
	res, err := r.WaitMany(context.Background(), ids, 30_000)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range res.Results {
		if x.Status != "done" {
			t.Fatalf("result %+v", x)
		}
	}
}

// ── conversations ───────────────────────────────────────────────────────────

func TestAgency_SendMessageKeepsTheConversation(t *testing.T) {
	m := &scriptLLM{reply: func(req llm.CompleteRequest) llm.Message {
		text := conversation(req)
		if strings.Contains(text, "second question") {
			if strings.Contains(text, "first question") && strings.Contains(text, "first answer") {
				return finish("I remember: first answer")
			}
			return finish("no memory")
		}
		return finish("first answer")
	}}
	r, root := newAgencyRunner(t, m, ChildAgentConfig{Agency: agencyOn()})

	first, err := r.SendMessage(context.Background(), agent.AgentMessageRequest{To: "backend", Message: "first question"})
	if err != nil || first.Reply != "first answer" {
		t.Fatalf("first: %+v %v", first, err)
	}
	if !strings.Contains(conversation(m.requests("first question")[0]), "Message from orchestrator") {
		t.Fatal("the recipient must be told who is asking")
	}
	second, err := r.SendMessage(context.Background(), agent.AgentMessageRequest{To: "backend", Message: "second question"})
	if err != nil {
		t.Fatal(err)
	}
	if second.Reply != "I remember: first answer" || second.Turns != 2 {
		t.Fatalf("the second message must continue the first conversation, got %+v", second)
	}
	var thread threadFile
	if err := readJSONFile(filepath.Join(root, ".orchestra", "agency", "threads", "orchestrator__backend.json"), &thread); err != nil {
		t.Fatalf("thread not persisted: %v", err)
	}
	if len(thread.Messages) != 4 {
		t.Fatalf("thread = %+v", thread)
	}
	board := r.Board()
	if board[0].Role != "architecture" || board[0].Agent != "backend" {
		t.Fatalf("a department is answered by its Lead: %+v", board[0])
	}
}

func TestAgency_SendMessageRefusals(t *testing.T) {
	m := &scriptLLM{reply: func(llm.CompleteRequest) llm.Message { return finish("ok") }}
	r, _ := newAgencyRunner(t, m, ChildAgentConfig{Agency: agencyOn()})
	ctx := context.Background()

	if _, err := r.SendMessage(ctx, agent.AgentMessageRequest{To: "worker", Message: "x"}); err == nil || !strings.Contains(err.Error(), "WorkOrders") {
		t.Fatalf("messaging a worker must point at task_spawn, got %v", err)
	}
	backend := rootScope().child("backend", "architecture", "architecture", "task_b")
	frontend := backend.child("frontend", "architecture", "architecture", "task_f")
	if _, err := r.sendMessage(ctx, frontend, agent.AgentMessageRequest{To: "backend", Message: "x"}); err == nil || !strings.Contains(err.Error(), "waiting on you") {
		t.Fatalf("messaging an ancestor must be refused (deadlock), got %v", err)
	}
	qa := rootScope().child("qa", "architecture", "architecture", "task_q")
	if _, err := r.sendMessage(ctx, qa, agent.AgentMessageRequest{To: "security", Message: "x"}); err == nil || !strings.Contains(err.Error(), "no flow qa > security") {
		t.Fatalf("a child without the edge must be refused, got %v", err)
	}
}

func TestAgency_DepartmentFlowsEnableConversation(t *testing.T) {
	m := &scriptLLM{reply: func(llm.CompleteRequest) llm.Message { return finish("the endpoint is /v1/invoices") }}
	settings := agencyOn()
	flows, err := config.ParseAgencyFlows([]string{"frontend > backend"})
	if err != nil {
		t.Fatal(err)
	}
	settings.Flows = append(settings.Flows, flows...)
	r, _ := newAgencyRunner(t, m, ChildAgentConfig{Agency: settings})

	fe := &scopedRunner{r: r, s: rootScope().child("frontend@web", "architecture", "architecture", "task_fe")}
	info := fe.AgencyInfo()
	found := false
	for _, c := range info.Contacts {
		found = found || c.Name == "backend"
	}
	if !found {
		t.Fatalf("frontend@web has the edge frontend > backend; contacts = %+v", info.Contacts)
	}
	reply, err := fe.SendMessage(context.Background(), agent.AgentMessageRequest{To: "backend", Message: "which endpoint?"})
	if err != nil || !strings.Contains(reply.Reply, "/v1/invoices") {
		t.Fatalf("reply %+v, err %v", reply, err)
	}
}

// ── notes ───────────────────────────────────────────────────────────────────

func TestAgency_PostToIdleDepartmentWaitsInItsInbox(t *testing.T) {
	m := &scriptLLM{reply: func(llm.CompleteRequest) llm.Message { return finish("ok") }}
	r, root := newAgencyRunner(t, m, ChildAgentConfig{Agency: agencyOn()})

	receipt, err := r.Post(context.Background(), agent.AgentPostRequest{To: "qa", Kind: "handoff", Message: "INBOX-NOTE: OpenAPI v1 is frozen"})
	if err != nil || receipt.Delivered != "inbox" {
		t.Fatalf("receipt %+v, err %v", receipt, err)
	}
	inbox := filepath.Join(root, ".orchestra", "agency", "inbox", "qa.json")
	if _, err := os.Stat(inbox); err != nil {
		t.Fatalf("note not persisted: %v", err)
	}
	spawnAndWait(t, r, agent.SubtaskSpawnRequest{Goal: "QA-GOAL", SubagentType: "architecture", Dept: "qa@web"})
	req := m.requests("QA-GOAL")[0]
	if !strings.Contains(conversation(req), "[handoff from orchestrator] INBOX-NOTE") {
		t.Fatalf("qa@web must read the note left for its department:\n%s", conversation(req))
	}
	if _, err := os.Stat(inbox); !os.IsNotExist(err) {
		t.Fatal("a delivered note must leave the inbox")
	}
}

func TestAgency_PostReachesARunningAgentOnItsNextStep(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	m := &scriptLLM{}
	m.reply = func(req llm.CompleteRequest) llm.Message {
		if steps(req) == 0 {
			once.Do(func() { close(started) })
			<-release
			return callTool("ls", map[string]string{"path": "."})
		}
		if strings.Contains(conversation(req), "LIVE-NOTE") {
			return finish("saw the note")
		}
		return finish("no note")
	}
	r, _ := newAgencyRunner(t, m, ChildAgentConfig{Agency: agencyOn()})

	id, err := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{Goal: "LIVE-GOAL", SubagentType: "general", Dept: "design", MaxSteps: 4, TimeoutMS: 30_000})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	receipt, err := r.Post(context.Background(), agent.AgentPostRequest{To: "design", Message: "LIVE-NOTE: tokens changed"})
	close(release)
	if err != nil || receipt.Delivered != "live" {
		t.Fatalf("receipt %+v, err %v", receipt, err)
	}
	res, err := r.Wait(context.Background(), id, 30_000)
	if err != nil || res.Result != "saw the note" {
		t.Fatalf("running agent must see the note on its next step: %+v %v", res, err)
	}
}

func TestAgency_ContractChangeRequestIsCopiedToTheHub(t *testing.T) {
	m := &scriptLLM{reply: func(llm.CompleteRequest) llm.Message { return finish("ok") }}
	r, _ := newAgencyRunner(t, m, ChildAgentConfig{Agency: agencyOn()})
	fe := &scopedRunner{r: r, s: rootScope().child("frontend", "architecture", "architecture", "task_fe")}

	if _, err := fe.Post(context.Background(), agent.AgentPostRequest{To: "backend", Kind: "contract_change_request", Message: "add ?page="}); err == nil {
		t.Fatal("a contract_change_request without artifact must be refused")
	}
	if _, err := fe.Post(context.Background(), agent.AgentPostRequest{To: "backend", Kind: "contract_change_request", Artifact: ".orchestra/contract/OpenAPI.v0.yaml", Message: "add ?page="}); err != nil {
		t.Fatal(err)
	}
	hub := r.DrainInbox()
	if len(hub) != 1 || hub[0].Kind != "contract_change_request" || hub[0].From != "frontend" {
		t.Fatalf("the Orchestrator must see every change request: %+v", hub)
	}
	if len(r.DrainInbox()) != 0 {
		t.Fatal("DrainInbox must clear")
	}
}

func TestAgency_MessageBudget(t *testing.T) {
	m := &scriptLLM{reply: func(llm.CompleteRequest) llm.Message { return finish("ok") }}
	settings := agencyOn()
	settings.MaxMessages = 1
	r, _ := newAgencyRunner(t, m, ChildAgentConfig{Agency: settings})

	if _, err := r.Post(context.Background(), agent.AgentPostRequest{To: "qa", Message: "one"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Post(context.Background(), agent.AgentPostRequest{To: "qa", Message: "two"}); err == nil || !strings.Contains(err.Error(), "budget") {
		t.Fatalf("second message must hit the budget, got %v", err)
	}
}

// ── WorkOrder relay ─────────────────────────────────────────────────────────

func TestAgency_LeadBatchWorkOrdersAreRelayed(t *testing.T) {
	batch := map[string]any{
		"summary": "brief ready",
		"batch_workorders": []map[string]any{
			{"task_id": "w1", "intent": "RELAY-ONE", "target_files": []string{"a.go"}},
			{"task_id": "w2", "intent": "RELAY-TWO", "target_files": []string{"b.go"}, "depends_on": []string{"w1"}},
		},
	}
	leadOut, _ := json.Marshal(batch)
	m := &scriptLLM{reply: func(req llm.CompleteRequest) llm.Message {
		if strings.Contains(conversation(req), "LEAD-GOAL") {
			return finish(string(leadOut))
		}
		// Workers report success without editing: no_changes.
		return finish(`{"status":"success"}`)
	}}
	r, _ := newAgencyRunner(t, m, ChildAgentConfig{Agency: agencyOn()})

	res := spawnAndWait(t, r, agent.SubtaskSpawnRequest{Goal: "LEAD-GOAL", SubagentType: "architecture", Dept: "backend"})
	var out struct {
		Summary string           `json:"summary"`
		Batch   []map[string]any `json:"batch_workorders"`
		Relayed struct {
			TaskIDs  []string `json:"task_ids"`
			Rejected []string `json:"rejected"`
		} `json:"relayed"`
	}
	if err := json.Unmarshal([]byte(res.Result), &out); err != nil {
		t.Fatalf("lead result is not JSON: %v\n%s", err, res.Result)
	}
	if len(out.Relayed.TaskIDs) != 2 || len(out.Relayed.Rejected) != 0 || out.Summary != "brief ready" {
		t.Fatalf("relay = %+v", out)
	}
	if out.Batch[0]["intent"] != "RELAY-ONE" {
		t.Fatalf("the Lead's result keeps a summary of each WorkOrder: %+v", out.Batch)
	}

	// The workers belong to the Orchestrator: it collects them.
	wm, err := r.WaitMany(context.Background(), out.Relayed.TaskIDs, 30_000)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(wm.Results[0].Result, "no_changes") {
		t.Fatalf("w1: %+v", wm.Results[0])
	}
	if !strings.Contains(wm.Results[1].Error, "dependency_unmet") {
		t.Fatalf("w2 depends on w1, which changed nothing: %+v", wm.Results[1])
	}
	for _, row := range r.Board() {
		if row.Role == "worker" && (row.Parent != "orchestrator" || row.Agent != "backend" || row.Depth != 2) {
			t.Fatalf("relayed worker row = %+v", row)
		}
	}
	if !strings.Contains(conversation(m.requests("RELAY-ONE")[0]), ".orchestra/depts/backend.md") {
		t.Fatal("relayed WorkOrders default to the Lead's department scratchpad")
	}
}

func TestAgency_RelayNeedsTheLeadsFlowToWorker(t *testing.T) {
	m := &scriptLLM{reply: func(llm.CompleteRequest) llm.Message {
		return finish(`{"batch_workorders":[{"intent":"write code","target_files":["x.go"]}]}`)
	}}
	r, _ := newAgencyRunner(t, m, ChildAgentConfig{Agency: agencyOn()})

	res := spawnAndWait(t, r, agent.SubtaskSpawnRequest{Goal: "PRD", SubagentType: "product"})
	if !strings.Contains(res.Result, "no flow product > worker") {
		t.Fatalf("Product has no edge to worker; its WorkOrders must be refused: %s", res.Result)
	}
}

func TestOrderWorkOrders(t *testing.T) {
	raws := []json.RawMessage{
		json.RawMessage(`{"task_id":"c","depends_on":["b"]}`),
		json.RawMessage(`{"task_id":"a"}`),
		json.RawMessage(`{"task_id":"b","depends_on":["a"]}`),
		json.RawMessage(`{"task_id":"x","depends_on":["y"]}`),
		json.RawMessage(`{"task_id":"y","depends_on":["x"]}`),
	}
	order, cycle := orderWorkOrders(raws)
	pos := map[int]int{}
	for i, v := range order {
		pos[v] = i
	}
	if len(order) != 3 || pos[1] > pos[2] || pos[2] > pos[0] {
		t.Fatalf("order = %v, want a before b before c", order)
	}
	if len(cycle) != 2 {
		t.Fatalf("cycle = %v, want x and y", cycle)
	}
}

// ── integration ─────────────────────────────────────────────────────────────

// Two workers each change their own file; task_wait over both verifies the
// edits together.
func TestAgency_WaitManyVerifiesWorkersTogether(t *testing.T) {
	m := &scriptLLM{}
	m.reply = func(req llm.CompleteRequest) llm.Message {
		file := "a.txt"
		if strings.Contains(conversation(req), "EDIT-B") {
			file = "b.txt"
		}
		switch steps(req) {
		case 0:
			return callTool("read", map[string]string{"path": file})
		case 1:
			return callTool("edit", map[string]string{"path": file, "search": "old", "replace": "new"})
		}
		return finish(`{"status":"success","path":"` + file + `"}`)
	}
	r, root := newAgencyRunner(t, m, ChildAgentConfig{Agency: agencyOn()})
	for _, f := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(root, f), []byte("old\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var ids []string
	for _, g := range []string{`{"intent":"EDIT-A","target_files":["a.txt"]}`, `{"intent":"EDIT-B","target_files":["b.txt"]}`} {
		id, err := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{Goal: g, SubagentType: "worker", MaxSteps: 6, TimeoutMS: 30_000})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	wm, err := r.WaitMany(context.Background(), ids, 30_000)
	if err != nil {
		t.Fatal(err)
	}
	for _, res := range wm.Results {
		if !workerOutcomeSucceeded(res.Result) {
			t.Fatalf("worker failed: %+v", res)
		}
	}
	var rep IntegrationReport
	if err := json.Unmarshal(wm.Integration, &rep); err != nil {
		t.Fatalf("integration report missing: %v (%s)", err, wm.Integration)
	}
	if rep.Workers != 2 || rep.Status != "passed" || len(rep.Files) != 2 {
		t.Fatalf("integration = %+v", rep)
	}
}

func TestAgency_ChildWaitsOnlyForItsOwnTasks(t *testing.T) {
	m := &scriptLLM{reply: func(llm.CompleteRequest) llm.Message { return finish("ok") }}
	r, _ := newAgencyRunner(t, m, ChildAgentConfig{Agency: agencyOn()})

	id, err := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{Goal: "mine", SubagentType: "explore", TimeoutMS: 30_000})
	if err != nil {
		t.Fatal(err)
	}
	other := &scopedRunner{r: r, s: rootScope().child("backend", "architecture", "architecture", "task_other")}
	if _, err := other.Wait(context.Background(), id, 1000); err == nil || !strings.Contains(err.Error(), "not by you") {
		t.Fatalf("a child must not collect another agent's task, got %v", err)
	}
	if _, err := r.Wait(context.Background(), id, 30_000); err != nil {
		t.Fatal(err)
	}
}

func TestAgencyFromConfig(t *testing.T) {
	cfg := &config.ProjectConfig{
		Agents: []config.AgentDefinition{
			{Name: "billing-lead", Base: "architecture", Description: "billing"},
			{Name: "Weird Name"},
		},
	}
	s, profiles := AgencyFromConfig(cfg, "orchestra")
	if !s.Enabled || s.MaxDepth != 2 || s.MaxParallel != 4 || len(s.Flows) != len(config.DefaultAgencyFlows()) {
		t.Fatalf("orchestra defaults: %+v", s)
	}
	if len(profiles) != 1 || profiles[0].Base != "architecture" {
		t.Fatalf("profiles = %+v (an unaddressable name is skipped)", profiles)
	}
	if s, _ := AgencyFromConfig(cfg, "build"); s.Enabled {
		t.Fatal("the agency is off by default outside orchestra mode")
	}
	cfg.Agency.Flows = []string{"billing-lead > worker"}
	if s, _ := AgencyFromConfig(cfg, "build"); !s.Enabled || len(s.Flows) != len(config.DefaultAgencyFlows())+1 {
		t.Fatalf("explicit flows turn the agency on in any mode: %+v", s)
	}
}

// agents: are subagents in any mode: with the agency off (a build turn) the
// root may still start them by name, and its enum must list them.
func TestAgency_CustomAgentsWithTheAgencyOff(t *testing.T) {
	m := &scriptLLM{reply: func(llm.CompleteRequest) llm.Message { return finish("reviewed") }}
	r, _ := newAgencyRunner(t, m, ChildAgentConfig{
		Agents: []AgentProfile{{Name: "reviewer", Base: "verifier", Description: "strict review"}},
	})
	info := r.AgencyInfo()
	if info.Enabled {
		t.Fatal("agency must stay off")
	}
	var names []string
	for _, c := range info.Delegates {
		names = append(names, c.Name)
	}
	if !strings.Contains(strings.Join(names, ","), "reviewer") {
		t.Fatalf("root delegates must include custom agents: %v", names)
	}
	res := spawnAndWait(t, r, agent.SubtaskSpawnRequest{Goal: "REVIEW-GOAL", SubagentType: "reviewer"})
	if res.Status != "done" {
		t.Fatalf("custom agent with the agency off: %+v", res)
	}
	if toolNames(m.requests("REVIEW-GOAL")[0])["write"] {
		t.Fatal("reviewer runs as its base (verifier): no write")
	}
}

// A scout sent out by the backend Lead is not the backend department: it
// must not inherit the address, and so must not read notes left for the
// Lead. The Lead's workers do inherit it.
func TestAgency_OnlyWorkersInheritTheLeadsDepartment(t *testing.T) {
	m := &scriptLLM{reply: func(llm.CompleteRequest) llm.Message { return finish(`{"status":"success"}`) }}
	r, _ := newAgencyRunner(t, m, ChildAgentConfig{Agency: agencyOn()})
	lead := &scopedRunner{r: r, s: rootScope().child("backend", "architecture", "architecture", "task_lead")}
	lead.s.dept = "backend"
	if _, err := r.Post(context.Background(), agent.AgentPostRequest{To: "backend", Message: "FOR-THE-LEAD"}); err != nil {
		t.Fatal(err)
	}

	spawnAndWait(t, lead, agent.SubtaskSpawnRequest{Goal: "SCOUT-X", SubagentType: "scout"})
	spawnAndWait(t, lead, agent.SubtaskSpawnRequest{Goal: `{"intent":"WORKER-X","target_files":["x.go"]}`, SubagentType: "worker"})

	board := r.Board()
	if board[0].Agent != "scout" || board[1].Agent != "backend" {
		t.Fatalf("scout keeps its role address, the worker takes the department's: %+v", board)
	}
	for _, marker := range []string{"SCOUT-X", "WORKER-X"} {
		if strings.Contains(conversation(m.requests(marker)[0]), "FOR-THE-LEAD") {
			t.Fatalf("%s consumed a note left for the Lead", marker)
		}
	}
}

func TestAgency_WorkerPostsReachSiblingsNotItself(t *testing.T) {
	m := &scriptLLM{reply: func(llm.CompleteRequest) llm.Message { return finish("ok") }}
	r, _ := newAgencyRunner(t, m, ChildAgentConfig{Agency: agencyOn()})
	worker := &scopedRunner{r: r, s: rootScope().child("backend", "worker", "worker", "task_w1")}
	receipt, err := worker.Post(context.Background(), agent.AgentPostRequest{To: "backend", Message: "renamed Sum to Add"})
	if err != nil {
		t.Fatalf("a worker posting to its department must be allowed: %v", err)
	}
	if receipt.Delivered != "inbox" {
		t.Fatalf("no sibling running → inbox, got %+v", receipt)
	}
	if _, err := worker.Post(context.Background(), agent.AgentPostRequest{To: "task_w1", Message: "x"}); err == nil {
		t.Fatal("posting to your own task id is talking to yourself")
	}
	if _, err := r.Post(context.Background(), agent.AgentPostRequest{To: "orchestrator", Message: "x"}); err == nil {
		t.Fatal("the root posting to itself must be refused")
	}
}

// ── read-only spawners (plan, architecture, ask at the top level) ───────────

func TestReadOnlySpawnerGetsOnlyReaders(t *testing.T) {
	m := &scriptLLM{reply: func(llm.CompleteRequest) llm.Message { return finish("ok") }}
	var guarded []string
	r, _ := newAgencyRunner(t, m, ChildAgentConfig{
		Agency: agencyOn(),
		Agents: []AgentProfile{
			{Name: "sneaky", Base: "explore", Tools: []string{"read", "write"}},
			{Name: "reviewer", Base: "verifier"},
		},
		GuardSpawn: func(role string) error {
			guarded = append(guarded, role)
			return nil
		},
	})
	ctx := context.Background()
	for _, sub := range []string{"worker", "general", "debug", "sneaky"} {
		_, err := r.Spawn(ctx, agent.SubtaskSpawnRequest{Goal: "x", SubagentType: sub, ReadOnlyChildren: true})
		if err == nil || !strings.Contains(err.Error(), "only reads") {
			t.Fatalf("%s from a read-only turn must be refused, got %v", sub, err)
		}
	}
	for _, sub := range []string{"explore", "scout", "reviewer"} {
		res := spawnAndWait(t, r, agent.SubtaskSpawnRequest{Goal: "READ-" + sub, SubagentType: sub, ReadOnlyChildren: true})
		if res.Status != "done" {
			t.Fatalf("%s must still run for a read-only turn: %+v", sub, res)
		}
	}

	// Outside a read-only turn the custom writer runs, and the phase guard
	// judges it as the writer it is, not by its read-only base.
	guarded = nil
	res := spawnAndWait(t, r, agent.SubtaskSpawnRequest{Goal: "SNEAKY", SubagentType: "sneaky"})
	if res.Status != "done" {
		t.Fatalf("sneaky: %+v", res)
	}
	if len(guarded) != 1 || guarded[0] != "general" {
		t.Fatalf("an explore-based agent with write is guarded as a writer, got %v", guarded)
	}
}

// After Close the turn is over: a relay or task_spawn racing it is refused
// instead of registering a child nothing will ever cancel.
func TestSpawnAfterCloseIsRefused(t *testing.T) {
	m := &scriptLLM{reply: func(llm.CompleteRequest) llm.Message { return finish("ok") }}
	r, _ := newAgencyRunner(t, m, ChildAgentConfig{Agency: agencyOn()})
	r.Close()
	if _, err := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{Goal: "late", SubagentType: "explore"}); err != ErrRunnerClosed {
		t.Fatalf("spawn after Close = %v, want ErrRunnerClosed", err)
	}
}
