package mcp

import (
	"context"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The SDK advertises the capability from the handler being non-nil, so "not
// opted in" has to mean "no handler" — otherwise the server is invited to send
// requests we will certainly refuse.
func TestRemoteClientOptions_NothingEnabledAdvertisesNothing(t *testing.T) {
	if got := remoteClientOptions("linear", InboundOptions{}); got != nil {
		t.Fatalf("options = %+v, want nil so the SDK advertises no capability", got)
	}
}

func TestRemoteClientOptions_OnlyWhatIsEnabled(t *testing.T) {
	sample := func(context.Context, SamplingRequest) (SamplingResult, error) {
		return SamplingResult{}, nil
	}
	opts := remoteClientOptions("linear", InboundOptions{AllowElicitation: true})
	if opts == nil || opts.ElicitationHandler == nil {
		t.Fatal("elicitation enabled but no handler installed")
	}
	if opts.CreateMessageHandler != nil {
		t.Error("sampling handler installed for a server that did not enable it")
	}

	opts = remoteClientOptions("linear", InboundOptions{AllowSampling: true, Sample: sample})
	if opts == nil || opts.CreateMessageHandler == nil {
		t.Fatal("sampling enabled but no handler installed")
	}
	if opts.ElicitationHandler != nil {
		t.Error("elicitation handler installed for a server that did not enable it")
	}

	// Enabled with no model behind it: promising sampling we cannot perform is
	// worse than staying quiet, exactly as on stdio.
	if opts := remoteClientOptions("linear", InboundOptions{AllowSampling: true}); opts != nil && opts.CreateMessageHandler != nil {
		t.Error("sampling advertised with no sampler wired")
	}
}

// The remote path must go through the same gate as stdio, not around it —
// a consent check that exists on one transport only is not a consent check.
func TestRemoteClientOptions_SamplingGoesThroughConsent(t *testing.T) {
	called := 0
	opts := remoteClientOptions("linear", InboundOptions{
		AllowSampling: true,
		Sample: func(context.Context, SamplingRequest) (SamplingResult, error) {
			called++
			return SamplingResult{Model: "m", Text: "t"}, nil
		},
		Consent: func(context.Context, ConsentRequest) bool { return false },
	})
	_, err := opts.CreateMessageHandler(context.Background(), &mcpsdk.CreateMessageRequest{
		Params: &mcpsdk.CreateMessageParams{MaxTokens: 10},
	})
	if err == nil {
		t.Fatal("a denied sampling request was served over the remote transport")
	}
	if called != 0 {
		t.Error("the model was called despite consent being denied")
	}
}

func TestRemoteClientOptions_SamplingResultCrossesBack(t *testing.T) {
	opts := remoteClientOptions("linear", InboundOptions{
		AllowSampling: true,
		Sample: func(context.Context, SamplingRequest) (SamplingResult, error) {
			return SamplingResult{Model: "test-model", Text: "answer"}, nil
		},
		Consent: func(context.Context, ConsentRequest) bool { return true },
	})
	res, err := opts.CreateMessageHandler(context.Background(), &mcpsdk.CreateMessageRequest{
		Params: &mcpsdk.CreateMessageParams{MaxTokens: 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Model != "test-model" {
		t.Errorf("Model = %q", res.Model)
	}
	tc, ok := res.Content.(*mcpsdk.TextContent)
	if !ok || tc.Text != "answer" {
		t.Errorf("Content = %+v, want the sampled text", res.Content)
	}
}
