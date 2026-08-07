package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/lsp/provision"
	"github.com/orchestra/orchestra/internal/lsp/registry"
	"github.com/spf13/cobra"
)

var lspCmd = &cobra.Command{
	Use:   "lsp",
	Short: "Inspect and provision language servers",
	Long: `Language server catalog, resolve status, and (later) auto-install.

TUI-first provisioning is documented in docs/architecture/lsp-auto-provision.md.
Phase A: list / status / doctor. Phase B: ensure + TUI consent.`,
}

var lspListCmd = &cobra.Command{
	Use:   "list",
	Short: "List built-in language-server recipes",
	Args:  cobra.NoArgs,
	RunE:  runLSPList,
}

var lspStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show configured servers and whether binaries resolve",
	Args:  cobra.NoArgs,
	RunE:  runLSPStatus,
}

var lspDoctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose LSP config, PATH/cache, and toolchains",
	Args:  cobra.NoArgs,
	RunE:  runLSPDoctor,
}

var lspEnsureCmd = &cobra.Command{
	Use:   "ensure [language|id]",
	Short: "Install a language server into ~/.orchestra/lsp (phase B)",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runLSPEnsure,
}

var lspEnsureDetect bool

func init() {
	lspEnsureCmd.Flags().BoolVar(&lspEnsureDetect, "detect", false, "detect project languages and ensure all automated servers")
	lspCmd.AddCommand(lspListCmd, lspStatusCmd, lspDoctorCmd, lspEnsureCmd)
	rootCmd.AddCommand(lspCmd)
}

func runLSPList(cmd *cobra.Command, _ []string) error {
	w := cmd.OutOrStdout()
	fmt.Fprintln(w, "Built-in LSP recipes:")
	fmt.Fprintln(w)
	for _, e := range registry.All() {
		fmt.Fprintf(w, "  %-28s lang=%-12s bin=%s\n", e.ID, e.Language, e.BinaryName)
		fmt.Fprintf(w, "    extensions: %s\n", strings.Join(e.Extensions, " "))
		fmt.Fprintf(w, "    version:    %s\n", e.Version)
		fmt.Fprintf(w, "    install:    %s\n", e.InstallHint)
		if e.RuntimeHint != "" {
			fmt.Fprintf(w, "    runtime:    %s\n", e.RuntimeHint)
		}
		fmt.Fprintln(w)
	}
	return nil
}

func loadProjectLSP() (*config.ProjectConfig, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	cfgPath := filepath.Join(cwd, ".orchestra.yml")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("config: %w (run 'orchestra init' first)", err)
	}
	return cfg, nil
}

func configuredFrom(cfg *config.ProjectConfig) []provision.ConfiguredServer {
	out := make([]provision.ConfiguredServer, 0, len(cfg.LSP.Servers))
	for _, s := range cfg.LSP.Servers {
		out = append(out, provision.ConfiguredServer{
			Language:   s.Language,
			Extensions: s.Extensions,
			Command:    s.Command,
			Disabled:   s.Disabled,
		})
	}
	return out
}

func runLSPStatus(cmd *cobra.Command, _ []string) error {
	cfg, err := loadProjectLSP()
	if err != nil {
		return err
	}
	return printLSPStatus(cmd.OutOrStdout(), cfg, false)
}

func runLSPDoctor(cmd *cobra.Command, _ []string) error {
	cfg, err := loadProjectLSP()
	if err != nil {
		return err
	}
	return printLSPStatus(cmd.OutOrStdout(), cfg, true)
}

