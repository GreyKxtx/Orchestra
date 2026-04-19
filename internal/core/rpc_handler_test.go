package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/protocol"
	"github.com/orchestra/orchestra/internal/store"
)

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
	h := NewRPCHandler(c)

	projectID, err := store.ComputeProjectID(root)
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
