package agent

import "testing"

// activeProviderStub reports a provider name the way llm.FallbackClient does.
type activeProviderStub struct{ name string }

func (s activeProviderStub) ActiveProvider() string { return s.name }

func TestProviderLabel_FollowsAFailover(t *testing.T) {
	// After a failover the tokens are billed by the standby, so usage.jsonl
	// must attribute them there — otherwise the ledger blames a provider that
	// was down and shows no spend for the one that answered.
	a := &Agent{}
	a.opts.ProviderLabel = "vllm"
	a.llm = nil
	if got := a.providerLabel(); got != "vllm" {
		t.Fatalf("with no reporter = %q, want the configured label", got)
	}

	a.activeProvider = activeProviderStub{name: "openrouter"}
	if got := a.providerLabel(); got != "openrouter" {
		t.Fatalf("after failover = %q, want openrouter", got)
	}

	// An empty report is not an answer; keep the configured label.
	a.activeProvider = activeProviderStub{name: "  "}
	if got := a.providerLabel(); got != "vllm" {
		t.Fatalf("blank report = %q, want the configured label", got)
	}
}
