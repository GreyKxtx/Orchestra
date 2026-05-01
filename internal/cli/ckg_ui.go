package cli

import (
	"context"
	"log"
	"path/filepath"

	"github.com/orchestra/orchestra/internal/ckg"
	"github.com/spf13/cobra"
)

var port int

var ckgUiCmd = &cobra.Command{
	Use:   "ckg-ui",
	Short: "Запустить интерактивный визуализатор Code Knowledge Graph в браузере",
	RunE: func(cmd *cobra.Command, args []string) error {
		workspaceRoot := "." // For now, run from current dir
		absRoot, err := filepath.Abs(workspaceRoot)
		if err != nil {
			return err
		}

		dbPath := filepath.Join(absRoot, ".orchestra", "ckg.db")
		store, err := ckg.NewStore("file:" + dbPath + "?cache=shared")
		if err != nil {
			return err
		}
		defer store.Close()

		// Update graph before starting UI so it's fresh
		orch := ckg.NewOrchestrator(store, absRoot)
		if err := orch.UpdateGraph(context.Background()); err != nil {
			log.Printf("Warning: failed to update graph: %v", err)
		}

		return ckg.StartUIServer(store, absRoot, port)
	},
}

func init() {
	ckgUiCmd.Flags().IntVarP(&port, "port", "p", 6061, "Port to run the UI server on")
	rootCmd.AddCommand(ckgUiCmd)
}
