package tui

import (
	"strings"
	"testing"

	"github.com/orchestra/orchestra/ui/tui/rpcclient"
	"github.com/orchestra/orchestra/ui/tui/state"
)

// startedTurnApp wires a fake core and starts a user turn so streaming events
// have an active assistant message to land on.
func startedTurnApp(t *testing.T) (*App, *fakeCore) {
	t.Helper()
	a, f := testCoreApp(t)
	a.currentSessionID = "sess-1"
	cmd := a.submitUserMessage("сделай дело")
	if cmd == nil || !a.turn.IsRunning() {
		t.Fatal("turn must be running after submit")
	}
	execCmdTree(cmd)
	return a, f
}

// hasSystemNotice scans session messages (incl. assistant segments) for text.
func hasSystemNotice(a *App, substr string) bool {
	for _, m := range a.session.Messages {
		if strings.Contains(m.Text, substr) {
			return true
		}
		for _, seg := range m.Segments {
			if strings.Contains(seg.Text, substr) {
				return true
			}
		}
	}
	return false
}

func lastAssistant(t *testing.T, a *App) state.Message {
	t.Helper()
	for i := len(a.session.Messages) - 1; i >= 0; i-- {
		if a.session.Messages[i].Role == state.RoleAssistant {
			return a.session.Messages[i]
		}
	}
	t.Fatal("no assistant message in session")
	return state.Message{}
}

func TestStreamingTurn_DeltasAccumulateAndFinish(t *testing.T) {
	a, _ := startedTurnApp(t)

	a.handleRPCEvent(rpcclient.Event{Kind: rpcclient.EventMessageDelta, Content: "Прив"})
	a.handleRPCEvent(rpcclient.Event{Kind: rpcclient.EventMessageDelta, Content: "ет"})
	a.handleRPCEvent(rpcclient.Event{Kind: rpcclient.EventDone})
	a.handleRPCEvent(rpcclient.Event{Kind: rpcclient.EventAgentRunCompleted})

	if a.turn.IsRunning() {
		t.Fatal("turn must be idle after AgentRunCompleted")
	}
	m := lastAssistant(t, a)
	if m.Streaming {
		t.Fatal("assistant message must be finished")
	}
	if m.Text != "Привет" {
		t.Fatalf("text=%q, want «Привет»", m.Text)
	}
}

func TestStreamingTurn_ThinkTagsRoutedToReasoning(t *testing.T) {
	a, _ := startedTurnApp(t)

	a.handleRPCEvent(rpcclient.Event{Kind: rpcclient.EventMessageDelta,
		Content: "<think>план действий</think>ответ"})
	a.handleRPCEvent(rpcclient.Event{Kind: rpcclient.EventDone})
	a.handleRPCEvent(rpcclient.Event{Kind: rpcclient.EventAgentRunCompleted})

	m := lastAssistant(t, a)
	if !strings.Contains(m.Reasoning, "план действий") {
		t.Fatalf("reasoning=%q, want think content", m.Reasoning)
	}
	if m.Text != "ответ" {
		t.Fatalf("text=%q, want «ответ» without think tags", m.Text)
	}
}

func TestTurnError_SurfacesOnCompletion(t *testing.T) {
	a, _ := startedTurnApp(t)

	a.handleRPCEvent(rpcclient.Event{Kind: rpcclient.EventError, Err: "llm timeout"})
	if a.turnError != "llm timeout" {
		t.Fatalf("turnError=%q", a.turnError)
	}
	a.handleRPCEvent(rpcclient.Event{Kind: rpcclient.EventAgentRunCompleted})

	if a.turn.IsRunning() {
		t.Fatal("turn must be idle after error completion")
	}
	if a.turnError != "" {
		t.Fatal("turnError must be consumed")
	}
	if !hasSystemNotice(a, "llm timeout") {
		t.Fatal("error text must be shown in chat")
	}
}

func TestConnectionClosed_MidTurnFailsTurn(t *testing.T) {
	a, _ := startedTurnApp(t)

	a.handleRPCEvent(rpcclient.Event{Kind: rpcclient.EventConnectionClosed})

	if a.turn.IsRunning() {
		t.Fatal("turn must not stay running after connection loss")
	}
	if !hasSystemNotice(a, "оборвалось") {
		t.Fatal("user must see a connection-lost notice")
	}
}

