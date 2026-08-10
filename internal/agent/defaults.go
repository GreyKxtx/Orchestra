package agent

import (
	"strings"
	"time"

	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
)

// RetryLimits groups circuit-breaker thresholds for the agent loop.
type RetryLimits struct {
	MaxInvalidRetries    int
	MaxDeniedToolRepeats int
	MaxToolErrorRepeats  int
	MaxFinalFailures     int
}

// FallbackRetryLimits is used when neither config nor provider heuristics apply.
func FallbackRetryLimits() RetryLimits {
	return RetryLimits{
		MaxInvalidRetries:    3,
		MaxDeniedToolRepeats: 2,
		MaxToolErrorRepeats:  6,
		MaxFinalFailures:     6,
	}
}

// RetryLimitsForProvider returns ROADMAP-tuned defaults: frontier APIs need
// fewer invalid-output retries; local OpenAI-compatible servers benefit from more.
func RetryLimitsForProvider(provider string) RetryLimits {
	p := strings.ToLower(strings.TrimSpace(provider))
	switch {
	case strings.Contains(p, "anthropic"),
		strings.Contains(p, "claude"),
		p == "openai",
		strings.Contains(p, "gpt"):
		return RetryLimits{
			MaxInvalidRetries:    1,
			MaxDeniedToolRepeats: 2,
			MaxToolErrorRepeats:  4,
			MaxFinalFailures:     4,
		}
	default:
		return RetryLimits{
			MaxInvalidRetries:    5,
			MaxDeniedToolRepeats: 2,
			MaxToolErrorRepeats:  8,
			MaxFinalFailures:     8,
		}
	}
}

// FillRetryLimits sets zero retry fields on opts using provider heuristics,
// then FallbackRetryLimits. Config/RPC explicit values (>0) must be copied
// onto opts before calling this helper.
func FillRetryLimits(opts *Options, provider string) {
	if opts == nil {
		return
	}
	lim := RetryLimitsForProvider(provider)
	if opts.MaxInvalidRetries <= 0 {
		opts.MaxInvalidRetries = lim.MaxInvalidRetries
	}
	if opts.MaxDeniedToolRepeats <= 0 {
		opts.MaxDeniedToolRepeats = lim.MaxDeniedToolRepeats
	}
	if opts.MaxToolErrorRepeats <= 0 {
		opts.MaxToolErrorRepeats = lim.MaxToolErrorRepeats
	}
	if opts.MaxFinalFailures <= 0 {
		opts.MaxFinalFailures = lim.MaxFinalFailures
	}
	fb := FallbackRetryLimits()
	if opts.MaxInvalidRetries <= 0 {
		opts.MaxInvalidRetries = fb.MaxInvalidRetries
	}
	if opts.MaxDeniedToolRepeats <= 0 {
		opts.MaxDeniedToolRepeats = fb.MaxDeniedToolRepeats
	}
	if opts.MaxToolErrorRepeats <= 0 {
		opts.MaxToolErrorRepeats = fb.MaxToolErrorRepeats
	}
	if opts.MaxFinalFailures <= 0 {
		opts.MaxFinalFailures = fb.MaxFinalFailures
	}
}

// ApplyDefaults fills safety-net defaults on opts (used by New and tests).
func ApplyDefaults(opts *Options) {
	if opts == nil {
		return
	}
	if opts.MaxSteps <= 0 {
		opts.MaxSteps = 24
	}
	FillRetryLimits(opts, opts.ProviderLabel)
	if opts.MaxPromptBytes <= 0 {
		opts.MaxPromptBytes = 64 * 1024
	}
	if opts.LLMStepTimeout <= 0 {
		opts.LLMStepTimeout = 25 * time.Second
	}
}

// ResolveResponseFormat builds grammar-constrained sampling config from llm.* YAML.
func ResolveResponseFormat(cfg llm.LLMConfig) *llm.ResponseFormat {
	if strings.TrimSpace(cfg.ResponseFormatType) == "" {
		return nil
	}
	rf := &llm.ResponseFormat{Type: cfg.ResponseFormatType}
	if cfg.ResponseFormatType == "json_schema" {
		rf.Schema = schema.AgentStepSchemaRaw()
		rf.SchemaName = "agent_step"
	}
	return rf
}
