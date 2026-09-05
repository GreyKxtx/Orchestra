package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/orchestra/orchestra/docs/examples"
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
// has none yet. It never overwrites a file that is already there — the
// human may have already written project-specific rules into it — and never
// writes anything under dryRun. Returns "created", "would-create" (dryRun),
// or "exists".
func ensureOrchestraMD(projectRoot string, dryRun bool, languages []string) (string, error) {
	path := filepath.Join(projectRoot, "ORCHESTRA.md")
	if _, err := os.Stat(path); err == nil {
		return "exists", nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if dryRun {
		return "would-create", nil
	}
	if err := os.WriteFile(path, []byte(renderOrchestraTemplate(languages)), 0o644); err != nil {
		return "", err
	}
	return "created", nil
}

// reportOrchestraMD prints what ensureOrchestraMD did, matching the tone of
// the other init steps (ensureGitignore, ensureLearningDirs).
func reportOrchestraMD(action string) {
	switch action {
	case "created":
		fmt.Println("Created ORCHESTRA.md — edit it with your stack, build/test commands, and conventions.")
	case "would-create":
		fmt.Println("[dry-run] would create ORCHESTRA.md")
	}
}
