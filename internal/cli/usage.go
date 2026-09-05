package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/usage"
	"github.com/spf13/cobra"
)

var (
	usageLast int
	usageJSON bool
)

var usageCmd = &cobra.Command{
	Use:   "usage",
	Short: "Show token usage and cost summary from .orchestra/usage.jsonl",
	Long: `Reads .orchestra/usage.jsonl (written by every apply / session.message run)
and prints aggregate totals plus a per-model breakdown.

By default aggregates every recorded run. Use --last N to limit to the most
recent N records. --json emits the parsed records and aggregate as JSON.`,
	RunE: runUsage,
}

func init() {
	usageCmd.Flags().IntVar(&usageLast, "last", 0, "Only aggregate the N most recent runs (0 = all)")
	usageCmd.Flags().BoolVar(&usageJSON, "json", false, "Emit JSON instead of a table")
	rootCmd.AddCommand(usageCmd)
}

func runUsage(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	cfg, err := config.Load(filepath.Join(cwd, ".orchestra.yml"))
	if err != nil {
		return fmt.Errorf("failed to load config: %w (run 'orchestra init' first)", err)
	}

	records, err := usage.Load(cfg.ProjectRoot)
	if err != nil {
		return err
	}
	if len(records) == 0 {
		fmt.Println("No usage records yet. Run `orchestra apply` to start recording.")
		return nil
	}

	if usageLast > 0 && usageLast < len(records) {
		records = records[len(records)-usageLast:]
	}

	perModel, totals := usage.Aggregate(records)

	if usageJSON {
		payload := struct {
			Records []usage.Record `json:"records"`
			PerModel []usage.Entry `json:"per_model"`
			Totals  usage.Entry   `json:"totals"`
		}{records, perModel, totals}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(payload)
	}

	fmt.Printf("Aggregated across %d run(s):\n\n", len(records))

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PROVIDER\tMODEL\tCALLS\tIN\tCACHED\tOUT\tTOTAL\tCOST")
	for _, e := range perModel {
		fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%s\t%d\t%d\t%s\n",
			e.Provider, e.Model, e.Calls,
			e.PromptTokens, formatCached(e.CachedPromptTokens, e.PromptTokens),
			e.CompletionTokens, e.TotalTokens,
			formatCost(e.CostUSD))
	}
	fmt.Fprintln(w, strings.Repeat("-", 8)+"\t"+strings.Repeat("-", 8)+"\t-----\t------\t------\t------\t------\t------")
	fmt.Fprintf(w, "TOTAL\t\t%d\t%d\t%s\t%d\t%d\t%s\n",
		totals.Calls, totals.PromptTokens, formatCached(totals.CachedPromptTokens, totals.PromptTokens),
		totals.CompletionTokens, totals.TotalTokens, formatCost(totals.CostUSD))
	return w.Flush()
}

// formatCached renders the prompt-cache hit as "tokens (pct%)" so a run that
// paid full price for its whole history is visible at a glance; local models
// never report a cache and get a dash.
func formatCached(cached, prompt int) string {
	if cached == 0 {
		return "—"
	}
	if prompt <= 0 {
		return fmt.Sprintf("%d", cached)
	}
	return fmt.Sprintf("%d (%d%%)", cached, cached*100/prompt)
}

func formatCost(c float64) string {
	if c == 0 {
		return "—"
	}
	return fmt.Sprintf("$%.4f", c)
}
