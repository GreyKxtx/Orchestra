package cli

import (
	"github.com/orchestra/orchestra/internal/llm"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "orchestra",
	Short: "Orchestra - LLM orchestrator for codebase",
	Long:  "Orchestra is a service orchestrator for working with LLM on project codebase.",
}

// testLLMClient is used for dependency injection in tests
var testLLMClient llm.Client

// SetTestClient sets a mock LLM client for testing
// This should only be called from tests
func SetTestClient(client llm.Client) {
	testLLMClient = client
}

// ResetTestClient resets the test client (for cleanup after tests)
func ResetTestClient() {
	testLLMClient = nil
}

// GetRootCmd returns the root command (for testing)
func GetRootCmd() *cobra.Command {
	return rootCmd
}

// Execute runs the CLI
func Execute() error {
	return rootCmd.Execute()
}