func TestConnectionClosed_IdleIsSilent(t *testing.T) {
	a, _ := testCoreApp(t)
	before := len(a.session.Messages)

	a.handleRPCEvent(rpcclient.Event{Kind: rpcclient.EventConnectionClosed})

	if len(a.session.Messages) != before {
		t.Fatal("idle connection close must not spam the chat")
	}
}

func TestToolLifecycle_StatusTransitions(t *testing.T) {
	a, _ := startedTurnApp(t)

	a.handleRPCEvent(rpcclient.Event{Kind: rpcclient.EventToolCallStart,
		ToolCallID: "t1", ToolCallName: "read"})
	tb, ok := a.session.FindToolBlock("t1")
	if !ok || tb.Status != state.ToolBlockRunning {
		t.Fatalf("tool must be running: ok=%v status=%v", ok, tb.Status)
	}

	a.handleRPCEvent(rpcclient.Event{Kind: rpcclient.EventToolCallDelta,
		ToolCallID: "t1", ArgsDelta: `{"path":"a.go"}`})
	tb, _ = a.session.FindToolBlock("t1")
	if tb.ArgsRaw != `{"path":"a.go"}` {
		t.Fatalf("args=%q", tb.ArgsRaw)
	}

	a.handleRPCEvent(rpcclient.Event{Kind: rpcclient.EventToolCallCompleted,
		ToolCallID: "t1", Content: "error: file not found"})
	tb, _ = a.session.FindToolBlock("t1")
	if tb.Status != state.ToolBlockFailed {
		t.Fatalf("status=%v, want failed for error: prefix", tb.Status)
	}

	a.handleRPCEvent(rpcclient.Event{Kind: rpcclient.EventToolCallStart,
		ToolCallID: "t2", ToolCallName: "grep"})
	a.handleRPCEvent(rpcclient.Event{Kind: rpcclient.EventToolCallCompleted,
		ToolCallID: "t2", Content: "skipped: denied"})
	tb, _ = a.session.FindToolBlock("t2")
	if tb.Status != state.ToolBlockSkipped {
		t.Fatalf("status=%v, want skipped", tb.Status)
	}

	a.handleRPCEvent(rpcclient.Event{Kind: rpcclient.EventToolCallStart,
		ToolCallID: "t3", ToolCallName: "ls"})
	a.handleRPCEvent(rpcclient.Event{Kind: rpcclient.EventToolCallCompleted,
		ToolCallID: "t3", Content: "ok"})
	tb, _ = a.session.FindToolBlock("t3")
	if tb.Status != state.ToolBlockCompleted {
		t.Fatalf("status=%v, want completed", tb.Status)
	}
}

func TestExecOutputChunk_StreamsIntoRunningBash(t *testing.T) {
	a, _ := startedTurnApp(t)

	a.handleRPCEvent(rpcclient.Event{Kind: rpcclient.EventToolCallStart,
		ToolCallID: "b1", ToolCallName: "bash"})
	a.handleRPCEvent(rpcclient.Event{Kind: rpcclient.EventExecOutputChunk, Content: "line1\n"})
	a.handleRPCEvent(rpcclient.Event{Kind: rpcclient.EventExecOutputChunk, Content: "line2\n"})

	tb, ok := a.session.FindToolBlock("b1")
	if !ok || !strings.Contains(tb.Result, "line1") || !strings.Contains(tb.Result, "line2") {
		t.Fatalf("exec output not streamed: %q", tb.Result)
	}
}

func TestPendingOps_DryRunArmsReview(t *testing.T) {
	a, _ := startedTurnApp(t)

	a.handleRPCEvent(rpcclient.Event{
		Kind: rpcclient.EventPendingOps,
		PendingOps: &rpcclient.PendingOpsPayload{
			Applied: false,
			Ops:     []map[string]any{{"op": "file.write_atomic", "path": "a.go"}},
			Diff:    []rpcclient.FileDiff{{Path: "a.go", Before: "old", After: "new"}},
		},
	})

	if !a.review.HasPendingOps() {
		t.Fatal("dry-run ops must arm the review")
	}
	if !a.review.Shown() || !a.session.LastDiffExpanded() {
		t.Fatal("diff block must be inserted and expanded")
	}
	if a.session.DiffFileCount() != 1 {
		t.Fatalf("diff files=%d, want 1", a.session.DiffFileCount())
	}
}

