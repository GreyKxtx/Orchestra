package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/memory"
	"github.com/spf13/cobra"
)

var memoryCmd = &cobra.Command{
	Use:   "memory",
	Short: "Inspect project memory",
}

var memoryStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show memory-write activity from .orchestra/llm_log.jsonl",
	Long: `Reads memory.note events from .orchestra/llm_log.jsonl (written by every
session.message turn since auto_summary_memory was made observable) and
reports how many turns wrote a durable fact, skipped (nothing changed), or
failed, and whether written notes came from the model or the rule-based
turn digest.

This is the number the field run could not produce: one note across 52
sessions, and no artifact said whether memory had tried and failed, or
never tried at all.`,
	RunE: runMemoryStats,
}

func init() {
	memoryCmd.AddCommand(memoryStatsCmd)
	rootCmd.AddCommand(memoryCmd)
}

func runMemoryStats(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	cfg, err := config.Load(filepath.Join(cwd, ".orchestra.yml"))
	if err != nil {
		return fmt.Errorf("failed to load config: %w (run 'orchestra init' first)", err)
	}
	logPath := filepath.Join(cfg.ProjectRoot, ".orchestra", "llm_log.jsonl")
	stats, err := memory.ParseNoteStats(logPath)
	if err != nil {
		return err
	}
	fmt.Println(formatMemoryStats(stats))
	return nil
}

// formatMemoryStats renders NoteStats for the terminal. Pure and separate
// from runMemoryStats so the message can be tested without capturing stdout.
func formatMemoryStats(stats memory.NoteStats) string {
	if stats.Total() == 0 {
		return "No memory.note events yet. Run a turn — auto_summary_memory is enabled by default."
	}
	return fmt.Sprintf(
		"written: %d (model: %d, digest: %d)\nskipped: %d  (turn changed nothing worth remembering)\nfailed:  %d",
		stats.Written, stats.FromModel, stats.FromDigest, stats.Skipped, stats.Failed,
	)
}
