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
	Short: "Manage provider credentials: api_key, OAuth logins, token commands",
}

var authListCmd = &cobra.Command{
	Use:   "list",
	Short: "List configured providers (keys redacted)",
	RunE:  runAuthList,
}

var (
	authSetKeyValue string
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
	fmt.Printf("Main LLM: model=%s api_base=%s %s\n",
		cfg.LLM.Model, cfg.LLM.APIBase, authStatusLine("llm", cfg.LLM))
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
		fmt.Printf("  %s: model=%s api_base=%s %s\n", n, p.Model, p.APIBase, authStatusLine(n, p))
	}
	return nil
}

// runAuthSetKey stores the secret in the gitignored .orchestra.env and leaves
// a ${NAME} reference in the config.
//
// It used to write the key itself into .orchestra.yml — a file that is
// committed, shared between frontends and rewritten whenever a setting
// changes. Putting a credential there meant the next `git add` published it.
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

	// Reuse the variable the config already names, so setting a key twice
	// does not leave two variables for one field. The field itself no longer
	// shows it — Load resolved ${NAME} before anyone got here — so ask the
	// config what the file said.
	field := "llm.api_key"
	current := cfg.LLM.APIKey
	if name != "default" && name != "llm" {
		p, ok := cfg.Providers[name]
		if !ok {
			return fmt.Errorf("provider %q not found; add it under providers: in .orchestra.yml first", name)
		}
		field, current = "providers."+name+".api_key", p.APIKey
	}
	envName := cfg.EnvVarFor(field)
	if envName == "" {
		envName = config.EnvRefName(current)
	}
	if envName == "" {
		envName = envVarNameFor(name)
	}
	ref := "${" + envName + "}"
	// The file already points at this variable: only the secret needs writing.
	alreadyReferenced := cfg.EnvVarFor(field) == envName

	if err := config.UpsertEnvVar(cfg.ProjectRoot, envName, key); err != nil {
		return err
	}

	// The config keeps the reference, never the key. Writing it is still
	// needed the first time: until the field says ${NAME}, nothing reads the
	// variable.
	if !alreadyReferenced {
		if name == "default" || name == "llm" {
			cfg.LLM.APIKey = ref
		} else {
			p := cfg.Providers[name]
			p.APIKey = ref
			cfg.Providers[name] = p
		}
		if err := config.Save(cfgPath, cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
	}

	fmt.Printf("Stored the key for %q as %s in %s\n", name, envName, filepath.Join(cfg.ProjectRoot, config.EnvFileName))
	fmt.Printf("%s refers to it as %s — the key itself is not in the config.\n", cfgPath, ref)
	return nil
}

// envVarNameFor is the variable a provider's key goes into when the config
// does not already name one: "gemini" → GEMINI_API_KEY.
func envVarNameFor(provider string) string {
	if provider == "default" || provider == "llm" {
		return "ORCHESTRA_API_KEY"
	}
	var b strings.Builder
	for _, r := range strings.ToUpper(provider) {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String() + "_API_KEY"
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
