package prompt

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
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
	family = NormalizePromptFamily(family)
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

// LoadEmbedded returns an embedded prompt file by name (e.g. "auto-router.txt"), or "".
func LoadEmbedded(name string) string {
	return loadPromptFile(name)
}

// BuildToolDescription returns the embedded tool prompt for name (e.g. "todowrite", "task")
// or fallback when no file exists.
func BuildToolDescription(name, fallback string) string {
	if s := loadPromptFile(name + ".txt"); s != "" {
		return s
	}
	return fallback
}

// SystemOverridePath returns the path to .orchestra/system.txt for workspaceRoot.
func SystemOverridePath(workspaceRoot string) string {
	return filepath.Join(workspaceRoot, ".orchestra", "system.txt")
}

// ModeSystemOverridePath returns the path to a per-mode override,
// .orchestra/system.<mode>.txt.
func ModeSystemOverridePath(workspaceRoot, mode string) string {
	return filepath.Join(workspaceRoot, ".orchestra", "system."+mode+".txt")
}

// LoadModeSystemOverride reads .orchestra/system.<mode>.txt, or "" when absent.
//
// A per-mode file is the only way to override a mode whose prompt carries a
// protocol (worker's WorkOrder + task_result contract, verifier's verdict
// format): the blanket .orchestra/system.txt deliberately does not reach them,
// because an override written for build mode would silently break the contract
// their parent depends on.
func LoadModeSystemOverride(workspaceRoot, mode string) string {
	if workspaceRoot == "" || strings.TrimSpace(mode) == "" {
		return ""
	}
	data, err := os.ReadFile(ModeSystemOverridePath(workspaceRoot, mode))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// LoadSystemOverride reads .orchestra/system.txt from workspaceRoot.
// If the file exists and is non-empty, its content replaces the built-in system prompt entirely.
// Returns empty string when no override is present.
func LoadSystemOverride(workspaceRoot string) string {
	if workspaceRoot == "" {
		return ""
	}
	data, err := os.ReadFile(SystemOverridePath(workspaceRoot))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// WriteSystemOverride atomically writes .orchestra/system.txt (creates .orchestra/ if needed).
// Empty content clears the override (deletes the file).
func WriteSystemOverride(workspaceRoot, content string) error {
	if strings.TrimSpace(workspaceRoot) == "" {
		return fmt.Errorf("workspaceRoot is empty")
	}
	dir := filepath.Join(workspaceRoot, ".orchestra")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir .orchestra: %w", err)
	}
	p := SystemOverridePath(workspaceRoot)
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove system.txt: %w", err)
		}
		return nil
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(trimmed+"\n"), 0o644); err != nil {
		return fmt.Errorf("write temp system.txt: %w", err)
	}
	if err := os.Rename(tmp, p); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename system.txt: %w", err)
	}
	return nil
}
