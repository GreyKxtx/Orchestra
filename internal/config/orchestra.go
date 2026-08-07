package config

import "strings"

// AutoRouterConfig classifies user queries into build|plan|explore when mode=agent.
type AutoRouterConfig struct {
	Enabled  *bool  `yaml:"enabled,omitempty"`  // default true when mode=agent
	Provider string `yaml:"provider,omitempty"` // named providers: entry; empty → fast_provider or main
	Model    string `yaml:"model,omitempty"`
}

// ResolvedEnabled reports whether auto-router LLM classification is enabled (default true).
func (a AutoRouterConfig) ResolvedEnabled() bool {
	if a.Enabled == nil {
		return true
	}
	return *a.Enabled
}

// OrchestraRole is provider/model for Lead or a worker tier.
type OrchestraRole struct {
	Provider string `yaml:"provider,omitempty"`
	Model    string `yaml:"model,omitempty"`
}

// OrchestraTier is one worker difficulty band (Claude-like opus/sonnet/haiku).
type OrchestraTier struct {
	Name     string `yaml:"name"` // complex | focused | micro
	Provider string `yaml:"provider,omitempty"`
	Model    string `yaml:"model,omitempty"`
}

// OrchestraConfig configures Lead + worker tiers for mode=orchestra.
type OrchestraConfig struct {
	Planner          OrchestraRole   `yaml:"planner,omitempty"`
	Tiers            []OrchestraTier `yaml:"tiers,omitempty"`
	DefaultTier      string          `yaml:"default_tier,omitempty"` // default focused
	MaxWorkerRetries int             `yaml:"max_worker_retries,omitempty"`
}

// ResolvedDefaultTier returns the default worker tier name.
func (o OrchestraConfig) ResolvedDefaultTier() string {
	if strings.TrimSpace(o.DefaultTier) != "" {
		return strings.TrimSpace(o.DefaultTier)
	}
	return "focused"
}

// ResolvedMaxWorkerRetries returns worker validation retry budget (default 3).
func (o OrchestraConfig) ResolvedMaxWorkerRetries() int {
	if o.MaxWorkerRetries <= 0 {
		return 3
	}
	return o.MaxWorkerRetries
}

// FindTier looks up a worker tier by name (case-insensitive).
func (o OrchestraConfig) FindTier(name string) *OrchestraTier {
	want := strings.TrimSpace(name)
	if want == "" {
		want = o.ResolvedDefaultTier()
	}
	for i := range o.Tiers {
		if strings.EqualFold(strings.TrimSpace(o.Tiers[i].Name), want) {
			return &o.Tiers[i]
		}
	}
	return nil
}
