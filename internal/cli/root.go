package cli

import (
	"sync"

	"github.com/orchestra/orchestra/llm"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "orchestra",
	Short: "Orchestra - interactive AI coding assistant",
	Long: `Orchestra is a local AI coding assistant with a terminal UI.

Run orchestra with no subcommand to open the interactive TUI (console agent).
Use subcommands for headless workflows: apply, core, init, search, etc.`,
}

// testLLMClient is the test-only LLM client injection point used by
// apply / llm-ping / eval to swap in a scripted mock. M4 in architecture
// audit added the RWMutex so SetTestClient called from a t.Parallel
// goroutine doesn't race against the read in the command handler. The
// production code path is unaffected because testLLMClient is nil by
// default and the read happens once per command.
var (
	testLLMClientMu sync.RWMutex
	testLLMClient   llm.Client
)

// SetTestClient sets a mock LLM client for testing.
// This should only be called from tests; production code must not touch it.
func SetTestClient(client llm.Client) {
	testLLMClientMu.Lock()
	defer testLLMClientMu.Unlock()
	testLLMClient = client
}

// ResetTestClient resets the test client (for cleanup after tests).
func ResetTestClient() {
	testLLMClientMu.Lock()
	defer testLLMClientMu.Unlock()
	testLLMClient = nil
}

// getTestLLMClient reads the injected client under the lock. Returns nil
// in production (no test ever called SetTestClient).
func getTestLLMClient() llm.Client {
	testLLMClientMu.RLock()
	defer testLLMClientMu.RUnlock()
	return testLLMClient
}

// GetRootCmd returns the root command (for testing).
func GetRootCmd() *cobra.Command {
	return rootCmd
}

// Execute runs the CLI.
func Execute() error {
	return rootCmd.Execute()
}
