package core

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/trajectory"
	"github.com/orchestra/orchestra/llm"
)

func TestTeeToTrajectory_RecordsWhatItForwards(t *testing.T) {
	root := t.TempDir()
	w, err := trajectory.NewWriter(root, "s1")
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	defer func() { _ = w.Close() }()

	var forwarded []string
	notify := func(method string, params any) { forwarded = append(forwarded, method) }

	tee := teeToTrajectory(notify, w)
	tee("agent/event", map[string]any{"type": "tool_call_start", "step": 1})
	tee("exec/output_chunk", map[string]any{"chunk": "hello"})

	// Forwarding must be unchanged — the tee is additive.
	if len(forwarded) != 2 || forwarded[0] != "agent/event" || forwarded[1] != "exec/output_chunk" {
		t.Fatalf("forwarded = %v, want [agent/event exec/output_chunk]", forwarded)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	events, recorded, err := trajectory.Read(root, "s1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !recorded || len(events) != 2 {
		t.Fatalf("recorded=%v len=%d, want true and 2", recorded, len(events))
	}
	if events[0].Type != "agent/event" || events[1].Type != "exec/output_chunk" {
		t.Errorf("types = %q,%q", events[0].Type, events[1].Type)
	}
	var first map[string]any
	if err := json.Unmarshal(events[0].Data, &first); err != nil {
		t.Fatalf("payload not stored as an object: %v", err)
	}
	if first["type"] != "tool_call_start" {
		t.Errorf("payload type = %v, want tool_call_start", first["type"])
	}
}

func TestTeeToTrajectory_NilWriterForwardsAndDoesNotPanic(t *testing.T) {
	// A session with no writer (a one-shot agent.run has no session id) must
	// keep notifying. Recording is best-effort; delivery is not.
	var forwarded int
	tee := teeToTrajectory(func(string, any) { forwarded++ }, nil)
	tee("agent/event", map[string]any{"type": "done"})
	if forwarded != 1 {
		t.Errorf("forwarded = %d, want 1", forwarded)
	}
}

// TestSessionTurn_LeavesATrajectoryOnDisk drives one session.message turn
// against a scripted LLM (no notifier attached, exactly like
// setupInitializedCore's other callers) and asserts the sidecar log left on
// disk reflects the turn.
//
// The harness attaches no notifier, so p.OnEvent is nil going into
// prepareAgentLaunch — this is precisely the configuration Step 5's tee must
// still record in. If this test finds no log, the bug is a nil guard, not a
// fault in the harness.
func TestSessionTurn_LeavesATrajectoryOnDisk(t *testing.T) {
	root := t.TempDir()

	// A final response with no patches: the agent settles on step 1 without
	// calling any tool, emitting a step_done("final") notification along the
	// way — the terminal marker for this turn.
	finalStep := `{"type":"final","final":{"patches":[]}}`
	_, h := setupInitializedCore(t, root, &fixedLLM{steps: []string{finalStep}})

	startP, _ := json.Marshal(SessionStartParams{})
	res, err := h.Handle(context.Background(), "session.start", startP)
	if err != nil {
		t.Fatalf("session.start: %v", err)
	}
	sessionID := res.(*SessionStartResult).SessionID

	msgP, _ := json.Marshal(SessionMessageParams{
		SessionID: sessionID,
		Content:   "say hello",
	})
	if _, err := h.Handle(context.Background(), "session.message", msgP); err != nil {
		t.Fatalf("session.message: %v", err)
	}

	events, recorded, err := trajectory.Read(root, sessionID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !recorded {
		t.Fatal("expected recorded=true; the session should have left a sidecar log")
	}
	if len(events) == 0 {
		t.Fatal("expected at least one recorded event")
	}

	// Seq must be contiguous from 1 — a gap would mean an event failed to
	// write, which trajectory.Writer treats as a visible defect, not silent
	// data loss.
	for i, ev := range events {
		if want := int64(i + 1); ev.Seq != want {
			t.Fatalf("events[%d].Seq = %d, want %d (contiguous from 1)", i, ev.Seq, want)
		}
	}

	// The turn's terminal marker: an agent/event whose payload says the step
	// finished as "final". (Note: with this fixedLLM script — a single-step
	// final response with no tool calls and no provider Usage — the agent
	// never emits a StreamEventDone/"done"-typed event; that only happens on
	// the real streaming path. step_done/"final" is the equivalent marker
	// here: it is what signals this turn's terminal step.)
	foundStepDone := false
	for _, ev := range events {
		if ev.Type != "agent/event" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal(ev.Data, &payload); err != nil {
			continue
		}
		if payload["type"] == "step_done" && payload["content"] == "final" {
			foundStepDone = true
			break
		}
	}
	if !foundStepDone {
		t.Errorf("expected an agent/event with payload type=step_done content=final, got events: %+v", events)
	}
}

// TestPrepareAgentLaunch_DoesNotLeakTheWriterWhenItFailsEarly pins the
// error-path close that this fix round added.
//
// prepareAgentLaunch opens the trajectory writer, then hands ownership to the
// *agentLaunch it returns; its callers defer Close. An error return in
// between abandons the open handle, and on Windows an open handle makes the
// sidecar undeletable, so sessionfile.Delete would half-delete the session.
//
// No configuration reaches such a return today — see the deviation note below,
// which is why this test has to construct the situation by hand. The close is
// defense for the error paths this function grows later, so the test asserts
// the mechanism rather than a live bug.
//
// Assert by consequence, not by inspecting internals: after the failed call,
// os.Remove on the sidecar must succeed. That is exactly the operation
// sessionfile.Delete performs, and on Windows it is the one that fails while
// a handle is open.
//
// Deviation from the brief: the brief said to "configure the failure through
// the config file the harness writes rather than by reaching into private
// state". That does not work for this specific branch — config.Load (used by
// New, used by setupInitializedCore) calls ProjectConfig.Validate, which
// calls validateAgents, which rejects an agents: entry whose provider is not
// also present in providers: at load time, with the exact same check
// resolveCustomAgentOpts relies on. The runtime AgentsUpsert RPC path
// (internal/core/runtime_agents.go) enforces the identical rule via
// ValidateAgentsOnly. I verified this empirically: a config file built this
// way makes Core construction itself fail, before prepareAgentLaunch is ever
// reached (New returns "invalid config: agent ... provider ... not defined in
// providers"). There is no exposed provider-delete API either, so an agent
// can never legitimately end up referencing a provider absent from the
// config it was loaded with.
//
// So this test builds the core the same way the harness does (no injected
// LLM client, so c.llmClientInjected stays false and the provider-lookup
// branch in resolveCustomAgentOpts is live — see core_agent.go:415), then
// appends directly to c.cfg.Agents in memory, bypassing the validation that
// every real entry point enforces. This matches the existing precedent of
// same-package tests building *Core by hand (e.g. hooks_lifecycle_test.go
// builds &Core{cfg: &config.ProjectConfig{}} directly). The consequence is
// that, as far as I can tell, this exact leak is not reachable today through
// any exposed configuration path — the fix is still correct defense in depth
// for whichever future error path does reach it, which is exactly why the
// brief asked for a defer keyed on the named return rather than patching the
// two current sites.
func TestPrepareAgentLaunch_DoesNotLeakTheWriterWhenItFailsEarly(t *testing.T) {
	root := t.TempDir()
	// No LLMClient override: c.llmClientInjected must be false for
	// resolveCustomAgentOpts's provider-lookup branch to run at all.
	c, _ := setupInitializedCore(t, root, nil)

	c.cfg.Agents = append(c.cfg.Agents, config.AgentDefinition{
		Name:     "leaky",
		Provider: "provider-not-in-providers-map",
	})

	const sessionID = "leak-probe"
	launch, err := c.prepareAgentLaunch(agentLaunchSpec{
		Mode:      "leaky",
		SessionID: sessionID,
	})
	if err == nil {
		t.Fatalf("expected prepareAgentLaunch to fail (unknown provider), got launch=%+v", launch)
	}

	sidecar := trajectory.Path(root, sessionID)
	if _, statErr := os.Stat(sidecar); statErr != nil {
		t.Fatalf("expected the trajectory writer to have created the sidecar, stat: %v", statErr)
	}
	if rmErr := os.Remove(sidecar); rmErr != nil {
		t.Fatalf("os.Remove(sidecar) = %v; a leaked writer handle would leave this undeletable on Windows", rmErr)
	}
}

// usageLLM is like fixedLLM but attaches provider Usage to each scripted
// response, so tests can assert exactly what the sidecar records for a step
// where the provider *did* report token accounting.
type usageLLM struct {
	steps []string
	usage []*llm.TokenUsage // parallel to steps; nil entries report no usage
	i     int
}

func (f *usageLLM) Complete(_ context.Context, _ llm.CompleteRequest) (*llm.CompleteResponse, error) {
	if f.i >= len(f.steps) {
		return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: `{"type":"final","final":{"patches":[]}}`}}, nil
	}
	out := f.steps[f.i]
	var u *llm.TokenUsage
	if f.i < len(f.usage) {
		u = f.usage[f.i]
	}
	f.i++
	return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: out}, Usage: u}, nil
}

