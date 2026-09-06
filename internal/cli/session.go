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

	sessionSearchInsensitive bool
	sessionSearchAll         bool
	sessionSearchLimit       int
	sessionForkAt            int
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

var sessionSearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search message text across saved sessions",
	Long: `Searches the text of every message in every session of this project and
prints one line per matching message, with the message index that
'orchestra session fork --at' and the TUI checkpoint picker both use.

By default only user and assistant message text is searched; --all adds
reasoning and tool blocks, which are much larger and noisier.`,
	Args: cobra.ExactArgs(1),
	RunE: runSessionSearch,
}

var sessionForkCmd = &cobra.Command{
	Use:   "fork <session-id>",
	Short: "Branch a session at a checkpoint, keeping the original intact",
	Long: `Creates a new session containing this session's history up to, but not
including, the message at --at, which must be a user message. The original
session is left exactly as it was — unlike rewind, nothing is destroyed.

Use 'orchestra session search' to find the index to branch at. This reads the
session file on disk, so a session currently open elsewhere may be missing its
most recent few seconds.`,
	Args: cobra.ExactArgs(1),
	RunE: runSessionFork,
}

func init() {
	sessionExportCmd.Flags().StringVarP(&sessionExportOut, "out", "o", "", "Output file (default: <id>.session.json or stdout when -)")
	sessionImportCmd.Flags().StringVar(&sessionImportID, "id", "", "Override session id on import")
	sessionImportCmd.Flags().BoolVar(&sessionImportForce, "force", false, "Overwrite existing session with same id")
	sessionSearchCmd.Flags().BoolVarP(&sessionSearchInsensitive, "insensitive", "i", false, "Case-insensitive search")
	sessionSearchCmd.Flags().BoolVar(&sessionSearchAll, "all", false, "Also search reasoning and tool blocks")
	sessionSearchCmd.Flags().IntVar(&sessionSearchLimit, "limit", 0, "Maximum number of matches (0 = no limit)")
	sessionForkCmd.Flags().IntVar(&sessionForkAt, "at", -1, "Index of the user message to branch at (see 'session search')")
	sessionCmd.AddCommand(sessionListCmd)
	sessionCmd.AddCommand(sessionExportCmd)
	sessionCmd.AddCommand(sessionImportCmd)
	sessionCmd.AddCommand(sessionSearchCmd)
	sessionCmd.AddCommand(sessionForkCmd)
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

func runSessionSearch(cmd *cobra.Command, args []string) error {
	cfg, err := loadProjectConfig()
	if err != nil {
		return err
	}
	hits, err := sessionfile.Search(cfg.ProjectRoot, sessionfile.SearchOptions{
		Query:       args[0],
		Insensitive: sessionSearchInsensitive,
		IncludeAll:  sessionSearchAll,
		Limit:       sessionSearchLimit,
	})
	if err != nil {
		return err
	}
	if len(hits) == 0 {
		fmt.Println("No matches.")
		return nil
	}

	currentSession := ""
	for _, h := range hits {
		if h.SessionID != currentSession {
			// The blank line separates sessions, so it belongs before every
			// header except the first — otherwise the output opens on it.
			if currentSession != "" {
				fmt.Println()
			}
			currentSession = h.SessionID
			title := strings.ReplaceAll(h.Title, "\n", " ")
			fmt.Printf("%s  %s  (%s)\n", h.SessionID, title, h.UpdatedAt.UTC().Format(time.RFC3339))
		}
		fmt.Printf("  #%-4d %-9s %s\n", h.Index, h.Role, h.Snippet)
	}
	return nil
}

func runSessionFork(cmd *cobra.Command, args []string) error {
	cfg, err := loadProjectConfig()
	if err != nil {
		return err
	}
	if sessionForkAt < 0 {
		return fmt.Errorf("--at is required: pass the index of the user message to branch at (see 'orchestra session search')")
	}
	id := strings.TrimSpace(args[0])
	snap, err := sessionfile.Load(cfg.ProjectRoot, id)
	if err != nil {
		return err
	}
	branch, err := sessionfile.ForkSnapshot(snap, sessionForkAt, sessionfile.NewID())
	if err != nil {
		return err
	}
	if err := sessionfile.Save(cfg.ProjectRoot, branch); err != nil {
		return err
	}
	fmt.Println(branch.ID)
	fmt.Fprintf(os.Stderr, "Forked %q at message %d → %s (%d messages)\n",
		id, sessionForkAt, branch.ID, len(branch.UIMessages))
	return nil
}