func TestPendingOps_AppliedShowsNoticeWithoutReview(t *testing.T) {
	a, _ := startedTurnApp(t)

	a.handleRPCEvent(rpcclient.Event{
		Kind: rpcclient.EventPendingOps,
		PendingOps: &rpcclient.PendingOpsPayload{
			Applied: true,
			Ops:     []map[string]any{{"op": "file.write_atomic", "path": "a.go"}},
		},
	})

	if a.review.PendingReview() {
		t.Fatal("applied ops must not enter review mode")
	}
	if !hasSystemNotice(a, "записано на диск") {
		t.Fatal("applied notice must be shown")
	}
}

func TestDiffReviewApply_FullFlowThroughCore(t *testing.T) {
	a, f := startedTurnApp(t)
	// End the turn so review hotkeys are active.
	a.handleRPCEvent(rpcclient.Event{
		Kind: rpcclient.EventPendingOps,
		PendingOps: &rpcclient.PendingOpsPayload{
			Ops: []map[string]any{
				{"op": "file.write_atomic", "path": "a.go"},
				{"op": "file.write_atomic", "path": "b.go"},
			},
			Diff: []rpcclient.FileDiff{
				{Path: "a.go", Before: "1", After: "2"},
				{Path: "b.go", Before: "3", After: "4"},
			},
		},
	})
	a.handleRPCEvent(rpcclient.Event{Kind: rpcclient.EventAgentRunCompleted})

	// Reject b.go, apply the rest.
	a.session.SetDiffFileReviewStatus(1, state.DiffReviewRejected)
	cmd := a.applyPendingDiffReviewCmd()
	if cmd == nil {
		t.Fatal("apply must produce a command")
	}
	execCmdTree(cmd)

	f.mu.Lock()
	applied := append([][]map[string]any(nil), f.appliedOps...)
	f.mu.Unlock()
	if len(applied) != 1 || len(applied[0]) != 1 {
		t.Fatalf("applied batches=%v", applied)
	}
	if applied[0][0]["path"] != "a.go" {
		t.Fatalf("rejected file leaked into apply: %v", applied[0][0])
	}

	// Result message clears review mode.
	a.handleDiffApplyResult(diffApplyResultMsg{applied: 1})
	if a.review.PendingReview() || len(a.review.PendingOps()) != 0 {
		t.Fatal("review must be cleared after successful apply")
	}
}

func TestQuestionFlow_AnswerViaEnter(t *testing.T) {
	a, f := testCoreApp(t)

	a.handleRPCEvent(rpcclient.Event{
		Kind:  rpcclient.EventQuestionAsked,
		ReqID: 9,
		Questions: []rpcclient.QuestionItemPayload{
			{Question: "Какой фреймворк?", Options: []string{"gin", "echo"}},
		},
	})
	if a.questionModal == nil || a.questionReqID != 9 {
		t.Fatalf("modal=%v reqID=%d", a.questionModal, a.questionReqID)
	}

	a.input.SetValue("gin")
	a.handleEnter()

	if a.questionModal != nil || a.questionReqID != 0 {
		t.Fatal("modal state must be cleared after answering")
	}
	f.mu.Lock()
	answers := append([]fakeQuestionAnswer(nil), f.questionAnswers...)
	f.mu.Unlock()
	if len(answers) != 1 || answers[0].ReqID != 9 ||
		len(answers[0].Answers) != 1 || answers[0].Answers[0] != "gin" {
		t.Fatalf("answers=%+v", answers)
	}
}

func TestRespawn_ClearsStaleQuestionAndPermState(t *testing.T) {
	a, _ := testCoreApp(t)
	a.cfg.Binary = "orchestra" // enable the respawn path (not executed in test)

	a.handleRPCEvent(rpcclient.Event{
		Kind:      rpcclient.EventQuestionAsked,
		ReqID:     5,
		Questions: []rpcclient.QuestionItemPayload{{Question: "q?"}},
	})
	a.handleRPCEvent(rpcclient.Event{
		Kind:    rpcclient.EventPermissionRequest,
		PermReq: &rpcclient.PermissionRequestPayload{Tool: "bash", ReqID: 6},
	})
	if a.questionModal == nil || a.permModal == nil {
		t.Fatal("both modals must be armed before respawn")
	}

	// Note: do not execute the returned command — it would spawn a process.
	if cmd := a.respawnRPCCmd(); cmd == nil {
		t.Fatal("respawn must produce a command")
	}

	if a.questionModal != nil || a.questionReqID != 0 {
		t.Fatal("stale question modal must be dropped on respawn")
	}
	if a.permModal != nil {
		t.Fatal("stale permission modal must be dropped on respawn")
	}
	if _, ok := a.perms.Current(); ok || a.perms.Waiting() != 0 {
		t.Fatal("permission queue must be reset on respawn")
	}
}

