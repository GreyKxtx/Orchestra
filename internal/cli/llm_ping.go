package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/llm"
	"github.com/spf13/cobra"
)

var llmPingCmd = &cobra.Command{
	Use:   "llm-ping",
	Short: "Test LLM provider connectivity and contract",
	Long: `Test LLM provider connectivity by sending a minimal request.

This command verifies:
- Provider is reachable (not 400/401/500)
- Response is structurally valid (JSON with choices)
- Basic contract compliance (messages field, model, etc.)

Use this to diagnose provider issues before running full agent workflows.`,
	RunE: runLLMPing,
}

func init() {
	rootCmd.AddCommand(llmPingCmd)
}

type pingResult struct {
	Success         bool   `json:"success"`
	URL             string `json:"url"`
	Model           string `json:"model"`
	TimeoutS        int    `json:"timeout_s"`
	RequestBytes    int    `json:"request_bytes"`
	ResponseBytes   int    `json:"response_bytes,omitempty"`
	DurationMS      int64  `json:"duration_ms"`
	HTTPCode        int    `json:"http_code,omitempty"`
	ErrorMessage    string `json:"error_message,omitempty"`
	ResponsePreview string `json:"response_preview,omitempty"`
}

func runLLMPing(cmd *cobra.Command, args []string) error {
	// Find project root (current dir or parent with .orchestra.yml)
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	cfgPath := filepath.Join(wd, ".orchestra.yml")
	if _, err := os.Stat(cfgPath); err != nil {
		// Try parent directory
		parent := filepath.Dir(wd)
		cfgPath = filepath.Join(parent, ".orchestra.yml")
		if _, err := os.Stat(cfgPath); err != nil {
			return fmt.Errorf("no .orchestra.yml found in current or parent directory")
		}
		wd = parent
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Create LLM client (use test client if set, otherwise create real client)
	var llmClient llm.Client
	if testLLMClient != nil {
		llmClient = testLLMClient
	} else {
		llmClient = llm.NewOpenAIClient(cfg.LLM)
	}

	// Prepare minimal request
	req := llm.CompleteRequest{
		Messages: []llm.Message{
			{
				Role:    llm.RoleUser,
				Content: "ping",
			},
		},
		Tools: nil, // No tools for smoke test
	}

	// Measure request size
	reqJSON, _ := json.Marshal(req)
	requestBytes := len(reqJSON)

	// Execute with timeout
	ctx, cancel := context.WithTimeout(cmd.Context(), time.Duration(cfg.LLM.TimeoutS)*time.Second)
	defer cancel()

	start := time.Now()
	resp, err := llmClient.Complete(ctx, req)
	duration := time.Since(start)

	result := pingResult{
		URL:          cfg.LLM.APIBase,
		Model:        cfg.LLM.Model,
		TimeoutS:     cfg.LLM.TimeoutS,
		RequestBytes: requestBytes,
		DurationMS:   duration.Milliseconds(),
	}

	if err != nil {
		result.Success = false
		result.ErrorMessage = err.Error()

		// Try to extract HTTP code from error message (best-effort)
		// Error format: "API error (status 400): ..." or "API returned status 401: ..."
		errMsg := err.Error()
		if strings.Contains(errMsg, "status ") {
			// Extract number after "status "
			var code int
			if _, scanErr := fmt.Sscanf(errMsg, "API error (status %d)", &code); scanErr == nil {
				result.HTTPCode = code
			} else if _, scanErr := fmt.Sscanf(errMsg, "API returned status %d", &code); scanErr == nil {
				result.HTTPCode = code
			}
		}

		// Print error details
		fmt.Fprintf(os.Stderr, "❌ LLM ping failed\n")
		fmt.Fprintf(os.Stderr, "URL: %s\n", result.URL)
		fmt.Fprintf(os.Stderr, "Model: %s\n", result.Model)
		fmt.Fprintf(os.Stderr, "Error: %s\n", result.ErrorMessage)
		if result.HTTPCode != 0 {
			fmt.Fprintf(os.Stderr, "HTTP Code: %d\n", result.HTTPCode)
		}
		fmt.Fprintf(os.Stderr, "Duration: %d ms\n", result.DurationMS)

		// Save result to artifact
		savePingResult(wd, result)
		return fmt.Errorf("llm ping failed: %w", err)
	}

	// Success: validate response structure
	result.Success = true
	result.HTTPCode = 200

	// Measure response size (best-effort)
	if resp != nil && resp.Message.Content != "" {
		responseJSON, _ := json.Marshal(resp)
		result.ResponseBytes = len(responseJSON)

		// Preview (first 500 bytes)
		preview := string(responseJSON)
		if len(preview) > 500 {
			preview = preview[:500] + "...(truncated)"
		}
		result.ResponsePreview = preview
	}

	// Print success
	fmt.Printf("✅ LLM ping successful\n")
	fmt.Printf("URL: %s\n", result.URL)
	fmt.Printf("Model: %s\n", result.Model)
	fmt.Printf("Duration: %d ms\n", result.DurationMS)
	fmt.Printf("Request size: %d bytes\n", result.RequestBytes)
	if result.ResponseBytes > 0 {
		fmt.Printf("Response size: %d bytes\n", result.ResponseBytes)
	}

	// Save result to artifact
	savePingResult(wd, result)

	return nil
}

func savePingResult(projectRoot string, result pingResult) error {
	artifactsDir := filepath.Join(projectRoot, ".orchestra")
	if err := os.MkdirAll(artifactsDir, 0755); err != nil {
		return fmt.Errorf("failed to create artifacts directory: %w", err)
	}

	resultPath := filepath.Join(artifactsDir, "llm_ping_result.json")
	resultJSON, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal result: %w", err)
	}
	resultJSON = append(resultJSON, '\n')

	if err := os.WriteFile(resultPath, resultJSON, 0644); err != nil {
		return fmt.Errorf("failed to write result: %w", err)
	}

	return nil
}
