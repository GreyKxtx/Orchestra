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
// Lookup order: {mode}-{family}.txt → {mode}.txt (+ addendum-{family}.txt) → build.txt
//
// The addendum exists because family tuning used to reach build mode only:
// build-local.txt is the most detailed prompt in the set, and every bit of it
// was lost the moment the agent ran as a worker, a verifier or in debug mode —
// which is where local models spend most of their time. Rather than fork every
// prompt per family, a mode without its own family variant gets a short shared
// addendum appended.
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
	base := loadPromptFile(mode + ".txt")
	if base == "" {
		return mustLoadPromptFile("build.txt")
	}
	if add := familyAddendum(mode, family); add != "" {
		base += "\n\n" + add
	}
	return base
}

// modesWithoutFamilyAddendum are runtime-internal single-shot prompts with an
// exact output contract (a markdown checkpoint, a title, a summary). Tool
// discipline does not apply to them and the extra text would only contradict
// the contract.
var modesWithoutFamilyAddendum = map[string]bool{
	"compaction": true,
	"title":      true,
	"summary":    true,
}

// familyAddendum returns the shared family-specific block for a mode that has
// no {mode}-{family}.txt of its own, or "" when there is nothing to add.
func familyAddendum(mode, family string) string {
	if family == "" || family == "default" || modesWithoutFamilyAddendum[mode] {
		return ""
	}
	return loadPromptFile("addendum-" + family + ".txt")
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
