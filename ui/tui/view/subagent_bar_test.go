package view

import (
	"strings"
	"testing"
	"time"

	"github.com/orchestra/orchestra/ui/tui/state"
)

func TestRenderSubagentBar_RunningAndDone(t *testing.T) {
	running := []state.SubagentTask{{
		TaskID:   "t1",
		Role:     "worker",
		Goal:     "internal/auth/jwt.go",
		Status:   "running",
		Duration: 4 * time.Second,
	}}
	got := RenderSubagentBar(running, 80, 0)
	if got == "" || !strings.Contains(got, "Worker #1") {
		t.Fatalf("running bar: %q", got)
	}

	done := []state.SubagentTask{{
		TaskID:        "t1",
		Role:          "worker",
		Status:        "done",
		Duration:      6200 * time.Millisecond,
		ResultSummary: "Modified ValidateToken (verified by go test)",
	}}
	got = RenderSubagentBar(done, 80, 0)
	if !strings.Contains(got, "Done") || !strings.Contains(got, "ValidateToken") {
		t.Fatalf("done badge: %q", got)
	}
}

func TestRenderSubagentBar_Empty(t *testing.T) {
	if RenderSubagentBar(nil, 80, 0) != "" {
		t.Fatal("empty tasks must render nothing")
	}
}
