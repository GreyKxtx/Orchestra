package cli

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/orchestra/orchestra/internal/ckg"
	"github.com/spf13/cobra"
)

var runtimeCmd = &cobra.Command{
	Use:   "runtime",
	Short: "Runtime Observability Bridge — управление трейсами и OTel-данными",
}

var runtimeIngestCmd = &cobra.Command{
	Use:   "ingest <file>",
	Short: "Загрузить OTel JSON-трейс в CKG и связать spans с узлами графа",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		traceFile := args[0]

		data, err := os.ReadFile(traceFile)
		if err != nil {
			return fmt.Errorf("read trace file: %w", err)
		}

		workspaceRoot := "."
		absRoot, err := filepath.Abs(workspaceRoot)
		if err != nil {
			return err
		}

		ctx := context.Background()

		dbPath := filepath.Join(absRoot, ".orchestra", "ckg.db")
		store, err := ckg.NewStore("file:" + dbPath + "?cache=shared")
		if err != nil {
			return fmt.Errorf("open CKG store: %w", err)
		}
		defer store.Close()

		orch := ckg.NewOrchestrator(store, absRoot)
		if err := orch.UpdateGraph(ctx); err != nil {
			log.Printf("Warning: failed to update CKG graph: %v", err)
		}

		traces, err := ckg.ParseOTLPJSON(data, absRoot)
		if err != nil {
			return fmt.Errorf("parse OTLP JSON: %w", err)
		}
		if len(traces) == 0 {
			fmt.Println("No traces found in file.")
			return nil
		}

		totalSpans := 0
		for _, td := range traces {
			if err := store.IngestTrace(ctx, td); err != nil {
				return fmt.Errorf("ingest trace %s: %w", td.TraceID, err)
			}
			totalSpans += len(td.Spans)
			fmt.Printf("trace %-32s  service=%-20s  spans=%d\n",
				td.TraceID, td.Service, len(td.Spans))
		}
		fmt.Printf("Ingested %d trace(s), %d span(s) total.\n", len(traces), totalSpans)
		return nil
	},
}

var (
	servePort          int
	serveWorkspaceRoot string
)

var runtimeServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Запустить OTLP/HTTP приёмник трейсов (порт 4318 по умолчанию)",
	RunE: func(cmd *cobra.Command, args []string) error {
		absRoot, err := filepath.Abs(serveWorkspaceRoot)
		if err != nil {
			return err
		}

		dbPath := filepath.Join(absRoot, ".orchestra", "ckg.db")
		if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
			return fmt.Errorf("create .orchestra dir: %w", err)
		}

		store, err := ckg.NewStore("file:" + dbPath + "?cache=shared")
		if err != nil {
			return fmt.Errorf("open CKG store: %w", err)
		}
		defer store.Close()

		addr := net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", servePort))
		srv := ckg.NewOTLPServer(store, absRoot)

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		errCh := make(chan error, 1)
		go func() {
			errCh <- srv.ListenAndServe(addr)
		}()

		select {
		case <-ctx.Done():
			log.Println("[otlp] shutting down…")
			shutCtx, cancel := context.WithTimeout(context.Background(), 5*1e9) // 5s
			defer cancel()
			return srv.Shutdown(shutCtx)
		case err := <-errCh:
			return err
		}
	},
}

func init() {
	runtimeServeCmd.Flags().IntVar(&servePort, "port", 4318, "порт OTLP/HTTP приёмника")
	runtimeServeCmd.Flags().StringVar(&serveWorkspaceRoot, "workspace-root", ".", "корень проекта (должен содержать .orchestra/)")

	runtimeCmd.AddCommand(runtimeIngestCmd)
	runtimeCmd.AddCommand(runtimeServeCmd)
	rootCmd.AddCommand(runtimeCmd)
}
