package e2e_real_llm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLLMContractSmoke tests that the LLM provider responds correctly to a minimal request.
//
// This test verifies:
// - Provider is reachable (not 400/401/500)
// - Response is structurally valid (JSON with choices)
// - Basic contract compliance (messages field, model, etc.)
//
// This is a critical smoke test to catch provider configuration issues early.
func TestLLMContractSmoke(t *testing.T) {
	requireE2ELLM(t)

	projectDir := setupTestProject(t)

	// Run llm-ping command
	stdout, stderr, exitCode := runOrchestra(t, projectDir, "llm-ping")

	combined := stdout + "\n" + stderr

	if exitCode != 0 {
		t.Fatalf("llm-ping failed (exit %d):\n%s", exitCode, combined)
	}

	// Check for success indicators
	if !strings.Contains(combined, "✅ LLM ping successful") {
		t.Fatalf("llm-ping did not report success:\n%s", combined)
	}

	// Verify artifact was created
	resultPath := filepath.Join(projectDir, ".orchestra", "llm_ping_result.json")
	resultData, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatalf("llm_ping_result.json not created: %v", err)
	}

	var result struct {
		Success       bool   `json:"success"`
		URL           string `json:"url"`
		Model         string `json:"model"`
		TimeoutS      int    `json:"timeout_s"`
		RequestBytes  int    `json:"request_bytes"`
		ResponseBytes int    `json:"response_bytes,omitempty"`
		DurationMS    int64  `json:"duration_ms"`
		HTTPCode      int    `json:"http_code,omitempty"`
		ErrorMessage  string `json:"error_message,omitempty"`
	}

	if err := json.Unmarshal(resultData, &result); err != nil {
		t.Fatalf("llm_ping_result.json is not valid JSON: %v", err)
	}

	// Validate result structure
	if !result.Success {
		t.Fatalf("llm-ping result indicates failure: %s", result.ErrorMessage)
	}

	if result.URL == "" {
		t.Fatalf("llm-ping result missing URL")
	}

	if result.Model == "" {
		t.Fatalf("llm-ping result missing model")
	}

	if result.DurationMS <= 0 {
		t.Fatalf("llm-ping result has invalid duration: %d ms", result.DurationMS)
	}

	if result.RequestBytes <= 0 {
		t.Fatalf("llm-ping result has invalid request size: %d bytes", result.RequestBytes)
	}

	// HTTP code should be 200 for success
	if result.HTTPCode != 0 && result.HTTPCode != 200 {
		t.Fatalf("llm-ping succeeded but HTTP code is not 200: %d", result.HTTPCode)
	}

	// Duration should be reasonable (< 30 seconds for smoke test)
	if result.DurationMS > 30000 {
		t.Logf("WARNING: llm-ping took %d ms (> 30s), provider may be slow", result.DurationMS)
	}

	t.Logf("✓ LLM contract smoke test passed: %s responded in %d ms", result.Model, result.DurationMS)
}
