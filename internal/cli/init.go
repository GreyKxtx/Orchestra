package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/orchestra/orchestra/docs/examples"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/instrument"
	"github.com/orchestra/orchestra/internal/lsp/provision"
	"github.com/spf13/cobra"
)

var initInstrument bool
var initDryRun bool

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize Orchestra project",
	Long:  "Creates .orchestra.yml configuration file in the project root",
	RunE:  runInit,
}

func init() {
	initCmd.Flags().BoolVar(&initInstrument, "instrument", false, "автоматически добавить OTel SDK инструментацию в проект")
	initCmd.Flags().BoolVar(&initDryRun, "dry-run", false, "показать что будет сделано без записи файлов")
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}
	ctx := context.Background()
	if cmd != nil && cmd.Context() != nil {
		ctx = cmd.Context()
	}
	return initProject(ctx, cwd, InitOptions{
		Instrument: initInstrument,
		DryRun:     initDryRun,
	})
}

// gitignoreMarker makes the bootstrap idempotent: init appends the block only
// when the marker line is absent from an existing .gitignore. The local
// playbooks ignore line is also backfilled on older projects that already
// have the marker.
const gitignoreMarker = "# Orchestra: local secrets & runtime artifacts"

// gitignoreBlock ignores runtime/secret artifacts while keeping the knowledge
// files (state, decisions, plans, specs, playbooks, product docs) tracked.
const gitignoreBlock = gitignoreMarker + ` (added by orchestra init)
.orchestra.local.yml
ORCHESTRA.local.md
*.orchestra.bak
*.bak
*.tmp
.orchestra/*
.orchestra/*.db*
!.orchestra/state.md
!.orchestra/decisions.md
!.orchestra/system.txt
!.orchestra/plans/
.orchestra/plans/local/
!.orchestra/specs/
!.orchestra/playbooks/
.orchestra/playbooks/local/
!.orchestra/product/
!.orchestra/docs/
`

const gitignoreLocalPlaybooks = ".orchestra/playbooks/local/"
const gitignoreLocalPlans = ".orchestra/plans/local/"
const gitignoreSQLite = ".orchestra/*.db*"

// ensureGitignore creates or appends the Orchestra ignore block. Secrets can
// live in .orchestra.local.yml and runtime logs under .orchestra/, so a bare
// `git add .` after init must not be able to commit them.
func ensureGitignore(projectRoot string) error {
	path := filepath.Join(projectRoot, ".gitignore")
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	body := string(existing)
	var extra strings.Builder
	if !strings.Contains(body, gitignoreMarker) {
		block := gitignoreBlock
		if len(existing) > 0 {
			sep := "\n"
			if !strings.HasSuffix(body, "\n") {
				sep = "\n\n"
			}
			block = sep + block
		}
		extra.WriteString(block)
		body += block
	}
	for _, line := range []string{gitignoreLocalPlaybooks, gitignoreLocalPlans, gitignoreSQLite, "*.bak", "*.tmp", "ORCHESTRA.local.md"} {
		if strings.Contains(body, line) {
			continue
		}
		if extra.Len() == 0 && len(body) > 0 && !strings.HasSuffix(body, "\n") {
			extra.WriteByte('\n')
		}
		extra.WriteString(line + "\n")
		body += line + "\n"
	}
	if extra.Len() == 0 {
		return nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.WriteString(extra.String()); err != nil {
		return err
	}
	fmt.Printf("Updated .gitignore: ignoring .orchestra runtime artifacts and .orchestra.local.yml.\n")
	return nil
}

// ensureLearningDirs creates the on-disk learning stack so lessons and local
// playbook overlays can accumulate between sessions without the first write.
func ensureLearningDirs(projectRoot string) error {
	for _, rel := range []string{
		filepath.Join(".orchestra", "memory", "lessons"),
		filepath.Join(".orchestra", "playbooks", "local"),
	} {
		if err := os.MkdirAll(filepath.Join(projectRoot, rel), 0o755); err != nil {
			return err
		}
	}
	return ensureL0Playbooks(projectRoot)
}

// ensureL0Playbooks materializes Orchestra's shipped L0 default playbooks
// into .orchestra/playbooks/l0/ so the Docs Lead can read them with
// fs.read — docs/examples/playbooks/ exists only in Orchestra's own repo
// checkout, not in the project init runs against. Always overwritten:
// these are Orchestra's own reference material refreshed to the installed
// version, not something a project edits — narrowing happens one layer up,
// in conventions.md (L1).
func ensureL0Playbooks(projectRoot string) error {
	dir := filepath.Join(projectRoot, ".orchestra", "playbooks", "l0")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for name, content := range examples.L0Playbooks() {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// suggestLocalOverlay warns when the committed config still carries API keys
// while no .orchestra.local.yml exists — the one migration step init cannot
// do safely on its own (moving a secret means editing the user's config).
func suggestLocalOverlay(projectRoot, configPath string) {
	if _, err := os.Stat(filepath.Join(projectRoot, config.LocalOverlayName)); err == nil {
		return
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if key, val, ok := strings.Cut(trimmed, ":"); ok &&
			strings.TrimSpace(key) == "api_key" && strings.Trim(strings.TrimSpace(val), `"'`) != "" {
			fmt.Printf("Hint: .orchestra.yml contains an api_key. Move secrets to %s (gitignored):\n", config.LocalOverlayName)
			fmt.Printf("  llm:\n    api_key: <your key>\n")
			fmt.Printf("It is merged over .orchestra.yml at load time and never written back.\n")
			return
		}
	}
}

// detectedLanguages returns the languages workspace-detect actually found in
// projectRoot, for the ORCHESTRA.md "Language / runtime" line. Deliberately
// NOT provision.InitServerSpecs, which folds in the go+typescript+python LSP
// fallback on an empty repo — that fallback is right for "give the user
// something to try", wrong for "state what this project is written in".
func detectedLanguages(projectRoot string) []string {
	entries := provision.Detect(projectRoot)
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Language)
	}
	return out
}

func lspServersFromInit(projectRoot string) []config.LSPServerConfig {
	specs := provision.InitServerSpecs(projectRoot)
	out := make([]config.LSPServerConfig, len(specs))
	for i, s := range specs {
		out[i] = config.LSPServerConfig{
			Language:   s.Language,
			Extensions: append([]string(nil), s.Extensions...),
			Command:    append([]string(nil), s.Command...),
		}
	}
	return out
}

func runInstrument(dir string, dryRun bool) error {
	langs := instrument.Detect(dir, instrument.Phase1Langs)
	if len(langs) == 0 {
		fmt.Println("[instrument] No supported languages detected.")
		return nil
	}

	prefix := ""
	if dryRun {
		prefix = "[dry-run] "
	}

	results, err := instrument.Instrument(dir, langs, dryRun)
	for _, r := range results {
		if r.Skipped {
			fmt.Printf("[instrument] %s: skipped — %s\n", r.Lang, r.SkipReason)
			continue
		}
		fmt.Printf("[instrument] %s%s: wrote %s\n", prefix, r.Lang, r.TelemetryFile)
		if r.Patched {
			fmt.Printf("[instrument] %s%s: patched %s\n", prefix, r.Lang, r.PatchedFile)
		}
		if r.InstallOutput != "" {
			fmt.Printf("[instrument] %s%s: install output:\n%s\n", prefix, r.Lang, r.InstallOutput)
		}
	}
	return err
}
