package llm

import "sync"

// Prompt-size calibration.
//
// The wire guard has to know how big a request is BEFORE sending it, and the
// only universal input it has is the byte count. EstimateTokensFromBytes turns
// bytes into tokens at roughly 1.8 bytes per token — deliberately pessimistic,
// because guessing low means the server rejects the request outright.
//
// Measured against a real turn that assumption is out by more than double: LM
// Studio charged 9178 tokens for a request the estimator scored at ~20000. On a
// 20k local model that is not caution, it is a refusal to work — an evaluation
// run had six turns rejected at step zero with nothing in history at all.
//
// Every OpenAI-compatible response reports what the prompt actually cost, so
// the ratio does not have to be guessed after the first answer. The client
// records bytes-per-token from the provider's own usage and estimates from
// that, keeping the most pessimistic measurement it has seen.

const (
	// Outside this range a reported ratio is not a tokenizer, it is a bad
	// report (a proxy that echoes zero, a server that counts characters).
	minObservedBytesPerToken = 1.2
	maxObservedBytesPerToken = 12.0

	// calibrationSafety shaves the measured ratio so the estimate stays a
	// little above reality: prompts vary, and being slightly pessimistic costs
	// a few tokens while being optimistic costs the whole request.
	calibrationSafety = 0.85

	// calibrationOverheadTokens covers the envelope the byte count does not
	// see — role markers, chat template scaffolding, per-message separators.
	calibrationOverheadTokens = 512
)

// promptCalibration is the measured bytes-per-token for one endpoint.
type promptCalibration struct {
	mu            sync.Mutex
	bytesPerToken float64 // 0 = nothing measured yet
}

// observe records one (request bytes, prompt tokens) pair from a response.
func (p *promptCalibration) observe(reqBytes, promptTokens int) {
	if reqBytes <= 0 || promptTokens <= 0 {
		return
	}
	ratio := float64(reqBytes) / float64(promptTokens)
	if ratio < minObservedBytesPerToken || ratio > maxObservedBytesPerToken {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	// The densest prompt seen wins: it is the one that would overflow.
	if p.bytesPerToken == 0 || ratio < p.bytesPerToken {
		p.bytesPerToken = ratio
	}
}

// ratio returns the measured bytes-per-token, or 0 when nothing is known.
func (p *promptCalibration) ratio() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.bytesPerToken
}

// observePromptUsage feeds one response's real prompt cost back into the
// estimator. Called from both the buffered and the streaming paths.
func (c *OpenAIClient) observePromptUsage(reqBytes, promptTokens int) {
	if c == nil {
		return
	}
	c.promptCal.observe(reqBytes, promptTokens)
}

// watchPromptUsage forwards a stream untouched while reading the prompt cost
// out of its final event. A stream reports usage only at the end, so this is
// the one place the streaming path can learn what the buffered path learns
// from the response body.
func (c *OpenAIClient) watchPromptUsage(in <-chan StreamEvent, reqBytes int) <-chan StreamEvent {
	if c == nil || in == nil {
		return in
	}
	out := make(chan StreamEvent)
	go func() {
		defer close(out)
		for ev := range in {
			if ev.Kind == StreamEventDone && ev.Response != nil && ev.Response.Usage != nil {
				c.observePromptUsage(reqBytes, ev.Response.Usage.PromptTokens)
			}
			out <- ev
		}
	}()
	return out
}

// estimatePromptTokens is the size of this request in tokens: measured when the
// endpoint has told us what its tokenizer does, pessimistic bytes-based
// arithmetic until then.
func (c *OpenAIClient) estimatePromptTokens(req CompleteRequest) int {
	bytes := estimateRequestBytes(req)
	if c == nil {
		return EstimateTokensFromBytes(bytes)
	}
	ratio := c.promptCal.ratio()
	if ratio <= 0 {
		return EstimateTokensFromBytes(bytes)
	}
	est := int(float64(bytes)/(ratio*calibrationSafety)) + calibrationOverheadTokens
	// Calibration may only make the estimate more accurate, never wilder than
	// the arithmetic it replaces.
	if pessimistic := EstimateTokensFromBytes(bytes); est > pessimistic {
		return pessimistic
	}
	return est
}
