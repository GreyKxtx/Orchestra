package core

import (
	"context"
	"testing"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/permission"
	"github.com/orchestra/orchestra/llm"
)

type namedRequester struct{ name string }

func (namedRequester) RequestPermission(context.Context, permission.Request) (permission.Response, error) {
	return permission.Response{Approved: true}, nil
}

// A turn's context carries the client that answers its consent prompts
// (ARCH-5): a language server to install under one session's tool call asks
// that session, not whichever turn last set a requester on the shared runner.
func TestRunContext_CarriesTheTurnsRequester(t *testing.T) {
	l := &agentLaunch{
		Opts:          agent.Options{PermissionRequester: namedRequester{name: "session-a"}},
		EventEnvelope: EventEnvelope{TurnID: "turn-1"},
	}
	ctx := l.RunContext(context.Background())
	got, ok := permission.RequesterFrom(ctx).(namedRequester)
	if !ok || got.name != "session-a" {
		t.Fatalf("the turn's context carries %v, want its own requester", permission.RequesterFrom(ctx))
	}
	if tr := llm.TraceFrom(ctx); tr.RunID != "turn-1" {
		t.Fatalf("the trace is still the turn's: %+v", tr)
	}
	// A launch with no client leaves the context without one.
	bare := (&agentLaunch{EventEnvelope: EventEnvelope{TurnID: "turn-2"}}).RunContext(context.Background())
	if permission.RequesterFrom(bare) != nil {
		t.Fatal("a turn with no client got a requester")
	}
}
