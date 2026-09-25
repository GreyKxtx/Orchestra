package core

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sort"
	"testing"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/patch/fsutil"
	"github.com/orchestra/orchestra/protocol"
	"github.com/orchestra/orchestra/protocol/wire"
)

// The handshake by version range (ProtocolVersion 24, ARCH-4). Before it,
// initialize compared three numbers for equality, so an extension one commit
// behind the core could not connect at all, and tools_version — which moves
// with tools no client calls — failed the same way.

func newHandshakeCore(t *testing.T) (h *RPCHandler, root, projectID string) {
	t.Helper()
	root = t.TempDir()
	cfg := config.DefaultConfig(root)
	if err := config.Save(filepath.Join(root, ".orchestra.yml"), cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}
	c, err := New(root, Options{})
	if err != nil {
		t.Fatalf("New core: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	projectID, err = fsutil.ComputeProjectID(root)
	if err != nil {
		t.Fatalf("ComputeProjectID: %v", err)
	}
	return NewRPCHandler(c), root, projectID
}

func handshake(t *testing.T, h *RPCHandler, p wire.InitializeParams) (*wire.InitializeResult, error) {
	t.Helper()
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	out, err := h.Handle(context.Background(), wire.MethodInitialize, raw)
	if err != nil {
		return nil, err
	}
	res, ok := out.(*InitializeResult)
	if !ok {
		t.Fatalf("initialize answered %T", out)
	}
	return res, nil
}

func TestInitialize_NegotiatesTheNewestSharedVersion(t *testing.T) {
	h, root, id := newHandshakeCore(t)
	res, err := handshake(t, h, wire.InitializeParams{
		ProjectRoot: root, ProjectID: id,
		ProtocolVersion: protocol.ProtocolVersion, MinProtocolVersion: protocol.MinProtocolVersion,
	})
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if res.Status != "ok" || res.ProtocolVersion != protocol.ProtocolVersion || res.ToolsVersion != protocol.ToolsVersion {
		t.Fatalf("result: %+v", res)
	}
	if h.core.ProtocolVersion() != protocol.ProtocolVersion {
		t.Fatalf("the core keeps the negotiated version: %d", h.core.ProtocolVersion())
	}
	if !res.Capabilities.Has(wire.MethodSessionMessage) || !res.Capabilities.Has(wire.NotifyAgentEvent) || !res.Capabilities.Has(wire.RequestPermission) {
		t.Fatalf("capabilities: %+v", res.Capabilities)
	}
	if res.Health.MinProtocolVersion != protocol.MinProtocolVersion || res.Health.ProtocolVersion != protocol.ProtocolVersion {
		t.Fatalf("health carries the range: %+v", res.Health)
	}
}

// A client of the previous release names one version, the old way, and
// gets it.
func TestInitialize_APreviousReleaseClientConnects(t *testing.T) {
	h, root, id := newHandshakeCore(t)
	previous := protocol.ProtocolVersion - 1
	res, err := handshake(t, h, wire.InitializeParams{ProjectRoot: root, ProjectID: id, ProtocolVersion: previous})
	if err != nil {
		t.Fatalf("a v%d client must connect to a v%d core: %v", previous, protocol.ProtocolVersion, err)
	}
	if res.ProtocolVersion != previous || h.core.ProtocolVersion() != previous {
		t.Fatalf("negotiated %d, want %d", res.ProtocolVersion, previous)
	}
}

// A client of the next release names a range that reaches down to this
// core's version, and gets this core's.
func TestInitialize_ANewerClientSpeaksTheCoreVersion(t *testing.T) {
	h, root, id := newHandshakeCore(t)
	res, err := handshake(t, h, wire.InitializeParams{
		ProjectRoot: root, ProjectID: id,
		ProtocolVersion: protocol.ProtocolVersion + 1, MinProtocolVersion: protocol.ProtocolVersion,
	})
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if res.ProtocolVersion != protocol.ProtocolVersion {
		t.Fatalf("negotiated %d, want %d", res.ProtocolVersion, protocol.ProtocolVersion)
	}
}

func TestInitialize_AClientOutsideTheRangeIsToldTheRange(t *testing.T) {
	h, root, id := newHandshakeCore(t)
	_, err := handshake(t, h, wire.InitializeParams{ProjectRoot: root, ProjectID: id, ProtocolVersion: protocol.MinProtocolVersion - 1})
	pe, ok := protocol.AsError(err)
	if !ok || pe.Code != protocol.ProtocolMismatch {
		t.Fatalf("want ProtocolMismatch, got %v", err)
	}
	data, _ := pe.Data.(map[string]any)
	if data["core_min"] != protocol.MinProtocolVersion || data["core_max"] != protocol.ProtocolVersion || data["core"] != protocol.ProtocolVersion {
		t.Fatalf("the error names the core's range so the client can say which side to update: %v", data)
	}
	if h.core.IsInitialized() {
		t.Fatal("a refused handshake leaves the core uninitialized")
	}
}

// tools_version moves with tools the client never calls; it no longer
// decides whether a client connects. The answer carries the core's.
func TestInitialize_ToolsVersionIsInformational(t *testing.T) {
	h, root, id := newHandshakeCore(t)
	res, err := handshake(t, h, wire.InitializeParams{
		ProjectRoot: root, ProjectID: id,
		ProtocolVersion: protocol.ProtocolVersion, ToolsVersion: protocol.ToolsVersion - 1,
	})
	if err != nil {
		t.Fatalf("a client written against tools v%d must connect: %v", protocol.ToolsVersion-1, err)
	}
	if res.ToolsVersion != protocol.ToolsVersion {
		t.Fatalf("the answer says which tools the core has: %d", res.ToolsVersion)
	}
	// Idempotent initialize compares what matters; a tools version that
	// differs from the first call's is not a different handshake.
	if _, err := handshake(t, h, wire.InitializeParams{
		ProjectRoot: root, ProjectID: id,
		ProtocolVersion: protocol.ProtocolVersion, ToolsVersion: protocol.ToolsVersion + 1,
	}); err != nil {
		t.Fatalf("re-initialize with another tools_version: %v", err)
	}
}

func TestInitialize_OpsVersionStillMustMatch(t *testing.T) {
	h, root, id := newHandshakeCore(t)
	_, err := handshake(t, h, wire.InitializeParams{
		ProjectRoot: root, ProjectID: id,
		ProtocolVersion: protocol.ProtocolVersion, OpsVersion: protocol.OpsVersion + 1,
	})
	if pe, ok := protocol.AsError(err); !ok || pe.Code != protocol.ProtocolMismatch {
		t.Fatalf("internal ops reach the disk; their version is exact: %v", err)
	}
}

// wire.Methods is what initialize advertises as capabilities. This reads the
// handler's switch so the list cannot drift from what the core answers.
func TestRPCHandler_ServesExactlyTheWireMethods(t *testing.T) {
	var got []string
	for m := range rpcMethods {
		got = append(got, m)
	}
	sort.Strings(got)
	var want []string
	for _, m := range wire.Methods() {
		if m != wire.MethodCancelRequest { // answered by the transport
			want = append(want, m)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("handler serves %d methods, wire lists %d:\n handler: %v\n wire:    %v", len(got), len(want), got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("handler serves %q where wire lists %q — add the method to protocol/wire/methods.go (and PROTOCOL.md)", got[i], want[i])
		}
	}
}

// The wire names the stream kinds by their strings; the llm module cannot
// import the contract (it sits below it), so this pins the two together.
func TestStreamKindsMatchTheWire(t *testing.T) {
	pairs := map[llm.StreamEventKind]string{
		llm.StreamEventMessageDelta:      wire.EventMessageDelta,
		llm.StreamEventReasoningDelta:    wire.EventReasoningDelta,
		llm.StreamEventToolCallStart:     wire.EventToolCallStart,
		llm.StreamEventToolCallDelta:     wire.EventToolCallDelta,
		llm.StreamEventToolCallCompleted: wire.EventToolCallCompleted,
		llm.StreamEventStepDone:          wire.EventStepDone,
		llm.StreamEventPendingOps:        wire.EventPendingOps,
		llm.StreamEventRecoverableError:  wire.EventRecoverableError,
		llm.StreamEventDone:              wire.EventDone,
		llm.StreamEventError:             wire.EventError,
		llm.StreamEventTodosUpdated:      wire.EventTodosUpdated,
		llm.StreamEventStepUsage:         wire.EventStepUsage,
		llm.StreamEventContextEstimate:   wire.EventContextEstimate,
	}
	for kind, name := range pairs {
		if string(kind) != name {
			t.Errorf("llm %q vs wire %q", kind, name)
		}
	}
	// exec_output is not an agent/event: it goes out as exec/output_chunk.
	if string(llm.StreamEventExecOutput) == wire.EventError {
		t.Fatal("unreachable")
	}
}

// Typed payloads: what the agent carries as JSON text goes out as the
// wire's structs, so the generated TypeScript describes what arrives.
func TestEmitAgentStreamEvent_TypedPayloads(t *testing.T) {
	t.Setenv("ORCH_STREAM_DEBOUNCE_MS", "0")
	var got []wire.AgentEvent
	onEvent := buildAgentOnEventWithChild(func(_ string, p any) {
		if ev, ok := p.(wire.AgentEvent); ok {
			got = append(got, ev)
		}
	}, EventEnvelope{TurnID: "t"}, &ChildScopeMeta{TaskID: "task-1", SubagentType: "worker"})
	onEvent(agentEvent(llm.StreamEventPendingOps, `{"ops":[{"type":"file.write_atomic","path":"a.go"}],"diff":[{"path":"a.go","before":"","after":"x"}],"applied":false}`))
	onEvent(agentEvent(llm.StreamEventStepUsage, `{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12,"cached_prompt_tokens":8}`))
	onEvent(agentEvent(llm.StreamEventContextEstimate, `{"prompt_tokens":40,"source":"estimate","breakdown":[{"key":"system","label":"System","tokens":30}]}`))
	onEvent(agentEvent(llm.StreamEventPendingOps, `not json`))
	if len(got) != 4 {
		t.Fatalf("got %d events", len(got))
	}
	ops, ok := got[0].Data.(wire.PendingOps)
	if !ok || len(ops.Ops) != 1 || ops.Ops[0]["path"] != "a.go" || len(ops.Diff) != 1 || ops.Diff[0].After != "x" {
		t.Fatalf("pending_ops data: %#v", got[0].Data)
	}
	if got[0].Scope != "child" || got[0].TaskID != "task-1" || got[0].SubagentType != "worker" || got[0].TurnID != "t" {
		t.Fatalf("child scope and envelope: %+v", got[0])
	}
	if u, ok := got[1].Data.(wire.Usage); !ok || u.PromptTokens != 10 || u.CachedPromptTokens != 8 {
		t.Fatalf("step_usage data: %#v", got[1].Data)
	}
	if u, ok := got[2].Data.(wire.Usage); !ok || u.Source != "estimate" || len(u.Breakdown) != 1 || u.Breakdown[0].Tokens != 30 {
		t.Fatalf("context_estimate data: %#v", got[2].Data)
	}
	// A payload that does not parse is not dropped: it goes out as text.
	if got[3].Data != nil || got[3].Content != "not json" {
		t.Fatalf("unparsable payload: %+v", got[3])
	}
}

func agentEvent(kind llm.StreamEventKind, content string) agent.AgentEvent {
	return agent.AgentEvent{Step: 1, Stream: llm.StreamEvent{Kind: kind, Content: content}}
}
