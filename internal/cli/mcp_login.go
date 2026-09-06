package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/mcpauth"
)

var mcpLoginCmd = &cobra.Command{
	Use:   "login <server>",
	Short: "Authorize Orchestra against an MCP server that requires OAuth",
	Long: `Runs the OAuth 2.1 authorization-code-with-PKCE flow for a server
configured with an oauth: block in .orchestra.yml, opening your browser and
listening on a loopback port for the redirect. The resulting token is
stored under ~/.orchestra/mcp-oauth/<server>.json and reused (and silently
refreshed) by every later run — this command is the only place Orchestra
ever opens a browser.

Over SSH or another headless session, forward the printed URL's port with
'ssh -L <port>:localhost:<port>' before running this command, or run it on
the machine with the browser.`,
	Args: cobra.ExactArgs(1),
	RunE: runMCPLogin,
}

var mcpLogoutCmd = &cobra.Command{
	Use:   "logout <server>",
	Short: "Remove a stored OAuth token for an MCP server",
	Args:  cobra.ExactArgs(1),
	RunE:  runMCPLogout,
}

func init() {
	mcpCmd.AddCommand(mcpLoginCmd)
	mcpCmd.AddCommand(mcpLogoutCmd)
}

// findServerInConfig looks up one server by name. Split out from
// findMCPServerConfig so tests can exercise the lookup itself without
// touching the filesystem.
func findServerInConfig(cfg config.MCPConfig, name string) (config.MCPServerConfig, error) {
	for _, srv := range cfg.Servers {
		if srv.Name == name {
			return srv, nil
		}
	}
	return config.MCPServerConfig{}, fmt.Errorf("mcp server %q is not configured in .orchestra.yml", name)
}

func findMCPServerConfig(name string) (config.MCPServerConfig, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return config.MCPServerConfig{}, err
	}
	cfg, err := config.Load(filepath.Join(cwd, ".orchestra.yml"))
	if err != nil {
		return config.MCPServerConfig{}, fmt.Errorf("config: %w (run 'orchestra init' first)", err)
	}
	return findServerInConfig(cfg.MCP, name)
}

func runMCPLogin(cmd *cobra.Command, args []string) error {
	name := args[0]
	srv, err := findMCPServerConfig(name)
	if err != nil {
		return err
	}
	if srv.OAuth == nil {
		return fmt.Errorf("mcp server %q has no oauth: block in .orchestra.yml", name)
	}
	if srv.URL == "" {
		return fmt.Errorf("mcp server %q: oauth requires url (a remote server)", name)
	}

	var clientSecret string
	if env := srv.OAuth.ClientSecretEnv; env != "" {
		clientSecret = os.Getenv(env)
		if clientSecret == "" {
			return fmt.Errorf("mcp server %q: client_secret_env %q is empty or unset", name, env)
		}
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Opening your browser to authorize %q...\n", name)
	ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Minute)
	defer cancel()
	if err := mcpauth.Login(ctx, mcpauth.LoginConfig{
		ServerName:   name,
		ServerURL:    srv.URL,
		ClientID:     srv.OAuth.ClientID,
		ClientSecret: clientSecret,
		Scopes:       srv.OAuth.Scopes,
	}); err != nil {
		return fmt.Errorf("mcp login %q: %w", name, err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Authorized %q.\n", name)
	return nil
}

func runMCPLogout(cmd *cobra.Command, args []string) error {
	name := args[0]
	if err := mcpauth.Logout(name); err != nil {
		return fmt.Errorf("mcp logout %q: %w", name, err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Logged out of %q.\n", name)
	return nil
}
