package agent

import (
	"testing"

	"github.com/orchestra/orchestra/internal/agent/guard"
)

func TestWorkerResultClaimsSuccess(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{`{"status":"success"}`, true},
		{`{"status":"error","message":"x"}`, false},
		{`{"status":"failed"}`, false},
		{"", true},
		{"not json", true},
	}
	for _, tc := range tests {
		if got := workerResultClaimsSuccess(tc.in); got != tc.want {
			t.Errorf("workerResultClaimsSuccess(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestBlockWorkerTaskResult_BlocksOnPendingLSP(t *testing.T) {
	a := &Agent{
		opts:  Options{Mode: ModeWorker},
		diags: guard.NewDiagTracker(),
	}
	a.diags.Observe("handler.go", "abc123", "LSP_ERRORS — line 1: undefined: Foo")

	err := a.blockWorkerTaskResult(`{"status":"success","path":"handler.go"}`)
	if err == nil {
		t.Fatal("expected block when LSP errors pending")
	}
	if !workerResultClaimsSuccess(`{"status":"success"}`) {
		t.Fatal("sanity")
	}
}

func TestBlockWorkerTaskResult_AllowsErrorStatus(t *testing.T) {
	a := &Agent{
		opts:  Options{Mode: ModeWorker},
		diags: guard.NewDiagTracker(),
	}
	a.diags.Observe("handler.go", "abc123", "LSP_ERRORS — line 1: undefined: Foo")

	if err := a.blockWorkerTaskResult(`{"status":"error","message":"gave up"}`); err != nil {
		t.Fatalf("error status should pass gate: %v", err)
	}
}

func TestBlockWorkerTaskResult_AllowsCleanWorker(t *testing.T) {
	a := &Agent{
		opts:  Options{Mode: ModeWorker},
		diags: guard.NewDiagTracker(),
	}
	if err := a.blockWorkerTaskResult(`{"status":"success"}`); err != nil {
		t.Fatalf("clean worker should pass: %v", err)
	}
}
