package llm

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// scriptedClient answers with a fixed error or reply and counts its calls.
type scriptedClient struct {
	label  string
	err    error
	calls  int
	stream int
}

func (s *scriptedClient) Complete(ctx context.Context, req CompleteRequest) (*CompleteResponse, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return &CompleteResponse{Message: Message{Role: RoleAssistant, Content: s.label}}, nil
}

func (s *scriptedClient) Plan(ctx context.Context, prompt string) (string, error) {
	return s.label, nil
}

func (s *scriptedClient) CompleteStream(ctx context.Context, req CompleteRequest) (<-chan StreamEvent, error) {
	s.stream++
	if s.err != nil {
		return nil, s.err
	}
	ch := make(chan StreamEvent, 1)
	ch <- StreamEvent{Kind: StreamEventDone, Response: &CompleteResponse{
		Message: Message{Role: RoleAssistant, Content: s.label},
	}}
	close(ch)
	return ch, nil
}

func unreachable() error {
	return &UnreachableError{Endpoint: "https://dead.ngrok.app/v1", Err: errors.New("dial tcp: refused")}
}

func TestFallback_SwitchesWhenThePrimaryIsUnreachable(t *testing.T) {
	primary := &scriptedClient{label: "primary", err: unreachable()}
	secondary := &scriptedClient{label: "secondary"}
	c := NewFallbackClient(primary, "vllm", secondary, "openrouter")

	resp, err := c.Complete(context.Background(), CompleteRequest{})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Message.Content != "secondary" {
		t.Errorf("answered by %q, want the secondary", resp.Message.Content)
	}
	if c.ActiveProvider() != "openrouter" {
		t.Errorf("ActiveProvider = %q, want openrouter", c.ActiveProvider())
	}
}

func TestFallback_StaysOnTheSecondaryAfterSwitching(t *testing.T) {
	// The failure mode this exists for is a dead endpoint for a whole day.
	// Re-probing the primary on every step is what produced 183 errors.
	primary := &scriptedClient{label: "primary", err: unreachable()}
	secondary := &scriptedClient{label: "secondary"}
	c := NewFallbackClient(primary, "vllm", secondary, "openrouter")

	for i := 0; i < 3; i++ {
		if _, err := c.Complete(context.Background(), CompleteRequest{}); err != nil {
			t.Fatal(err)
		}
	}
	if primary.calls != 1 {
		t.Errorf("primary called %d times, want 1 — it must not be retried once latched", primary.calls)
	}
	if secondary.calls != 3 {
		t.Errorf("secondary calls = %d, want 3", secondary.calls)
	}
}

func TestFallback_OrdinaryErrorsAreNotAFailover(t *testing.T) {
	// A 400 from the model is the model's answer, not an outage. Switching
	// providers would hide a bad request behind a different bill.
	primary := &scriptedClient{label: "primary", err: errors.New("400 invalid tool schema")}
	secondary := &scriptedClient{label: "secondary"}
	c := NewFallbackClient(primary, "vllm", secondary, "openrouter")

	if _, err := c.Complete(context.Background(), CompleteRequest{}); err == nil {
		t.Fatal("expected the primary's error to surface")
	}
	if secondary.calls != 0 {
		t.Errorf("secondary called %d times, want 0", secondary.calls)
	}
	if c.ActiveProvider() != "vllm" {
		t.Errorf("ActiveProvider = %q, want the primary still", c.ActiveProvider())
	}
}

func TestFallback_SecondaryFailureReportsBoth(t *testing.T) {
	primary := &scriptedClient{label: "primary", err: unreachable()}
	secondary := &scriptedClient{label: "secondary", err: errors.New("401 no credits")}
	c := NewFallbackClient(primary, "vllm", secondary, "openrouter")

	_, err := c.Complete(context.Background(), CompleteRequest{})
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"vllm", "openrouter", "401 no credits"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestFallback_StreamingFailsOver(t *testing.T) {
	primary := &scriptedClient{label: "primary", err: unreachable()}
	secondary := &scriptedClient{label: "secondary"}
	c := NewFallbackClient(primary, "vllm", secondary, "openrouter")

	ch, err := c.CompleteStream(context.Background(), CompleteRequest{})
	if err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
	var got string
	for ev := range ch {
		if ev.Kind == StreamEventDone && ev.Response != nil {
			got = ev.Response.Message.Content
		}
	}
	if got != "secondary" {
		t.Errorf("stream came from %q, want the secondary", got)
	}
}

func TestFallback_NotifiesOnce(t *testing.T) {
	primary := &scriptedClient{label: "primary", err: unreachable()}
	secondary := &scriptedClient{label: "secondary"}
	c := NewFallbackClient(primary, "vllm", secondary, "openrouter")

	var switches []string
	c.OnSwitch = func(from, to string, err error) { switches = append(switches, from+"→"+to) }

	for i := 0; i < 3; i++ {
		_, _ = c.Complete(context.Background(), CompleteRequest{})
	}
	if len(switches) != 1 || switches[0] != "vllm→openrouter" {
		t.Errorf("switches = %v, want one vllm→openrouter", switches)
	}
}