func TestTurnUsage_RecordedOnAssistantMessage(t *testing.T) {
	a, _ := startedTurnApp(t)

	a.handleRPCEvent(rpcclient.Event{
		Kind:  rpcclient.EventTurnUsage,
		Usage: &rpcclient.UsageTurnPayload{PromptTokens: 1200, CompletionTokens: 340},
	})
	a.handleRPCEvent(rpcclient.Event{Kind: rpcclient.EventAgentRunCompleted})

	m := lastAssistant(t, a)
	if m.TokensIn != 1200 || m.TokensOut != 340 {
		t.Fatalf("tokens in/out = %d/%d", m.TokensIn, m.TokensOut)
	}
}

func TestTodosUpdated_SetsChecklist(t *testing.T) {
	a, _ := startedTurnApp(t)

	a.handleRPCEvent(rpcclient.Event{
		Kind: rpcclient.EventTodosUpdated,
		Todos: []rpcclient.TodoItem{
			{ID: "1", Content: "первое", Status: "in_progress"},
			{ID: "2", Content: "второе", Status: "pending"},
		},
	})

	if len(a.todos) != 2 {
		t.Fatalf("todos=%d, want 2", len(a.todos))
	}
	m := lastAssistant(t, a)
	found := false
	for _, seg := range m.Segments {
		if seg.Kind == state.SegmentTodos && len(seg.Todos) == 2 {
			found = true
		}
	}
	if !found {
		t.Fatal("checklist segment must be upserted into the assistant turn")
	}
}

func TestNoticeTurnStop_MaxStepsHintsContinue(t *testing.T) {
	a, _ := startedTurnApp(t)

	a.handleRPCEvent(rpcclient.Event{
		Kind:       rpcclient.EventTurnTodos,
		StopReason: "max_steps",
		OpenTodos:  2,
	})

	if !hasSystemNotice(a, "Лимит шагов") {
		t.Fatal("max_steps stop must explain itself in chat")
	}
}

func TestNoticeTurnStop_CompletedIsSilent(t *testing.T) {
	a, _ := startedTurnApp(t)
	before := len(a.session.Messages)

	a.handleRPCEvent(rpcclient.Event{
		Kind:       rpcclient.EventTurnTodos,
		StopReason: "completed",
	})

	if len(a.session.Messages) != before {
		t.Fatal("completed turn must not add footer noise")
	}
	if hasSystemNotice(a, "продолжай") {
		t.Fatal("no continue hint expected on completed")
	}
}

func TestTurnMemory_FailureIsLoud(t *testing.T) {
	a, _ := startedTurnApp(t)

	a.handleRPCEvent(rpcclient.Event{
		Kind:   rpcclient.EventTurnMemory,
		Memory: &rpcclient.MemoryNotePayload{Outcome: "failed", Source: "digest", Detail: "write agent.md: permission denied"},
	})

	// The field run's memory failures went to stderr, where nobody saw them
	// for nine days. A failed write has to appear in the chat itself.
	if !hasSystemNotice(a, "Память") || !hasSystemNotice(a, "permission denied") {
		t.Fatal("a failed memory write must be reported in chat with its reason")
	}
}

func TestTurnMemory_WrittenIsBriefAndNamesSource(t *testing.T) {
	a, _ := startedTurnApp(t)

	a.handleRPCEvent(rpcclient.Event{
		Kind:   rpcclient.EventTurnMemory,
		Memory: &rpcclient.MemoryNotePayload{Outcome: "written", Source: "digest", Detail: "[session:s1] goal: fix; done: edit README.md"},
	})

	if !hasSystemNotice(a, "Память") {
		t.Fatal("a written note must be acknowledged so the operator can see memory working")
	}
	if !hasSystemNotice(a, "дайджест") {
		t.Fatal("the notice must say the note came from the digest, not the model")
	}
}

