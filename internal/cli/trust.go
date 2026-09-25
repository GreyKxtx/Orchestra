package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/spf13/cobra"
)

var (
	trustRevoke bool
	trustStatus bool
)

var trustCmd = &cobra.Command{
	Use:   "trust",
	Short: "Trust this workspace's own settings (MCP servers, hooks, exec consent, endpoints)",
	Long: `Settings in .orchestra.yml, .orchestra.local.yml and .mcp.json that act on this
machine — MCP servers, hooks, lsp.servers, exec.confirm/allow, web.confirm,
permission allow rules, auth token commands, and an api_base that would receive
your key — take effect only once you trust the workspace. A clone of someone
else's repository brings its config along; nothing in it runs until you say so.

orchestra trust            record the current settings as trusted
orchestra trust --status   show what is ignored until then
orchestra trust --revoke   forget the workspace

A later change to those settings (a pull, an agent's edit) needs trusting again.
Set security.workspace_trust: off in ~/.orchestra/config.yml, or
ORCHESTRA_WORKSPACE_TRUST=off in the environment, to turn the check off.`,
	Args: cobra.NoArgs,
	RunE: runTrust,
}

func init() {
	trustCmd.Flags().BoolVar(&trustRevoke, "revoke", false, "Forget this workspace")
	trustCmd.Flags().BoolVar(&trustStatus, "status", false, "Show whether the workspace is trusted and what is ignored")
	rootCmd.AddCommand(trustCmd)
}

func runTrust(cmd *cobra.Command, _ []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	path := filepath.Join(cwd, ".orchestra.yml")
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("no .orchestra.yml in %s", cwd)
	}
	out := cmd.OutOrStdout()
	switch {
	case trustRevoke:
		if err := config.UntrustWorkspace(path); err != nil {
			return err
		}
		fmt.Fprintf(out, "Forgot %s: its MCP servers, hooks and other machine-level settings are ignored again.\n", cwd)
		return nil
	case trustStatus:
		st, err := config.WorkspaceTrustStatus(path)
		if err != nil {
			return err
		}
		printTrust(out, cwd, st)
		return nil
	}
	before, err := config.WorkspaceTrustStatus(path)
	if err != nil {
		return err
	}
	if _, err := config.TrustWorkspace(path); err != nil {
		return err
	}
	if len(before.Ignored) > 0 {
		fmt.Fprintf(out, "Trusted %s. Now in effect: %s\n", cwd, strings.Join(before.Ignored, ", "))
	} else {
		fmt.Fprintf(out, "Trusted %s.\n", cwd)
	}
	return nil
}

func printTrust(out interface{ Write([]byte) (int, error) }, dir string, st config.WorkspaceTrust) {
	switch {
	case !st.Enforced:
		fmt.Fprintf(out, "Workspace trust is off (security.workspace_trust or ORCHESTRA_WORKSPACE_TRUST).\n")
	case st.Trusted:
		fmt.Fprintf(out, "%s is trusted.\n", dir)
	default:
		fmt.Fprintf(out, "%s is not trusted. Ignored until `orchestra trust`: %s\n", dir, strings.Join(st.Ignored, ", "))
	}
}

// warnUntrusted tells a CLI run which of the workspace's settings it is
// running without.
func warnUntrusted(cfg *config.ProjectConfig) {
	if cfg == nil {
		return
	}
	if st := cfg.Trust(); len(st.Ignored) > 0 {
		fmt.Fprintf(os.Stderr, "orchestra: this workspace is not trusted — ignoring %s. Run `orchestra trust` to use them.\n",
			strings.Join(st.Ignored, ", "))
	}
}
