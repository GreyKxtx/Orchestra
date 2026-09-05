package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/orchestra/orchestra/docs/examples"
	"github.com/orchestra/orchestra/internal/memory"
)

// orchestraMDLanguagePlaceholder marks the spot in the template filled with
// the languages workspace-detect actually found — never the LSP fallback
// trio (go+typescript+python), which would lie about an empty repo.
const orchestraMDLanguagePlaceholder = "{{LANGUAGES}}"

// renderOrchestraTemplate fills the language line of the embedded template.
// Detection finding nothing leaves the field blank for a human to fill,
// exactly like the template does before any substitution.
func renderOrchestraTemplate(languages []string) string {
	value := ""
	if len(languages) > 0 {
		value = " " + strings.Join(languages, ", ")
	}
	return strings.Replace(examples.OrchestraTemplate, orchestraMDLanguagePlaceholder, value, 1)
}

// ensureOrchestraMD writes ORCHESTRA.md from the template when the project
// has no project-instruction file at all yet. It never overwrites an
// existing ORCHESTRA.md — the human may have written project-specific rules
// into it — and it also skips when AGENTS.md / CLAUDE.md / .cursorrules
// already has real content: ORCHESTRA.md wins that fallback at runtime
// (memory.FindProjectInstructions), so writing an empty stub would shadow
// working instructions instead of adding to them. Returns the action taken
// ("created", "would-create", or "exists") and, for "exists" via a
// fallback, which file was found (empty when ORCHESTRA.md itself is what
// exists). Never writes anything under dryRun.
func ensureOrchestraMD(projectRoot string, dryRun bool, languages []string) (action, foundFallback string, err error) {
	path := filepath.Join(projectRoot, "ORCHESTRA.md")
	if _, statErr := os.Stat(path); statErr == nil {
		return "exists", "", nil
	} else if !os.IsNotExist(statErr) {
		return "", "", statErr
	}
	if _, name := memory.FindProjectInstructions(projectRoot); name != "" {
		return "exists", name, nil
	}
	if dryRun {
		return "would-create", "", nil
	}
	if err := os.WriteFile(path, []byte(renderOrchestraTemplate(languages)), 0o644); err != nil {
		return "", "", err
	}
	return "created", "", nil
}

// reportOrchestraMD prints what ensureOrchestraMD did, matching the tone of
// the other init steps (ensureGitignore, ensureLearningDirs).
func reportOrchestraMD(action, foundFallback string) {
	switch action {
	case "created":
		fmt.Println("Created ORCHESTRA.md — edit it with your stack, build/test commands, and conventions.")
	case "would-create":
		fmt.Println("[dry-run] would create ORCHESTRA.md")
	case "exists":
		if foundFallback != "" {
			fmt.Printf("%s already provides project instructions — Orchestra reads it; not creating ORCHESTRA.md.\n", foundFallback)
		}
	}
}