func printLSPStatus(w io.Writer, cfg *config.ProjectConfig, doctor bool) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	cache, err := provision.CacheRoot()
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "auto_install: %s\n", cfg.LSP.EffectiveAutoInstall())
	fmt.Fprintf(w, "cache:        %s\n", cache)
	fmt.Fprintf(w, "workspace:    %s\n", cwd)
	fmt.Fprintln(w)

	configured := configuredFrom(cfg)
	if len(configured) == 0 {
		fmt.Fprintln(w, "Configured (.orchestra.yml): (none)")
		fmt.Fprintln(w)
	} else {
		fmt.Fprintln(w, "Configured (.orchestra.yml):")
		for _, st := range provision.InspectConfigured(configured) {
			printServerStatus(w, st, doctor)
		}
	}

	detected := provision.Detect(cwd)
	fmt.Fprintln(w, "Detected (workspace markers / extensions):")
	if len(detected) == 0 {
		fmt.Fprintln(w, "  (none)")
		fmt.Fprintln(w)
	} else {
		detSpecs := make([]provision.ConfiguredServer, 0, len(detected))
		for _, e := range detected {
			spec := provision.SpecFromEntry(e)
			detSpecs = append(detSpecs, provision.ConfiguredServer{
				Language:   spec.Language,
				Extensions: spec.Extensions,
				Command:    spec.Command,
			})
		}
		for _, st := range provision.InspectConfigured(detSpecs) {
			printServerStatus(w, st, doctor)
		}
	}

	// Effective = yaml ∪ detect (same merge runtime uses).
	cfgSpecs := make([]provision.ServerSpec, 0, len(configured))
	for _, s := range configured {
		cfgSpecs = append(cfgSpecs, provision.ServerSpec{
			Language:   s.Language,
			Extensions: s.Extensions,
			Command:    s.Command,
			Disabled:   s.Disabled,
		})
	}
	merged := provision.MergeServers(cfgSpecs, detected)
	fmt.Fprintln(w, "Effective (runtime merge):")
	if len(merged) == 0 {
		fmt.Fprintln(w, "  (none)")
		fmt.Fprintln(w)
	} else {
		eff := make([]provision.ConfiguredServer, 0, len(merged))
		for _, s := range merged {
			eff = append(eff, provision.ConfiguredServer{
				Language:   s.Language,
				Extensions: s.Extensions,
				Command:    s.Command,
				Disabled:   s.Disabled,
			})
		}
		for _, st := range provision.InspectConfigured(eff) {
			printServerStatus(w, st, doctor)
		}
	}

	if doctor {
		fmt.Fprintln(w, "Registry recipes:")
		for _, e := range registry.All() {
			auto := ""
			if provision.CanEnsure(e.ID) {
				auto = " [auto-ensure]"
			}
			fmt.Fprintf(w, "  %s (%s)%s — %s\n", e.ID, e.Language, auto, e.InstallHint)
		}
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Next: orchestra lsp ensure   # detect + install missing")
		fmt.Fprintln(w, "Docs: docs/architecture/lsp-auto-provision.md")
	}
	return nil
}

func printServerStatus(w io.Writer, st provision.ServerStatus, doctor bool) {
	ext := strings.Join(st.Extensions, ",")
	cmd := strings.Join(st.Command, " ")
	fmt.Fprintf(w, "  %s  [%s]  %s\n", st.Language, ext, cmd)
	if st.OK {
		fmt.Fprintf(w, "    binary: OK (%s) %s\n", st.Source, st.Resolved)
	} else {
		fmt.Fprintf(w, "    binary: MISSING\n")
		if st.Error != "" {
			fmt.Fprintf(w, "    error:  %s\n", st.Error)
		}
		if st.Hint != "" {
			fmt.Fprintf(w, "    hint:   %s\n", st.Hint)
		}
	}
	if doctor && st.RuntimeMsg != "" {
		flag := "OK"
		if !st.RuntimeOK {
			flag = "WARN"
		}
		fmt.Fprintf(w, "    runtime: %s — %s\n", flag, st.RuntimeMsg)
	}
	fmt.Fprintln(w)
}

func runLSPEnsure(cmd *cobra.Command, args []string) error {
	if lspEnsureDetect || len(args) == 0 {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "detecting languages in %s…\n", cwd)
		installed, skipped, err := provision.EnsureDetected(cmd.Context(), cwd, "true", nil)
		if err != nil {
			return err
		}
		if len(installed) == 0 && len(skipped) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "nothing to install (already present or no languages detected)")
			return nil
		}
		for _, id := range installed {
			fmt.Fprintf(cmd.OutOrStdout(), "OK ensured %s\n", id)
		}
		for _, s := range skipped {
			fmt.Fprintf(cmd.OutOrStdout(), "skip %s\n", s)
		}
		return nil
	}
	name := args[0]
	e, ok := registry.ByLanguage(name)
	if !ok {
		e, ok = registry.ByID(name)
	}
	if !ok {
		return fmt.Errorf("unknown language/id %q — see orchestra lsp list", name)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "ensuring %s (%s)…\n", e.ID, e.Version)
	if err := provision.Ensure(cmd.Context(), e.ID); err != nil {
		return err
	}
	res, err := provision.Resolve(append([]string{e.BinaryName}, e.DefaultArgs...))
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "OK %s (%s)\n", res.Command[0], res.Source)
	return nil
}
