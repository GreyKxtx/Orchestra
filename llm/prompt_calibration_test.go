package llm

import (
	"strings"
	"testing"
)

func calibrationRequest(bodyBytes int) CompleteRequest {
	return CompleteRequest{
		Messages: []Message{{Role: RoleUser, Content: strings.Repeat("a", bodyBytes)}},
	}
}

// The wire guard assumed ~1.8 bytes per token. Measured against LM Studio on a
// real turn it is nearer 3.7, so a prompt the server counts as 9k was refused
// as "~20000 tokens" — half the window thrown away, and on a 20k model that is
// the difference between working and not working at all.
func TestPromptEstimate_LearnsTheRealRatioFromUsage(t *testing.T) {
	c := &OpenAIClient{contextTokens: 20000}

	const bodyBytes = 34000
	req := calibrationRequest(bodyBytes)

	before := c.estimatePromptTokens(req)
	if before < bodyBytes/2 {
		t.Fatalf("without evidence the estimate must stay pessimistic, got %d", before)
	}

	// The server reports what the prompt really cost: ~3.7 bytes per token.
	c.observePromptUsage(estimateRequestBytes(req), 9178)

	after := c.estimatePromptTokens(req)
	if after >= before {
		t.Fatalf("a measured ratio must lower the estimate: %d -> %d", before, after)
	}
	// Close to the truth, and never optimistic about it.
	if after < 9178 {
		t.Fatalf("the estimate must not undercut what the server actually charged: %d < 9178", after)
	}
	if after > 14000 {
		t.Fatalf("the estimate is still far too pessimistic: %d", after)
	}
}

// A request that really fits must be sent. This is the failure the eval hit:
// six turns refused at step zero with nothing in history.
func TestMaxTokensForRequest_SendsAPromptThatActuallyFits(t *testing.T) {
	c := &OpenAIClient{contextTokens: 20000, wantMaxTokens: 4000, maxTokens: 4000}
	req := calibrationRequest(35000)

	if _, err := c.maxTokensForRequest(req); err == nil {
		t.Fatal("fixture invalid: this prompt must look too large before calibration")
	}
	c.observePromptUsage(estimateRequestBytes(req), 9178)
	if _, err := c.maxTokensForRequest(req); err != nil {
		t.Fatalf("a prompt the server counts at 9178 tokens must be sent on a 20000 window: %v", err)
	}
}

// One odd sample must not make the client reckless: the most pessimistic
// observation wins, so a lucky short prompt cannot unlock an oversized one.
func TestPromptEstimate_KeepsTheMostPessimisticObservation(t *testing.T) {
	c := &OpenAIClient{contextTokens: 20000}
	req := calibrationRequest(34000)

	c.observePromptUsage(estimateRequestBytes(req), 4000) // 8.5 bytes/token — generous
	generous := c.estimatePromptTokens(req)
	c.observePromptUsage(estimateRequestBytes(req), 12000) // 2.8 bytes/token — denser
	dense := c.estimatePromptTokens(req)

	if dense <= generous {
		t.Fatalf("the denser measurement must win: generous=%d dense=%d", generous, dense)
	}
}

// Nonsense from a provider is ignored rather than trusted.
func TestPromptEstimate_IgnoresImpossibleUsage(t *testing.T) {
	c := &OpenAIClient{contextTokens: 20000}
	req := calibrationRequest(34000)
	baseline := c.estimatePromptTokens(req)

	c.observePromptUsage(estimateRequestBytes(req), 0)
	c.observePromptUsage(0, 9178)
	c.observePromptUsage(estimateRequestBytes(req), -5)
	// 100 bytes/token is not a tokenizer, it is a broken report.
	c.observePromptUsage(estimateRequestBytes(req), 340)

	if got := c.estimatePromptTokens(req); got != baseline {
		t.Fatalf("garbage usage must not move the estimate: %d -> %d", baseline, got)
	}
}
