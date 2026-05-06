package prompt

import "strings"

// DetectPromptFamily infers model family from the model name string.
//
// Returns one of: "anthropic", "gpt", "gemini", "local", "default".
//   - "anthropic" — Claude models (Anthropic)
//   - "gpt"       — GPT and O-series models (OpenAI)
//   - "gemini"    — Gemini models (Google)
//   - "local"     — open-source/local models (Qwen, Llama, Mistral, DeepSeek, Gemma, Phi, Yi)
//   - "default"   — unrecognized, uses the base build.txt prompt
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
