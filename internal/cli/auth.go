package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/spf13/cobra"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage provider API keys in .orchestra.yml",
}

var authListCmd = &cobra.Command{
	Use:   "list",
	Short: "List configured providers (keys redacted)",
	RunE:  runAuthList,
}

var (
	authSetKeyProvider string
	authSetKeyValue    string
)

var authSetKeyCmd = &cobra.Command{
	Use:   "set-key [provider]",
	Short: "Set api_key for a named provider",
	Args:  cobra.ExactArgs(1),
	RunE:  runAuthSetKey,
}

func init() {
	authSetKeyCmd.Flags().StringVar(&authSetKeyValue, "key", "", "API key value (or set ORCH_API_KEY env for main llm)")
	authCmd.AddCommand(authListCmd)
	authCmd.AddCommand(authSetKeyCmd)
	rootCmd.AddCommand(authCmd)
}

func runAuthList(cmd *cobra.Command, args []string) error {
	cfg, err := loadProjectConfig()
	if err != nil {
		return err
	}
	fmt.Printf("Main LLM: model=%s api_base=%s key=%s\n",
		cfg.LLM.Model, cfg.LLM.APIBase, redactKey(cfg.LLM.APIKey))
	if len(cfg.Providers) == 0 {
		fmt.Println("providers: (none)")
		return nil
	}
	names := make([]string, 0, len(cfg.Providers))
	for n := range cfg.Providers {
		names = append(names, n)
	}
	sort.Strings(names)
	fmt.Println("providers:")
	for _, n := range names {
		p := cfg.Providers[n]
		fmt.Printf("  %s: model=%s api_base=%s key=%s\n", n, p.Model, p.APIBase, redactKey(p.APIKey))
	}
	return nil
}

func runAuthSetKey(cmd *cobra.Command, args []string) error {
	name := strings.TrimSpace(args[0])
	key := strings.TrimSpace(authSetKeyValue)
	if key == "" {
		key = strings.TrimSpace(os.Getenv("ORCH_API_KEY"))
	}
	if key == "" {
		return fmt.Errorf("provide --key or set ORCH_API_KEY")
	}

	cfg, err := loadProjectConfig()
	if err != nil {
		return err
	}
	cfgPath := filepath.Join(cfg.ProjectRoot, ".orchestra.yml")

	if name == "default" || name == "llm" {
		cfg.LLM.APIKey = key
	} else {
		if cfg.Providers == nil {
			cfg.Providers = map[string]config.LLMConfig{}
		}
		p, ok := cfg.Providers[name]
		if !ok {
			return fmt.Errorf("provider %q not found; add it under providers: in .orchestra.yml first", name)
		}
		p.APIKey = key
		cfg.Providers[name] = p
	}

	if err := config.Save(cfgPath, cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	fmt.Printf("Updated api_key for %q in %s\n", name, cfgPath)
	return nil
}

func redactKey(k string) string {
	k = strings.TrimSpace(k)
	if k == "" {
		return "(empty)"
	}
	if len(k) <= 4 {
		return "****"
	}
	return k[:2] + "…" + k[len(k)-2:]
}
