package tui

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/orchestra/orchestra/internal/sessionfile"
	"github.com/orchestra/orchestra/ui/tui/rpcclient"
)

// fakeCore is a scripted in-memory coreClient. It records every call so App
// flows (turns, permissions, diff apply, workflows) can be asserted without
// spawning the core subprocess.
type fakeCore struct {
	mu sync.Mutex

	eventsCh chan rpcclient.Event
	closed   bool

	// Recorded calls.
	sessionMessages []fakeTurnCall
	agentRuns       []fakeTurnCall
	uiSyncs         []string // session ids
	appliedOps      [][]map[string]any
	toolCalls       []string // tool names
	permAnswers     []fakePermAnswer
	questionAnswers []fakeQuestionAnswer

	// Scripted responses.
	sessionGetResult *rpcclient.SessionGetResult
	sessionStartID   string
}

type fakeTurnCall struct {
	SessionID string
	Query     string
	Mode      string
	Opts      rpcclient.AgentRunOptions
}

type fakePermAnswer struct {
	ReqID    int64
	Decision rpcclient.PermissionDecision
}

type fakeQuestionAnswer struct {
	ReqID   int64
	Answers []string
}

var _ coreClient = (*fakeCore)(nil)

func newFakeCore() *fakeCore {
	return &fakeCore{eventsCh: make(chan rpcclient.Event, 64)}
}

func (f *fakeCore) Events() <-chan rpcclient.Event { return f.eventsCh }

func (f *fakeCore) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.closed {
		f.closed = true
		close(f.eventsCh)
	}
	return nil
}

func (f *fakeCore) SessionStart(_ context.Context, sessionID string) (string, bool, error) {
	if sessionID != "" {
		return sessionID, true, nil
	}
	if f.sessionStartID != "" {
		return f.sessionStartID, false, nil
	}
	return "fake-session", false, nil
}

func (f *fakeCore) SessionGet(_ context.Context, _ string) (*rpcclient.SessionGetResult, error) {
	if f.sessionGetResult != nil {
		return f.sessionGetResult, nil
	}
	return &rpcclient.SessionGetResult{}, nil
}

func (f *fakeCore) SessionUISync(_ context.Context, sessionID, _, _ string, _ []sessionfile.UIMessage, _ float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uiSyncs = append(f.uiSyncs, sessionID)
	return nil
}

func (f *fakeCore) SessionRewind(_ context.Context, _ string, _ int) (*rpcclient.SessionRewindResult, error) {
	return &rpcclient.SessionRewindResult{}, nil
}

func (f *fakeCore) SessionCompact(_ context.Context, _, _ string) (*rpcclient.SessionCompactResult, error) {
	return &rpcclient.SessionCompactResult{}, nil
}

func (f *fakeCore) SessionMessage(_ context.Context, sessionID, query, mode string, opts rpcclient.AgentRunOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessionMessages = append(f.sessionMessages, fakeTurnCall{SessionID: sessionID, Query: query, Mode: mode, Opts: opts})
	return nil
}

func (f *fakeCore) AgentRun(_ context.Context, query, mode string, opts rpcclient.AgentRunOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.agentRuns = append(f.agentRuns, fakeTurnCall{Query: query, Mode: mode, Opts: opts})
	return nil
}

func (f *fakeCore) ApplyOps(_ context.Context, rawOps []map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.appliedOps = append(f.appliedOps, rawOps)
	return nil
}

func (f *fakeCore) ToolCall(_ context.Context, tool string, _ json.RawMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.toolCalls = append(f.toolCalls, tool)
	return nil
}

func (f *fakeCore) WorkflowList(_ context.Context) ([]rpcclient.WorkflowSummary, error) {
	return nil, nil
}

func (f *fakeCore) WorkflowRun(_ context.Context, _, _ string, _ rpcclient.WorkflowRunOptions) (*rpcclient.WorkflowRunResult, error) {
	return &rpcclient.WorkflowRunResult{}, nil
}

func (f *fakeCore) SkillList(_ context.Context) ([]rpcclient.SkillSummary, error) { return nil, nil }

func (f *fakeCore) SkillInvoke(_ context.Context, _, _ string, _ rpcclient.SkillInvokeOptions) (*rpcclient.SkillInvokeResult, error) {
	return &rpcclient.SkillInvokeResult{}, nil
}

func (f *fakeCore) QueryLSPStatusDetail(_ context.Context) (string, int, string, error) {
	return "idle", 0, "", nil
}

func (f *fakeCore) RespondPermission(reqID int64, approved bool) {
	f.RespondPermissionDecision(reqID, rpcclient.PermissionDecision{Approved: approved})
}

func (f *fakeCore) RespondPermissionDecision(reqID int64, d rpcclient.PermissionDecision) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.permAnswers = append(f.permAnswers, fakePermAnswer{ReqID: reqID, Decision: d})
}

func (f *fakeCore) RespondQuestion(reqID int64, answers []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.questionAnswers = append(f.questionAnswers, fakeQuestionAnswer{ReqID: reqID, Answers: answers})
}

// snapshot helpers (lock once, copy out).

func (f *fakeCore) recordedSessionMessages() []fakeTurnCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeTurnCall(nil), f.sessionMessages...)
}

func (f *fakeCore) recordedPermAnswers() []fakePermAnswer {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakePermAnswer(nil), f.permAnswers...)
}