func TestTurnMemory_SkippedIsSilent(t *testing.T) {
	a, _ := startedTurnApp(t)
	before := len(a.session.Messages)

	a.handleRPCEvent(rpcclient.Event{
		Kind:   rpcclient.EventTurnMemory,
		Memory: &rpcclient.MemoryNotePayload{Outcome: "skipped", Detail: "turn changed no files"},
	})

	// Most turns read and answer; telling the user "nothing to remember" every
	// time would be the grep-noise problem again, in the chat this time.
	if len(a.session.Messages) != before || hasSystemNotice(a, "Память") {
		t.Fatal("a skipped note must not add chat noise")
	}
}

func TestChildEvents_DoNotPolluteLeadChat(t *testing.T) {
	a, _ := startedTurnApp(t)

	a.handleRPCEvent(rpcclient.Event{
		Kind:         rpcclient.EventChildStarted,
		TaskID:       "task_1",
		SubagentType: "worker",
		Content:      `{"intent":"edit jwt","target_files":["internal/auth/jwt.go"]}`,
	})
	a.handleRPCEvent(rpcclient.Event{
		Kind:         rpcclient.EventToolCallStart,
		Scope:        "child",
		TaskID:       "task_1",
		ToolCallID:   "c1",
		ToolCallName: "read",
		Content:      "reading jwt.go",
	})
	a.handleRPCEvent(rpcclient.Event{
		Kind:    rpcclient.EventMessageDelta,
		Scope:   "child",
		TaskID:  "task_1",
		Content: "worker token stream must not appear in Lead chat",
	})

	m := lastAssistant(t, a)
	if len(m.ToolBlocks) != 0 {
		t.Fatalf("child tool_call must not create Lead tool blocks, got %d", len(m.ToolBlocks))
	}
	if strings.Contains(m.Text, "worker token stream") {
		t.Fatal("child message_delta must not land in Lead text")
	}
	snaps := a.subagents.Snapshot(m.StartedAt)
	if len(snaps) != 1 || snaps[0].Status != "running" {
		t.Fatalf("expected running subagent, got %+v", snaps)
	}
	if snaps[0].Goal != "internal/auth/jwt.go" {
		t.Fatalf("goal=%q", snaps[0].Goal)
	}
}

func TestChildDone_CollapsesToBadge(t *testing.T) {
	a, _ := startedTurnApp(t)
	a.handleRPCEvent(rpcclient.Event{
		Kind:         rpcclient.EventChildStarted,
		TaskID:       "task_1",
		SubagentType: "worker",
		Content:      "internal/auth/jwt.go",
	})
	a.handleRPCEvent(rpcclient.Event{
		Kind:         rpcclient.EventChildDone,
		TaskID:       "task_1",
		SubagentType: "worker",
		ChildStatus:  "done",
		Content:      "Modified ValidateToken (verified by go test)",
	})

	snaps := a.subagents.Snapshot(lastAssistant(t, a).StartedAt)
	if len(snaps) != 1 || snaps[0].Status != "done" {
		t.Fatalf("expected done badge, got %+v", snaps)
	}
	if !strings.Contains(snaps[0].ResultSummary, "ValidateToken") {
		t.Fatalf("summary=%q", snaps[0].ResultSummary)
	}
	view := a.chat.View()
	if !strings.Contains(view, "Done") && !strings.Contains(view, "ValidateToken") {
		t.Fatalf("subagent bar missing done badge, view=%q", view)
	}
}

func TestChildQueued_ShowsWaitingLock(t *testing.T) {
	a, _ := startedTurnApp(t)
	a.handleRPCEvent(rpcclient.Event{
		Kind:          rpcclient.EventChildQueued,
		TaskID:        "task_2",
		SubagentType:  "worker",
		Content:       `{"target_files":["internal/server/router.go"]}`,
		WaitingReason: "overlapping target_files; serialized per spec §5.6",
	})
	snaps := a.subagents.Snapshot(lastAssistant(t, a).StartedAt)
	if len(snaps) != 1 || snaps[0].Status != "queued" {
		t.Fatalf("expected queued, got %+v", snaps)
	}
	view := a.chat.View()
	if !strings.Contains(view, "queued") {
		t.Fatalf("queued worker missing from bar: %q", view)
	}
}
