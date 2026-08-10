package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/orchestra/orchestra/internal/sessionfile"
	"github.com/orchestra/orchestra/internal/sessionstore"
	"github.com/spf13/cobra"
)

var sessionCmd = &cobra.Command{
	Use:   "session",
	Short: "Export, import, and list saved chat sessions",
	Long:  "Sessions live under .orchestra/sessions/*.json (v4 schema). Export produces a portable bundle for backup or transfer.",
}

var (
	sessionExportOut string
	sessionImportID  string
	sessionImportForce bool
)

var sessionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List saved sessions in the current project",
	Args:  cobra.NoArgs,
	RunE:  runSessionList,
}

var sessionExportCmd = &cobra.Command{
	Use:   "export <session-id>",
	Short: "Export a session to a portable JSON file",
	Args:  cobra.ExactArgs(1),
	RunE:  runSessionExport,
}

var sessionImportCmd = &cobra.Command{
	Use:   "import <file>",
	Short: "Import a session from export bundle or raw snapshot JSON",
	Args:  cobra.ExactArgs(1),
	RunE:  runSessionImport,
}

func init() {
	sessionExportCmd.Flags().StringVarP(&sessionExportOut, "out", "o", "", "Output file (default: <id>.session.json or stdout when -)")
	sessionImportCmd.Flags().StringVar(&sessionImportID, "id", "", "Override session id on import")
	sessionImportCmd.Flags().BoolVar(&sessionImportForce, "force", false, "Overwrite existing session with same id")
	sessionCmd.AddCommand(sessionListCmd)
	sessionCmd.AddCommand(sessionExportCmd)
	sessionCmd.AddCommand(sessionImportCmd)
	rootCmd.AddCommand(sessionCmd)
}

func runSessionList(cmd *cobra.Command, args []string) error {
	cfg, err := loadProjectConfig()
	if err != nil {
		return err
	}
	meta, err := sessionstore.List(cfg.ProjectRoot)
	if err != nil {
		return err
	}
	if len(meta) == 0 {
		fmt.Println("No sessions in .orchestra/sessions/")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tTITLE\tMSGS\tUPDATED")
	for _, m := range meta {
		title := strings.ReplaceAll(m.Title, "\n", " ")
		if len(title) > 48 {
			title = title[:47] + "…"
		}
		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n",
			m.ID, title, m.MsgCount, m.UpdatedAt.UTC().Format(time.RFC3339))
	}
	tw.Flush()
	return nil
}

func runSessionExport(cmd *cobra.Command, args []string) error {
	cfg, err := loadProjectConfig()
	if err != nil {
		return err
	}
	id := strings.TrimSpace(args[0])
	data, err := sessionfile.Export(cfg.ProjectRoot, id)
	if err != nil {
		return err
	}

	outPath := strings.TrimSpace(sessionExportOut)
	if outPath == "" {
		outPath = id + ".session.json"
	}
	if outPath == "-" {
		_, err = os.Stdout.Write(data)
		return err
	}
	if err := os.WriteFile(outPath, data, 0o600); err != nil {
		return fmt.Errorf("write export: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Exported session %q → %s (%d bytes)\n", id, outPath, len(data))
	return nil
}

func runSessionImport(cmd *cobra.Command, args []string) error {
	cfg, err := loadProjectConfig()
	if err != nil {
		return err
	}
	path := args[0]
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read import file: %w", err)
	}

	id, err := sessionfile.Import(cfg.ProjectRoot, data, sessionfile.ImportOptions{
		ID:    sessionImportID,
		Force: sessionImportForce,
	})
	if err != nil {
		return err
	}
	abs := filepath.Join(cfg.ProjectRoot, ".orchestra", "sessions", id+".json")
	fmt.Printf("Imported session %q → %s\n", id, abs)
	fmt.Fprintln(os.Stderr, "Resume in TUI: pick session from list or session.start via RPC.")
	return nil
}
