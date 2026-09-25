package permission

import (
	"context"
	"testing"
)

type stubRequester struct{ name string }

func (s stubRequester) RequestPermission(context.Context, Request) (Response, error) {
	return Response{Approved: true}, nil
}

func TestRequesterRidesInTheContext(t *testing.T) {
	if got := RequesterFrom(context.Background()); got != nil {
		t.Fatalf("a bare context has a requester: %v", got)
	}
	base := context.Background()
	if ctx := WithRequester(base, nil); ctx != base {
		t.Fatal("a nil requester changed the context")
	}
	ctx := WithRequester(base, stubRequester{name: "turn-a"})
	if got, ok := RequesterFrom(ctx).(stubRequester); !ok || got.name != "turn-a" {
		t.Fatalf("RequesterFrom = %v", RequesterFrom(ctx))
	}
	// A child context of another turn's answers to its own client.
	inner := WithRequester(ctx, stubRequester{name: "turn-b"})
	if got := RequesterFrom(inner).(stubRequester); got.name != "turn-b" {
		t.Fatalf("the inner requester is %q", got.name)
	}
	if got := RequesterFrom(ctx).(stubRequester); got.name != "turn-a" {
		t.Fatalf("the outer context was changed: %q", got.name)
	}
}
