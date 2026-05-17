package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/mcp"
	"github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Inspect MCP (Model Context Protocol) servers",
	Long: `Commands for inspecting MCP server configurations.

MCP servers are configured in .orchestra.yml:

  mcp:
    servers:
      - name: myserver
        command: ["npx", "-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
        env:
          API_KEY: "${MY_API_KEY}"
      - name: other-server
        command: ["./bin/my-mcp-server"]
        disabled: true
`,
}

var mcpListToolsCmd = &cobra.Command{
	Use:   "list-tools",
	Short: "List all tools from configured MCP servers",
	Long:  "Connect to all enabled MCP servers and list their available tools.",
	RunE:  runMCPListTools,
}

func init() {
	mcpCmd.AddCommand(mcpListToolsCmd)
	rootCmd.AddCommand(mcpCmd)
}

func runMCPListTools(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	cfg, err := config.Load(filepath.Join(cwd, ".orchestra.yml"))
	if err != nil {
		return fmt.Errorf("config: %w (run 'orchestra init' first)", err)
	}
	return printMCPTools(cmd.OutOrStdout(), cfg.MCP)
}

func printMCPTools(w io.Writer, cfg config.MCPConfig) error {
	if len(cfg.Servers) == 0 {
		fmt.Fprintln(w, "No MCP servers configured in .orchestra.yml")
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "Add servers under the 'mcp:' section:")
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "  mcp:")
		fmt.Fprintln(w, "    servers:")
		fmt.Fprintln(w, "      - name: myserver")
		fmt.Fprintln(w, `        command: ["npx", "-y", "@modelcontextprotocol/server-filesystem", "/tmp"]`)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	mgr, errs := mcp.NewManager(ctx, cfg)
	defer mgr.Close()

	// Group tools by server name (parsed from "mcp:<server>:<tool>").
	byServer := make(map[string][]string)
	for _, def := range mgr.ListToolDefs() {
		parts := strings.SplitN(def.Function.Name, ":", 3)
		if len(parts) != 3 {
			continue
		}
		serverName := parts[1]
		line := fmt.Sprintf("  %-42s %s", def.Function.Name, def.Function.Description)
		byServer[serverName] = append(byServer[serverName], line)
	}

	// Print servers in config order.
	for _, srv := range cfg.Servers {
		if srv.Disabled || len(srv.Command) == 0 {
			fmt.Fprintf(w, "▷ %s [disabled]\n\n", srv.Name)
			continue
		}
		tools := byServer[srv.Name]
		plural := "s"
		if len(tools) == 1 {
			plural = ""
		}
		fmt.Fprintf(w, "▶ %s (%d tool%s)\n", srv.Name, len(tools), plural)
		for _, line := range tools {
			fmt.Fprintln(w, line)
		}
		fmt.Fprintln(w, "")
	}

	for _, e := range errs {
		fmt.Fprintf(w, "Warning: %v\n", e)
	}
	return nil
}
