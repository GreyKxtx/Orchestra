package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// panickyMCP is an MCPCaller that panics on Call. Used to verify that
// Runner.Call's deferred recover converts the panic to an error instead
// of unwinding the calling goroutine (which, in the parallel batch path,
// would take down the whole process). C3 regression.
type panickyMCP struct{}

func (panickyMCP) Call(_ context.Context, name string, _ json.RawMessage) (json.RawMessage, error) {
	panic("intentional panic from mock MCP tool " + name)
}

func TestRunnerCall_RecoversFromPanic(t *testing.T) {
	root := t.TempDir()
	r, err := NewRunner(root, RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	r.SetMCPCaller(panickyMCP{})

	_, err = r.Call(context.Background(), "mcp:foo:explode", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error from panicking tool, got nil")
	}
	if !strings.Contains(err.Error(), "panicked") {
		t.Errorf("error message should mention panic, got %q", err.Error())
	}
}
