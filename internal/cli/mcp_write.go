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
	mcpAddURL          string
	mcpAddBearerEnv    string
	mcpAddHeaders      []string
	mcpAddEnv          []string
	mcpAddCallTimeoutS int
	mcpAddDisabled     bool
)

var mcpAddCmd = &cobra.Command{
	Use:   "add <name> [-- command args...]",
	Short: "Add or replace an MCP server in .orchestra.yml",
	Long: `Configures a stdio server (command after --) or a remote one (--url).
Replaces any existing server with the same name.

Examples:
  orchestra mcp add filesystem -- npx -y @modelcontextprotocol/server-filesystem /tmp
  orchestra mcp add linear --url https://mcp.linear.app/sse --bearer-env LINEAR_TOKEN
`,
	Args: cobra.MinimumNArgs(1),
	RunE: runMCPAdd,
}

var mcpRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove an MCP server from .orchestra.yml",
	Args:  cobra.ExactArgs(1),
	RunE:  runMCPRemove,
}

var mcpGetCmd = &cobra.Command{
	Use:   "get <name>",
	Short: "Show one MCP server's resolved configuration",
	Args:  cobra.ExactArgs(1),
	RunE:  runMCPGet,
}

func init() {
	mcpAddCmd.Flags().StringVar(&mcpAddURL, "url", "", "Remote server endpoint (Streamable HTTP) instead of a stdio command")
	mcpAddCmd.Flags().StringVar(&mcpAddBearerEnv, "bearer-env", "", "Env var holding the bearer token sent to a remote server")
	mcpAddCmd.Flags().StringArrayVar(&mcpAddHeaders, "header", nil, "Extra request header for a remote server, KEY=VALUE (repeatable)")
	mcpAddCmd.Flags().StringArrayVar(&mcpAddEnv, "env", nil, "Environment variable for a stdio server, KEY=VALUE (repeatable)")
	mcpAddCmd.Flags().IntVar(&mcpAddCallTimeoutS, "call-timeout", 0, "Per-call timeout in seconds (0 = no limit beyond the caller's own)")
	mcpAddCmd.Flags().BoolVar(&mcpAddDisabled, "disabled", false, "Add the server disabled")

	mcpCmd.AddCommand(mcpAddCmd)
	mcpCmd.AddCommand(mcpRemoveCmd)
	mcpCmd.AddCommand(mcpGetCmd)
}

// splitMCPAddArgs separates the server name from the stdio command in `mcp
// add <name> -- <command> [args...]`. dash is cobra's ArgsLenAtDash() — the
// index of "--" among args, or -1 when there was none.
func splitMCPAddArgs(args []string, dash int) (name string, command []string, err error) {
	nameArgs := args
	if dash >= 0 {
		nameArgs = args[:dash]
		command = args[dash:]
	}
	if len(nameArgs) != 1 {
		return "", nil, fmt.Errorf("usage: orchestra mcp add <name> [-- command args...]")
	}
	return nameArgs[0], command, nil
}

// parseKeyValuePairs turns repeated "KEY=VALUE" flag values into a map, or
// an error naming the malformed entry. Returns nil (not empty) for no
// pairs, matching the omitempty on MCPServerConfig.Headers/Env.
func parseKeyValuePairs(pairs []string) (map[string]string, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(pairs))
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok || strings.TrimSpace(k) == "" {
			return nil, fmt.Errorf("expected KEY=VALUE, got %q", p)
		}
		out[strings.TrimSpace(k)] = v
	}
	return out, nil
}

// mcpAddServerFromArgs builds the MCPServerConfig for `mcp add` from its
// parsed flags and positional args. Pure — no cobra, no I/O — so the
// argument handling is testable on its own.
func mcpAddServerFromArgs(name string, command []string, url, bearerEnv string, headers, env []string, callTimeoutS int, disabled bool) (config.MCPServerConfig, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return config.MCPServerConfig{}, fmt.Errorf("server name is required")
	}
	hdrMap, err := parseKeyValuePairs(headers)
	if err != nil {
		return config.MCPServerConfig{}, fmt.Errorf("--header: %w", err)
	}
	envMap, err := parseKeyValuePairs(env)
	if err != nil {
		return config.MCPServerConfig{}, fmt.Errorf("--env: %w", err)
	}
	return config.MCPServerConfig{
		Name:           name,
		Command:        command,
		URL:            strings.TrimSpace(url),
		BearerTokenEnv: strings.TrimSpace(bearerEnv),
		Headers:        hdrMap,
		Env:            envMap,
		CallTimeoutS:   callTimeoutS,
		Disabled:       disabled,
	}, nil
}

// formatMCPServer renders one server for `mcp get`. Pure, so the message is
// tested without capturing stdout.
func formatMCPServer(s config.MCPServerConfig) string {
	var b strings.Builder
	fmt.Fprintf(&b, "name: %s\n", s.Name)
	if len(s.Command) > 0 {
		fmt.Fprintf(&b, "command: %s\n", strings.Join(s.Command, " "))
	}
	if s.URL != "" {
		fmt.Fprintf(&b, "url: %s\n", s.URL)
	}
	if s.BearerTokenEnv != "" {
		fmt.Fprintf(&b, "bearer_token_env: %s\n", s.BearerTokenEnv)
	}
	if len(s.Headers) > 0 {
		fmt.Fprintf(&b, "headers: %d\n", len(s.Headers))
	}
	if len(s.Env) > 0 {
		fmt.Fprintf(&b, "env: %d var(s)\n", len(s.Env))
	}
	if s.CallTimeoutS > 0 {
		fmt.Fprintf(&b, "call_timeout_s: %d\n", s.CallTimeoutS)
	}
	fmt.Fprintf(&b, "disabled: %v", s.Disabled)
	return b.String()
}

func mcpConfigPath() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(cwd, ".orchestra.yml"), nil
}

func runMCPAdd(cmd *cobra.Command, args []string) error {
	name, command, err := splitMCPAddArgs(args, cmd.ArgsLenAtDash())
	if err != nil {
		return err
	}
	srv, err := mcpAddServerFromArgs(name, command, mcpAddURL, mcpAddBearerEnv, mcpAddHeaders, mcpAddEnv, mcpAddCallTimeoutS, mcpAddDisabled)
	if err != nil {
		return err
	}
	cfgPath, err := mcpConfigPath()
	if err != nil {
		return err
	}
	if err := config.SetMCPServer(cfgPath, srv); err != nil {
		return fmt.Errorf("%w (run 'orchestra init' first if .orchestra.yml does not exist yet)", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Added MCP server %q to .orchestra.yml\n", srv.Name)
	return nil
}

func runMCPRemove(cmd *cobra.Command, args []string) error {
	cfgPath, err := mcpConfigPath()
	if err != nil {
		return err
	}
	removed, err := config.RemoveMCPServer(cfgPath, args[0])
	if err != nil {
		return err
	}
	if !removed {
		return fmt.Errorf("no MCP server named %q in .orchestra.yml (a server defined in .mcp.json cannot be removed here — edit that file instead)", args[0])
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Removed MCP server %q\n", args[0])
	return nil
}

func runMCPGet(cmd *cobra.Command, args []string) error {
	cfgPath, err := mcpConfigPath()
	if err != nil {
		return err
	}
	srv, ok, err := config.GetMCPServer(cfgPath, args[0])
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("no MCP server named %q", args[0])
	}
	fmt.Fprintln(cmd.OutOrStdout(), formatMCPServer(srv))
	return nil
}
