package core

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/orchestra/orchestra/llm"
)

// modelLimitDiscoveryTimeout bounds the background ask. Generous, because
// nothing waits on it: a server that is slow to answer still gets to answer.
const modelLimitDiscoveryTimeout = 10 * time.Second

// startModelLimitDiscovery asks the LLM server what the model's context
// window really is, off the constructor's path. New has already applied the
// static catalogue's answer; this replaces it with the server's when the
// server has one, via applyDiscoveredModelLimits on the next RPC.
//
// It used to be a blocking call in New with an 8-second timeout. For a
// reachable endpoint that was a fifth of a second on every open; for an
// endpoint that was down, or on a VPN that was not up, it was the whole eight
// seconds, during which the window showed a start screen doing nothing.
func (c *Core) startModelLimitDiscovery() {
	if c == nil || c.cfg == nil {
		return
	}
	// A value copy: the goroutine must not read cfg.LLM while writers are
	// mutating it under runMu.
	snapshot := c.cfg.LLM
	if strings.TrimSpace(snapshot.APIBase) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), modelLimitDiscoveryTimeout)
	c.limitsCancel = cancel
	go func() {
		defer cancel()
		lim, err := llm.DiscoverModelLimits(ctx, snapshot)
		if err != nil || lim.ContextTokens <= 0 {
			if c.debug && err != nil && ctx.Err() == nil {
				fmt.Fprintf(os.Stderr, "orchestra: model limit discovery: %v\n", err)
			}
			return
		}
		c.limitsMu.Lock()
		c.discoveredLimits = &lim
		c.limitsMu.Unlock()
	}()
}

// applyDiscoveredModelLimits folds the server's answer into cfg.LLM, once it
// has arrived. Called before every RPC is dispatched, beside
// RefreshConfigIfChanged, and idempotent: ApplyDiscoveredLimits reports
// whether anything changed, and only a change rebuilds the client.
//
// Kept rather than consumed, so a config reloaded from disk — which
// RefreshConfigIfChanged does whenever the file's mtime moves — gets the
// discovered window back on the next call instead of losing it.
func (c *Core) applyDiscoveredModelLimits() {
	if c == nil || c.cfg == nil || c.llmClientInjected {
		return
	}
	c.limitsMu.Lock()
	lim := c.discoveredLimits
	c.limitsMu.Unlock()
	if lim == nil {
		return
	}
	if !c.runMu.TryLock() {
		return // agent turn in flight; retried on the next RPC
	}
	defer c.runMu.Unlock()

	c.cfgMu.Lock()
	// The model may have been switched since the ask went out; a window
	// measured for one model says nothing about another.
	sameModel := strings.EqualFold(strings.TrimSpace(c.cfg.LLM.Model), strings.TrimSpace(lim.Model))
	changed := sameModel && llm.ApplyDiscoveredLimits(&c.cfg.LLM, *lim)
	fresh := c.cfg.LLM
	c.cfgMu.Unlock()
	if !changed {
		return
	}
	client := llm.NewClient(fresh)
	if oc, ok := llm.AsOpenAIClient(client); ok {
		oc.SetLogger(llm.NewLogger(c.workspaceRoot))
	}
	c.llmClient = llm.MaybeWrapFallback(client, c.cfg.LLMRegistry(), fresh, llm.NewLogger(c.workspaceRoot))
	c.publishSamplingTarget()
}
