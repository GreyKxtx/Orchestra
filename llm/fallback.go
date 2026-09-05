package llm

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// FallbackClient runs on a primary provider and moves to a secondary one when
// the primary becomes unreachable.
//
// The failure it exists for is an endpoint that is down for hours, not a
// flaky packet: in the August field run a dead tunnel produced 183 identical
// connect errors across a day, one per step, each paying the full dial
// timeout. So the switch latches — after the first failover every later call
// goes straight to the secondary, and the primary is not probed again for the
// life of this client (a new run, or a restarted core, starts on the primary
// again).
//
// Only connect-level failures fail over. A 400, a refused tool schema or a
// content filter is the provider answering, not an outage; retrying that
// elsewhere would hide a bad request behind a second bill.
type FallbackClient struct {
	primary       Client
	primaryName   string
	secondary     Client
	secondaryName string

	// OnSwitch, when set, is called once — on the failover itself — with the
	// provider names and the error that caused it. Set it before first use.
	OnSwitch func(from, to string, err error)

	mu       sync.Mutex
	switched bool
}

// NewFallbackClient wraps primary with a standby secondary. The names are
// labels for messages and usage records, not lookups.
func NewFallbackClient(primary Client, primaryName string, secondary Client, secondaryName string) *FallbackClient {
	return &FallbackClient{
		primary:       primary,
		primaryName:   primaryName,
		secondary:     secondary,
		secondaryName: secondaryName,
	}
}

// ActiveProvider names the provider currently in use.
func (f *FallbackClient) ActiveProvider() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.switched {
		return f.secondaryName
	}
	return f.primaryName
}

// active returns the client to try first, and whether a failover is still
// available.
func (f *FallbackClient) active() (Client, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.switched {
		return f.secondary, false
	}
	return f.primary, true
}

// markSwitched latches the failover and fires OnSwitch exactly once, even if
// two concurrent calls both saw the primary fail.
func (f *FallbackClient) markSwitched(cause error) {
	f.mu.Lock()
	first := !f.switched
	f.switched = true
	cb := f.OnSwitch
	from, to := f.primaryName, f.secondaryName
	f.mu.Unlock()
	if first && cb != nil {
		cb(from, to, cause)
	}
}

// failoverError explains both halves: which provider went down and how the
// standby answered. A bare secondary error reads like the standby is broken.
func (f *FallbackClient) failoverError(primaryErr, secondaryErr error) error {
	return fmt.Errorf("provider %q is unreachable (%v) and the fallback %q also failed: %w",
		f.primaryName, primaryErr, f.secondaryName, secondaryErr)
}

// Complete implements Client.
func (f *FallbackClient) Complete(ctx context.Context, req CompleteRequest) (*CompleteResponse, error) {
	c, canFail := f.active()
	resp, err := c.Complete(ctx, req)
	if err == nil || !canFail || !IsUnreachableError(err) {
		return resp, err
	}
	f.markSwitched(err)
	resp, secErr := f.secondary.Complete(ctx, req)
	if secErr != nil {
		return nil, f.failoverError(err, secErr)
	}
	return resp, nil
}

// CompleteStream implements Streamer.
//
// Failover happens only when the primary fails before producing any event.
// Once tokens have reached the caller the turn cannot be restarted elsewhere
// without duplicating output, and a mid-stream stall is a transient anyway.
func (f *FallbackClient) CompleteStream(ctx context.Context, req CompleteRequest) (<-chan StreamEvent, error) {
	c, canFail := f.active()
	s, ok := c.(Streamer)
	if !ok {
		return nil, ErrStreamingUnsupported
	}
	ch, err := s.CompleteStream(ctx, req)
	if err == nil || !canFail || !IsUnreachableError(err) {
		return ch, err
	}
	f.markSwitched(err)
	sec, ok := f.secondary.(Streamer)
	if !ok {
		return nil, ErrStreamingUnsupported
	}
	ch, secErr := sec.CompleteStream(ctx, req)
	if secErr != nil {
		return nil, f.failoverError(err, secErr)
	}
	return ch, nil
}

