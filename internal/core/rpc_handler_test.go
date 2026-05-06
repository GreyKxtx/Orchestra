package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/llm"
	"github.com/orchestra/orchestra/internal/protocol"
	"github.com/orchestra/orchestra/internal/cache"
)

// fixedLLM always returns the same scripted responses in order.
type fixedLLM struct {
	steps []string
	i     int
}

func (f *fixedLLM) Complete(_ context.Context, _ llm.CompleteRequest) (*llm.CompleteResponse, error) {
	if f.i >= len(f.steps) {
		return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: `{"type":"final","final":{"patches":[]}}`}}, nil
	}
	out := f.steps[f.i]
	f.i++
	return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: out}}, nil
}

func (f *fixedLLM) Plan(_ context.Context, _ string) (string, error) { return "{}", nil }

func TestRPCHandler_RequiresInitialize(t *testing.T) {
	root := t.TempDir()

	cfg := config.DefaultConfig(root)
	if err := config.Save(filepath.Join(root, ".orchestra.yml"), cfg); err != nil {
		t.Fatalf("Save config failed: %v", err)
	}

	c, err := New(root, Options{})
	if err != nil {
		t.Fatalf("New core failed: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	h := NewRPCHandler(c)

	// agent.run should require initialize.
	params, _ := json.Marshal(AgentRunParams{Query: "x"})
	_, err = h.Handle(context.Background(), "agent.run", params)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	pe, ok := protocol.AsError(err)
	if !ok || pe.Code != protocol.NotInitialized {
		t.Fatalf("expected NotInitialized, got %T: %v", err, err)
	}
}

func TestRPCHandler_Initialize_ThenToolCall(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "README.md"), []byte("hello\n"), 0644)

	cfg := config.DefaultConfig(root)
	if err := config.Save(filepath.Join(root, ".orchestra.yml"), cfg); err != nil {
		t.Fatalf("Save config failed: %v", err)
	}

	c, err := New(root, Options{})
	if err != nil {
		t.Fatalf("New core failed: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	h := NewRPCHandler(c)

	projectID, err := cache.ComputeProjectID(root)
	if err != nil {
		t.Fatalf("ComputeProjectID failed: %v", err)
	}

	initParams, _ := json.Marshal(InitializeParams{
		ProjectRoot:     root,
		ProjectID:       projectID,
		ProtocolVersion: protocol.ProtocolVersion,
		OpsVersion:      protocol.OpsVersion,
		ToolsVersion:    protocol.ToolsVersion,
	})
	_, err = h.Handle(context.Background(), "initialize", initParams)
	if err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	// initialize should be idempotent for the same parameters.
	_, err = h.Handle(context.Background(), "initialize", initParams)
	if err != nil {
		t.Fatalf("second initialize failed: %v", err)
	}

	// initialize with different values must fail and not change already initialized state.
	badInitParams, _ := json.Marshal(InitializeParams{
		ProjectRoot:     root,
		ProjectID:       "sha256:deadbeef",
		ProtocolVersion: protocol.ProtocolVersion,
	})
	_, err = h.Handle(context.Background(), "initialize", badInitParams)
	if err == nil {
		t.Fatalf("expected error for mismatched initialize, got nil")
	}

	// tool.call should now work.
	callParams, _ := json.Marshal(ToolCallParams{
		Name:  "fs.list",
		Input: json.RawMessage(`{"limit":10}`),
	})
	out, err := h.Handle(context.Background(), "tool.call", callParams)
	if err != nil {
		t.Fatalf("tool.call failed: %v", err)
	}
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("unexpected tool.call result type: %T", out)
	}
	if _, ok := m["files"]; !ok {
		t.Fatalf("expected files in result, got: %+v", m)
	}

	// exec.run via tool.call should be denied by default (requires consent).
	execParams, _ := json.Marshal(ToolCallParams{
		Name:  "exec.run",
		Input: json.RawMessage(`{"command":"echo","args":["hi"],"workdir":"."}`),
	})
	_, err = h.Handle(context.Background(), "tool.call", execParams)
	if err == nil {
		t.Fatalf("expected error for exec.run, got nil")
	}
	pe, ok := protocol.AsError(err)
	if !ok || pe.Code != protocol.ExecDenied {
		t.Fatalf("expected ExecDenied, got %T: %v", err, err)
	}

	// Still initialized after failed re-initialize attempt.
	_, err = h.Handle(context.Background(), "tool.call", callParams)
	if err != nil {
		t.Fatalf("tool.call after failed initialize should still work: %v", err)
	}
}

func setupInitializedCore(t *testing.T, root string, llmOverride llm.Client) (*Core, *RPCHandler) {
	t.Helper()
	cfg := config.DefaultConfig(root)
	if err := config.Save(filepath.Join(root, ".orchestra.yml"), cfg); err != nil {
		t.Fatalf("Save config failed: %v", err)
	}
	c, err := New(root, Options{LLMClient: llmOverride})
	if err != nil {
		t.Fatalf("New core failed: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	h := NewRPCHandler(c)

	projectID, err := cache.ComputeProjectID(root)
	if err != nil {
		t.Fatalf("ComputeProjectID: %v", err)
	}
	initP, _ := json.Marshal(InitializeParams{
		ProjectRoot:     root,
		ProjectID:       projectID,
		ProtocolVersion: protocol.ProtocolVersion,
		OpsVersion:      protocol.OpsVersion,
		ToolsVersion:    protocol.ToolsVersion,
	})
	if _, err := h.Handle(context.Background(), "initialize", initP); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}
	return c, h
}

func TestSession_StartHistoryClose(t *testing.T) {
	root := t.TempDir()
	_, h := setupInitializedCore(t, root, &fixedLLM{})

	// session.start
	startP, _ := json.Marshal(SessionStartParams{})
	res, err := h.Handle(context.Background(), "session.start", startP)
	if err != nil {
		t.Fatalf("session.start: %v", err)
	}
	sr, ok := res.(*SessionStartResult)
	if !ok {
		t.Fatalf("expected *SessionStartResult, got %T", res)
	}
	sessionID := sr.SessionID
	if sessionID == "" {
		t.Fatal("expected non-empty session_id")
	}

	// session.history — initially empty
	histP, _ := json.Marshal(SessionHistoryParams{SessionID: sessionID})
	histRes, err := h.Handle(context.Background(), "session.history", histP)
	if err != nil {
		t.Fatalf("session.history: %v", err)
	}
	hr := histRes.(*SessionHistoryResult)
	if len(hr.Messages) != 0 {
		t.Fatalf("expected empty history, got %d messages", len(hr.Messages))
	}

	// session.close
	closeP, _ := json.Marshal(SessionCloseParams{SessionID: sessionID})
	if _, err := h.Handle(context.Background(), "session.close", closeP); err != nil {
		t.Fatalf("session.close: %v", err)
	}

	// session.history after close → not found
	_, err = h.Handle(context.Background(), "session.history", histP)
	if err == nil {
		t.Fatal("expected error after close, got nil")
	}
}

func TestSession_MessageUpdatesHistory(t *testing.T) {
	root := t.TempDir()
	// Write a target file so fs.read in the query makes sense.
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello\n"), 0644); err != nil {
		t.Fatalf("write a.txt: %v", err)
	}

	fileHash := cache.ComputeSHA256([]byte("hello\n"))
	patch := `{"type":"file.search_replace","path":"a.txt","search":"hello","replace":"hello","file_hash":"` + fileHash + `"}`
	finalResp := `{"type":"final","final":{"patches":[` + patch + `]}}`

	_, h := setupInitializedCore(t, root, &fixedLLM{
		steps: []string{finalResp},
	})

	// Start a session.
	startP, _ := json.Marshal(SessionStartParams{})
	res, err := h.Handle(context.Background(), "session.start", startP)
	if err != nil {
		t.Fatalf("session.start: %v", err)
	}
	sessionID := res.(*SessionStartResult).SessionID

	// Send a message — agent does a no-op search_replace (search == replace).
	msgP, _ := json.Marshal(SessionMessageParams{
		SessionID: sessionID,
		Content:   "check a.txt",
		Apply:     false, // dry-run
	})
	msgRes, err := h.Handle(context.Background(), "session.message", msgP)
	if err != nil {
		t.Fatalf("session.message: %v", err)
	}
	mr := msgRes.(*SessionMessageResult)
	if mr.Applied {
		t.Fatal("expected Applied=false for dry-run")
	}

	// History should now contain the final assistant message.
	histP, _ := json.Marshal(SessionHistoryParams{SessionID: sessionID})
	histRes, err := h.Handle(context.Background(), "session.history", histP)
	if err != nil {
		t.Fatalf("session.history: %v", err)
	}
	hr := histRes.(*SessionHistoryResult)
	if len(hr.Messages) == 0 {
		t.Fatal("expected non-empty history after session.message")
	}
}

func TestSession_Cancel_IdleIsNoOp(t *testing.T) {
	root := t.TempDir()
	_, h := setupInitializedCore(t, root, &fixedLLM{})

	startP, _ := json.Marshal(SessionStartParams{})
	res, err := h.Handle(context.Background(), "session.start", startP)
	if err != nil {
		t.Fatalf("session.start: %v", err)
	}
	sessionID := res.(*SessionStartResult).SessionID

	cancelP, _ := json.Marshal(SessionCancelParams{SessionID: sessionID})
	if _, err := h.Handle(context.Background(), "session.cancel", cancelP); err != nil {
		t.Fatalf("session.cancel on idle session: %v", err)
	}
}

// slowLLM blocks Complete until the context is cancelled.
// It closes ready the first time Complete is entered so callers can synchronize.
type slowLLM struct {
	ready chan struct{}
	once  sync.Once
}

func (s *slowLLM) Complete(ctx context.Context, _ llm.CompleteRequest) (*llm.CompleteResponse, error) {
	s.once.Do(func() { close(s.ready) })
	<-ctx.Done()
	return nil, ctx.Err()
}

func (s *slowLLM) Plan(_ context.Context, _ string) (string, error) { return "{}", nil }

func TestSession_Cancel_InterruptsRunningTurn(t *testing.T) {
	root := t.TempDir()

	slow := &slowLLM{ready: make(chan struct{})}
	_, h := setupInitializedCore(t, root, slow)

	startP, _ := json.Marshal(SessionStartParams{})
	res, err := h.Handle(context.Background(), "session.start", startP)
	if err != nil {
		t.Fatalf("session.start: %v", err)
	}
	sessionID := res.(*SessionStartResult).SessionID

	// Run session.message in the background; it will block inside the LLM.
	msgErrCh := make(chan error, 1)
	go func() {
		msgP, _ := json.Marshal(SessionMessageParams{
			SessionID: sessionID,
			Content:   "slow task",
		})
		_, err := h.Handle(context.Background(), "session.message", msgP)
		msgErrCh <- err
	}()

	// Wait until the LLM has been entered.
	<-slow.ready

	// Cancel the running turn.
	cancelP, _ := json.Marshal(SessionCancelParams{SessionID: sessionID})
	if _, err := h.Handle(context.Background(), "session.cancel", cancelP); err != nil {
		t.Fatalf("session.cancel: %v", err)
	}

	// session.message must return an error (context cancelled).
	if err := <-msgErrCh; err == nil {
		t.Fatal("expected session.message to fail after cancel, got nil")
	}

	// Session must still be accessible after cancellation.
	histP, _ := json.Marshal(SessionHistoryParams{SessionID: sessionID})
	if _, err := h.Handle(context.Background(), "session.history", histP); err != nil {
		t.Fatalf("session.history after cancel should succeed: %v", err)
	}
}

func TestSession_ApplyPending_NoOpsReturnsNotApplied(t *testing.T) {
	root := t.TempDir()
	_, h := setupInitializedCore(t, root, &fixedLLM{})

	startP, _ := json.Marshal(SessionStartParams{})
	res, err := h.Handle(context.Background(), "session.start", startP)
	if err != nil {
		t.Fatalf("session.start: %v", err)
	}
	sessionID := res.(*SessionStartResult).SessionID

	// No session.message was sent, so pending ops are empty.
	applyP, _ := json.Marshal(SessionApplyPendingParams{SessionID: sessionID})
	applyRes, err := h.Handle(context.Background(), "session.apply_pending", applyP)
	if err != nil {
		t.Fatalf("session.apply_pending: %v", err)
	}
	ar, ok := applyRes.(*SessionApplyPendingResult)
	if !ok {
		t.Fatalf("expected *SessionApplyPendingResult, got %T", applyRes)
	}
	if ar.Applied {
		t.Fatal("expected Applied=false when no pending ops")
	}
}
