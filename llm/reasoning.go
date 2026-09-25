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

// anthropicThinking is Anthropic's thinking parameter: {type:"enabled",
// budget_tokens} on models before 4.6, {type:"adaptive"} from 4.6 on.
type anthropicThinking struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
	Display      string `json:"display,omitempty"`
}

// anthropicOutputConfig carries the effort level, adaptive thinking's depth.
type anthropicOutputConfig struct {
	Effort string `json:"effort,omitempty"`
}

// anthropicAdaptiveMaxTokens is the default max_tokens for models that
// think adaptively: thinking and answer share it.
const anthropicAdaptiveMaxTokens = 32000

// anthropicAdaptiveThinking reports whether model takes {type:"adaptive"}
// thinking rather than {type:"enabled", budget_tokens}. Per Anthropic's model
// matrix: 4.5 and earlier take only "enabled" ("adaptive" is a 400); 4.6
// takes both and deprecates "enabled"; 4.7 and later reject "enabled" with a
// 400. A model whose version cannot be read (Mythos Preview, an alias) is
// taken as new: sending "enabled" to one is what breaks.
func anthropicAdaptiveThinking(model string) bool {
	major, minor, ok := claudeVersion(model)
	if !ok {
		return true
	}
	return major > 4 || (major == 4 && minor >= 6)
}

// claudeVersion reads the generation from a Claude model id, in the forms
// the API, Bedrock and Vertex use: claude-opus-4-5-20251101,
// claude-3-7-sonnet-latest, anthropic.claude-sonnet-4-5-20250929-v1:0,
// claude-sonnet-4-5@20250929, claude-opus-5-5. A dated suffix is not a minor
// version: claude-sonnet-4-20250514 is 4.0.
func claudeVersion(model string) (major, minor int, ok bool) {
	m := strings.ToLower(model)
	i := strings.Index(m, "claude")
	if i < 0 {
		return 0, 0, false
	}
	fields := strings.FieldsFunc(m[i+len("claude"):], func(r rune) bool {
		return r == '-' || r == '_' || r == '.' || r == '@' || r == ':' || r == '/'
	})
	small := func(f string) (int, bool) {
		if len(f) == 0 || len(f) > 2 {
			return 0, false
		}
		n := 0
		for _, r := range f {
			if r < '0' || r > '9' {
				return 0, false
			}
			n = n*10 + int(r-'0')
		}
		return n, true
	}
	for k, f := range fields {
		v, isNum := small(f)
		if !isNum {
			continue
		}
		major = v
		if k+1 < len(fields) {
			if w, isNum := small(fields[k+1]); isNum {
				minor = w
			}
		}
		return major, minor, true
	}
	return 0, 0, false
}

// anthropicEffort maps the configured effort onto Anthropic's levels (low,
// medium, high, xhigh, max). A budget alone picks the nearest level.
func anthropicEffort(r *ReasoningConfig) string {
	switch e := r.effort(); e {
	case "":
		return effortFor(r.budget())
	case "minimal":
		return "low"
	default:
		return e
	}
}