// Plan implements Client.
func (f *FallbackClient) Plan(ctx context.Context, prompt string) (string, error) {
	c, canFail := f.active()
	out, err := c.Plan(ctx, prompt)
	if err == nil || !canFail || !IsUnreachableError(err) {
		return out, err
	}
	f.markSwitched(err)
	out, secErr := f.secondary.Plan(ctx, prompt)
	if secErr != nil {
		return "", f.failoverError(err, secErr)
	}
	return out, nil
}

// Unwrap exposes the primary so helpers such as AsOpenAIClient can reach the
// concrete client the logger was attached to.
func (f *FallbackClient) Unwrap() Client { return f.primary }

// ContextTokens reports the smaller of the two windows: a run that may land
// on either provider has to budget for the one that fits less.
func (f *FallbackClient) ContextTokens() int {
	ctxOf := func(c Client) int {
		if p, ok := c.(interface{ ContextTokens() int }); ok {
			return p.ContextTokens()
		}
		return 0
	}
	a, b := ctxOf(f.primary), ctxOf(f.secondary)
	switch {
	case a > 0 && b > 0 && b < a:
		return b
	case a > 0:
		return a
	default:
		return b
	}
}

// MaybeWrapFallback returns a FallbackClient when cfg names a resolvable
// standby provider; otherwise it returns main untouched.
//
// logger, when non-nil, is attached to the standby too — otherwise a run that
// fails over would stop writing llm_log.jsonl exactly when the log matters.
func MaybeWrapFallback(main Client, reg ProviderRegistry, cfg LLMConfig, logger *Logger) Client {
	name := strings.TrimSpace(cfg.FallbackProvider)
	if name == "" {
		return main
	}
	fbCfg, ok := reg.FindProvider(name)
	if !ok {
		return main
	}
	// A standby on the same endpoint goes down with the primary; wrapping it
	// would only add a second dial timeout to every step of the outage.
	if sameEndpoint(fbCfg.APIBase, cfg.APIBase) {
		return main
	}
	fbCfg.FallbackProvider = "" // one hop only: no chains, no cycles
	secondary := NewClient(fbCfg)
	if oc, found := AsOpenAIClient(secondary); found && logger != nil {
		oc.SetLogger(logger)
	}
	fb := NewFallbackClient(main, providerLabel(cfg), secondary, name)
	if logger != nil {
		fb.OnSwitch = func(from, to string, err error) {
			logger.LogProviderSwitch(from, to, err.Error())
		}
	}
	return fb
}

func sameEndpoint(a, b string) bool {
	return strings.EqualFold(strings.TrimRight(strings.TrimSpace(a), "/"),
		strings.TrimRight(strings.TrimSpace(b), "/"))
}

// providerLabel names a config for messages and usage records.
func providerLabel(cfg LLMConfig) string {
	if p := strings.TrimSpace(cfg.Provider); p != "" {
		return p
	}
	return "primary"
}

// clientUnwrapper is implemented by the Client decorators (router, fallback).
type clientUnwrapper interface{ Unwrap() Client }

// AsOpenAIClient finds the concrete OpenAI-compatible client inside any stack
// of decorators.
//
// Twenty-odd call sites attach the request logger with a plain type assertion
// on the client. Every wrapper added since would have silently emptied
// llm_log.jsonl at those sites; this is the assertion they should use.
func AsOpenAIClient(c Client) (*OpenAIClient, bool) {
	for i := 0; i < 8 && c != nil; i++ {
		if oc, ok := c.(*OpenAIClient); ok {
			return oc, true
		}
		u, ok := c.(clientUnwrapper)
		if !ok {
			return nil, false
		}
		c = u.Unwrap()
	}
	return nil, false
}
