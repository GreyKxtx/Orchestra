package prompt

import (
	"embed"
	"fmt"
	"strings"
)

//go:embed files/*.txt
var promptFiles embed.FS

// BuildSystemPromptForMode returns a system prompt for the given agent mode and model family.
//
// mode: "build" (default), "plan", "explore", "general", "compaction", "title", "summary".
// family: "anthropic", "gpt", "gemini", "local", "" / "default" (see DetectPromptFamily).
//
// Lookup order: {mode}-{family}.txt → {mode}.txt → build.txt
func BuildSystemPromptForMode(mode, family string) string {
	if mode == "" {
		mode = "build"
	}
	if family != "" && family != "default" {
		if s := loadPromptFile(mode + "-" + family + ".txt"); s != "" {
			return s
		}
	}
	if s := loadPromptFile(mode + ".txt"); s != "" {
		return s
	}
	return mustLoadPromptFile("build.txt")
}

// BuildSystemPrompt returns the default build-mode prompt.
func BuildSystemPrompt() string {
	return BuildSystemPromptForMode("build", "")
}

// BuildSystemPromptForFamily returns a build-mode prompt for the given model family.
func BuildSystemPromptForFamily(family string) string {
	return BuildSystemPromptForMode("build", family)
}

func loadPromptFile(name string) string {
	data, err := promptFiles.ReadFile("files/" + name)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func mustLoadPromptFile(name string) string {
	s := loadPromptFile(name)
	if s == "" {
		panic(fmt.Sprintf("prompt: required file %q not found in embed", name))
	}
	return s
}
