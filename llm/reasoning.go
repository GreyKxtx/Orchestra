package llm

import "strings"

// Anthropic rejects a thinking budget below 1024 tokens.
const minThinkingBudget = 1024

// effortBudgets maps an effort level to a thinking budget, for the providers
// that take a number instead of a word. The steps are deliberately coarse:
// this is a dial, not a tuning parameter.
var effortBudgets = map[string]int{
	"minimal": minThinkingBudget,
	"low":     2048,
	"medium":  8192,
	"high":    16384,
	"max":     32768,
}

// budget resolves the thinking budget in tokens. An explicit BudgetTokens
// wins over the effort mapping; anything below the provider floor is raised
// to it rather than being sent and rejected.
func (r *ReasoningConfig) budget() int {
	if r == nil {
		return 0
	}
	n := r.BudgetTokens
	if n <= 0 {
		n = effortBudgets[strings.ToLower(strings.TrimSpace(r.Effort))]
	}
	if n > 0 && n < minThinkingBudget {
		n = minThinkingBudget
	}
	return n
}

// effort returns the normalized effort level, or "" when none was set.
func (r *ReasoningConfig) effort() string {
	if r == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(r.Effort))
}

// empty reports whether the config asks for nothing at all.
func (r *ReasoningConfig) empty() bool {
	return r == nil || (r.effort() == "" && r.BudgetTokens <= 0)
}

// resolveReasoning returns the reasoning settings to apply for model, or nil.
//
// A model the capability snapshot lists *without* a reasoning control gets
// nothing: sending one is a 400, and that is the case the snapshot exists to
// catch. A model the snapshot does not know is the user's call — a local
// finetune or a model newer than the snapshot still gets what was configured.
func resolveReasoning(cfg *ReasoningConfig, model string) *ReasoningConfig {
	if cfg.empty() {
		return nil
	}
	if mi, known := LookupModelInfo(model); known && !mi.Reasoning {
		return nil
	}
	return cfg
}

// openRouterReasoning is OpenRouter's reasoning object. It is a separate
// shape from OpenAI's flat reasoning_effort, and the two are not
// interchangeable: OpenRouter rejects the flat field.
type openRouterReasoning struct {
	Effort    string `json:"effort,omitempty"`
	MaxTokens int    `json:"max_tokens,omitempty"`
}

// applyReasoning writes the reasoning control in the dialect this endpoint
// speaks, or leaves the body untouched when there is nothing to send.
func (c *OpenAIClient) applyReasoning(body *chatCompletionRequest) {
	r := resolveReasoning(c.reasoning, c.model)
	if r == nil {
		return
	}
	if c.reportsCost() { // OpenRouter
		body.Reasoning = &openRouterReasoning{Effort: r.effort(), MaxTokens: r.BudgetTokens}
		return
	}
	// OpenAI and Azure take the flat field, and only a word — a caller who
	// gave only a budget still gets a sensible level out of it.
	effort := r.effort()
	if effort == "" {
		effort = effortFor(r.budget())
	}
	body.ReasoningEffort = effort
}

// effortFor maps a token budget back onto the nearest effort word.
func effortFor(budget int) string {
	switch {
	case budget <= 0:
		return ""
	case budget <= effortBudgets["low"]:
		return "low"
	case budget <= effortBudgets["medium"]:
		return "medium"
	default:
		return "high"
	}
}

// anthropicThinking is Anthropic's extended-thinking block.
type anthropicThinking struct {
	Type         string `json:"type"` // "enabled"
	BudgetTokens int    `json:"budget_tokens"`
}