func (f *usageLLM) Plan(_ context.Context, _ string) (string, error) { return "{}", nil }

// stepUsageEvents collects the "data" payload of every recorded agent/event
// whose type is want ("step_usage" or "context_estimate").
func stepUsageEvents(t *testing.T, events []trajectory.Event, want string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, ev := range events {
		if ev.Type != "agent/event" {
			continue
		}
		var payload struct {
			Type string         `json:"type"`
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal(ev.Data, &payload); err != nil {
			continue
		}
		if payload.Type != want {
			continue
		}
		out = append(out, payload.Data)
	}
	return out
}

// TestSessionTurn_DoesNotRecordAnEstimateAsProviderUsage drives a real turn
// against a scripted LLM that reports no usage (fixedLLM never sets
// CompleteResponse.Usage) and reads the sidecar back.
//
// This is the branch's hardest constraint: the spec refuses estimated token
// counts outright, and for a provider that reports no usage the byte-derived
// estimate emitted by Agent.emitPromptContextEstimate would otherwise be the
// *only* step_usage recorded for the step — a fabricated number with nothing
// on the log to contradict it.
func TestSessionTurn_DoesNotRecordAnEstimateAsProviderUsage(t *testing.T) {
	root := t.TempDir()

	finalStep := `{"type":"final","final":{"patches":[]}}`
	_, h := setupInitializedCore(t, root, &fixedLLM{steps: []string{finalStep}})

	startP, _ := json.Marshal(SessionStartParams{})
	res, err := h.Handle(context.Background(), "session.start", startP)
	if err != nil {
		t.Fatalf("session.start: %v", err)
	}
	sessionID := res.(*SessionStartResult).SessionID

	msgP, _ := json.Marshal(SessionMessageParams{
		SessionID: sessionID,
		Content:   "say hello",
	})
	if _, err := h.Handle(context.Background(), "session.message", msgP); err != nil {
		t.Fatalf("session.message: %v", err)
	}

	events, recorded, err := trajectory.Read(root, sessionID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !recorded {
		t.Fatal("expected recorded=true")
	}

	for _, data := range stepUsageEvents(t, events, "step_usage") {
		if data["source"] == "estimate" {
			t.Fatalf("recorded a step_usage event with source=estimate — the byte-derived estimate must never be recorded under the provider-usage event type: %+v", data)
		}
	}
	if len(stepUsageEvents(t, events, "context_estimate")) == 0 {
		t.Fatal("expected at least one context_estimate event — a fix that simply stopped emitting the estimate would also pass the step_usage assertion above without this")
	}
}

// TestSessionTurn_RecordsRealStepUsageUnmodified pins that when the provider
// *does* report usage, the recorded step_usage event carries those exact
// numbers unmodified. The whole-branch review confirmed this holds today;
// this test exists so Fix A (widening the agent_events.go translation to
// cover both StreamEventStepUsage and StreamEventContextEstimate) cannot
// regress it.
func TestSessionTurn_RecordsRealStepUsageUnmodified(t *testing.T) {
	root := t.TempDir()

	finalStep := `{"type":"final","final":{"patches":[]}}`
	llmClient := &usageLLM{
		steps: []string{finalStep},
		usage: []*llm.TokenUsage{{PromptTokens: 4096, CompletionTokens: 128, TotalTokens: 4224}},
	}
	_, h := setupInitializedCore(t, root, llmClient)

	startP, _ := json.Marshal(SessionStartParams{})
	res, err := h.Handle(context.Background(), "session.start", startP)
	if err != nil {
		t.Fatalf("session.start: %v", err)
	}
	sessionID := res.(*SessionStartResult).SessionID

	msgP, _ := json.Marshal(SessionMessageParams{
		SessionID: sessionID,
		Content:   "say hello",
	})
	if _, err := h.Handle(context.Background(), "session.message", msgP); err != nil {
		t.Fatalf("session.message: %v", err)
	}

	events, recorded, err := trajectory.Read(root, sessionID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !recorded {
		t.Fatal("expected recorded=true")
	}

	usages := stepUsageEvents(t, events, "step_usage")
	if len(usages) == 0 {
		t.Fatal("expected at least one recorded step_usage event")
	}
	data := usages[0]
	if got, want := int(data["prompt_tokens"].(float64)), 4096; got != want {
		t.Errorf("prompt_tokens = %d, want %d", got, want)
	}
	if got, want := int(data["completion_tokens"].(float64)), 128; got != want {
		t.Errorf("completion_tokens = %d, want %d", got, want)
	}
	if got, want := int(data["total_tokens"].(float64)), 4224; got != want {
		t.Errorf("total_tokens = %d, want %d", got, want)
	}
	if data["source"] != nil {
		t.Errorf("provider-reported step_usage carries a source field %v; only the estimate should", data["source"])
	}
}
