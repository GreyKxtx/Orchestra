package prompt

import "strings"

// DetectPromptFamily infers model family from the model name string.
//
// Returns one of: "anthropic", "gpt", "gemini", "kimi", "local", "default".
//   - "anthropic" — Claude models (Anthropic)
//   - "gpt"       — GPT-4 and O-series reasoning models (OpenAI); beast-mode autonomous execution
//   - "gemini"    — Gemini models (Google)
//   - "kimi"      — Kimi / Moonshot models
//   - "local"     — open-source/local models: Qwen, Llama, Mistral, DeepSeek, Gemma, Phi, Yi
//   - "default"   — unrecognized, falls back to build.txt
func DetectPromptFamily(modelName string) string {
	lower := strings.ToLower(modelName)
	switch {
	case strings.Contains(lower, "claude"):
		return "anthropic"
	case strings.Contains(lower, "gpt") ||
		strings.HasPrefix(lower, "o1") || strings.HasPrefix(lower, "o3") ||
		strings.HasPrefix(lower, "o4"):
		return "gpt"
	case strings.Contains(lower, "gemini"):
		return "gemini"
	case strings.Contains(lower, "kimi") || strings.Contains(lower, "moonshot"):
		return "kimi"
	case strings.Contains(lower, "qwen") ||
		strings.Contains(lower, "llama") ||
		strings.Contains(lower, "mistral") || strings.Contains(lower, "mixtral") ||
		strings.Contains(lower, "deepseek") ||
		strings.Contains(lower, "gemma") ||
		strings.Contains(lower, "phi-") ||
		strings.Contains(lower, "yi-"):
		return "local"
	default:
		return "default"
	}
}
