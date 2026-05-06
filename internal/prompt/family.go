package prompt

import "strings"

// DetectPromptFamily infers model family from the model name string.
// Returns one of: "openai", "qwen", "llama", "mistral", "deepseek", "gemma".
// Falls back to "openai" if unrecognized.
func DetectPromptFamily(modelName string) string {
	lower := strings.ToLower(modelName)
	switch {
	case strings.Contains(lower, "qwen"):
		return "qwen"
	case strings.Contains(lower, "llama"):
		return "llama"
	case strings.Contains(lower, "mistral") || strings.Contains(lower, "mixtral"):
		return "mistral"
	case strings.Contains(lower, "deepseek"):
		return "deepseek"
	case strings.Contains(lower, "gemma"):
		return "gemma"
	default:
		return "openai"
	}
}
