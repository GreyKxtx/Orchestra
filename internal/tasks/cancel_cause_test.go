package tasks

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/protocol/schema"
)

// TestClassifyChildRunErr verifies the resilience audit P4 contract: cancel
// causes recorded via context.WithCancelCause map to distinguishable
// SubtaskResult statuses instead of collapsing into "timeout".
func TestClassifyChildRunErr(t *testing.T) {
	cases := []struct {
		name       string
		cause      error
		runErr     error
		wantStatus string
	}{
		{"user_cancel", ErrCauseUserCancel, context.Canceled, "cancelled"},
		{"stale_contract", ErrCauseStaleContract, context.Canceled, "cancelled"},
		{"wait_abandoned", ErrCauseWaitAbandoned, context.Canceled, "cancelled"},
		{"shutdown", ErrCauseShutdown, context.Canceled, "cancelled"},
		{"plain_timeout", nil, context.DeadlineExceeded, "timeout"},
		{"real_error", nil, errors.New("llm exploded"), "error"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancelCause(context.Background())
			if tc.cause != nil {
				cancel(tc.cause)
			} else if errors.Is(tc.runErr, context.DeadlineExceeded) || errors.Is(tc.runErr, context.Canceled) {
				cancel(tc.runErr)
			}
			defer cancel(nil)

			status, msg := classifyChildRunErr(ctx, tc.runErr)
			if status != tc.wantStatus {
				t.Errorf("status = %q, want %q", status, tc.wantStatus)
			}
			if msg == "" {
				t.Error("expected non-empty error message")
			}
			if tc.cause != nil && !errors.Is(context.Cause(ctx), tc.cause) {
				t.Errorf("context.Cause = %v, want %v", context.Cause(ctx), tc.cause)
			}
		})
	}
}

// TestSpawnCancel_ReportsCancelledStatus exercises the full path: a spawned
// child cancelled via Cancel() must come back as "cancelled" with the
// user-cancel cause text, not as a generic "timeout".
func TestSpawnCancel_ReportsCancelledStatus(t *testing.T) {
	blockingLLM := &blockMockLLM{ready: make(chan struct{})}
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("schema.NewValidator: %v", err)
	}
	tr, err := tools.NewRunner(t.TempDir(), tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("tools.NewRunner: %v", err)
	}
	r := New(blockingLLM, v, tr, ChildAgentConfig{})
	t.Cleanup(func() {
		r.Close()
		_ = tr.Close()
	})

	id, err := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{
		Goal:      "hang until cancelled",
		MaxSteps:  1,
		TimeoutMS: 60_000,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if err := r.Cancel(context.Background(), id); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	res, err := r.Wait(context.Background(), id, 5000)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if res.Status != "cancelled" {
		t.Fatalf("status = %q, want %q (error: %s)", res.Status, "cancelled", res.Error)
	}
	if !strings.Contains(res.Error, "task_cancel") {
		t.Errorf("expected user-cancel cause in error, got %q", res.Error)
	}
}
