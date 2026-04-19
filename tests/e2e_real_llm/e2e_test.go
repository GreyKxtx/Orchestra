package e2e_real_llm

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/protocol"
)

const (
	envE2ELLM = "ORCH_E2E_LLM"
)

// requireE2ELLM skips test if ORCH_E2E_LLM is not set to "1"
func requireE2ELLM(t *testing.T) {
	if os.Getenv(envE2ELLM) != "1" {
		t.Skipf("set %s=1 to enable E2E tests with real LLM", envE2ELLM)
	}
}

// findOrchestraBinary finds orchestra binary (built or in PATH)
func findOrchestraBinary(t *testing.T) string {
	// Try relative path first (if built in project root)
	relPath := "../../orchestra"
	if _, err := os.Stat(relPath); err == nil {
		abs, _ := filepath.Abs(relPath)
		return abs
	}
	// Try orchestra.exe on Windows
	relPathExe := "../../orchestra.exe"
	if _, err := os.Stat(relPathExe); err == nil {
		abs, _ := filepath.Abs(relPathExe)
		return abs
	}
	// Fallback to PATH
	return "orchestra"
}

// runOrchestra runs orchestra command and returns stdout, stderr, exit code
func runOrchestra(t *testing.T, workdir string, args ...string) (stdout, stderr string, exitCode int) {
	binary := findOrchestraBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = workdir
	cmd.Env = os.Environ()

	var stdoutBuf, stderrBuf strings.Builder
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := cmd.Run()
	stdout = stdoutBuf.String()
	stderr = stderrBuf.String()

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("Failed to run orchestra: %v\nStdout: %s\nStderr: %s", err, stdout, stderr)
		}
	}

	return stdout, stderr, exitCode
}

// setupTestProject creates a minimal test project fixture
func setupTestProject(t *testing.T) string {
	tmpDir := t.TempDir()

	// Create .orchestra.yml
	cfg := map[string]interface{}{
		"project_root":     tmpDir,
		"exclude_dirs":     []string{".git", ".orchestra"},
		"context_limit_kb": 50,
		"llm": map[string]interface{}{
			"api_base":    getLLMAPIBase(),
			"api_key":     getLLMAPIKey(),
			"model":       getLLMModel(),
			"max_tokens":  8000,
			"temperature": 0.0, // Deterministic for tests
		},
		"agent": map[string]interface{}{
			"max_steps":           15, // Increased for real LLM which may need more steps
			"max_invalid_retries": 3,
		},
	}

	cfgBytes, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	cfgPath := filepath.Join(tmpDir, ".orchestra.yml")
	if err := os.WriteFile(cfgPath, cfgBytes, 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// Create simple Go project
	mainGo := `package main

import "fmt"

func greet(name string) {
	fmt.Printf("Hello, %s!\n", name)
}

func main() {
	greet("World")
}
`
	mainPath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(mainPath, []byte(mainGo), 0644); err != nil {
		t.Fatalf("Failed to write main.go: %v", err)
	}

	// Create utils.go
	utilsGo := `package main

func add(a, b int) int {
	return a + b
}
`
	utilsPath := filepath.Join(tmpDir, "utils.go")
	if err := os.WriteFile(utilsPath, []byte(utilsGo), 0644); err != nil {
		t.Fatalf("Failed to write utils.go: %v", err)
	}

	return tmpDir
}

// getLLMAPIBase returns LLM API base URL from env or default
func getLLMAPIBase() string {
	if v := os.Getenv("ORCH_E2E_LLM_API_BASE"); v != "" {
		return v
	}
	return "http://localhost:8000/v1"
}

// getLLMAPIKey returns LLM API key from env or default
func getLLMAPIKey() string {
	if v := os.Getenv("ORCH_E2E_LLM_API_KEY"); v != "" {
		return v
	}
	return "lm-studio" // Default for local LLM
}

// getLLMModel returns LLM model name from env or default
func getLLMModel() string {
	if v := os.Getenv("ORCH_E2E_LLM_MODEL"); v != "" {
		return v
	}
	return "gpt-3.5-turbo" // Default fallback
}

// ErrorCategory classifies errors from orchestra output
type ErrorCategory int

const (
	ErrorCategoryOK             ErrorCategory = iota
	ErrorCategoryModelOutput                  // Model generated invalid ops/schema
	ErrorCategorySystemBug                    // Internal system error (panic, protocol, NotInitialized, etc.)
	ErrorCategoryInfrastructure               // Network, timeout, DNS, connection issues
)

// classifyError categorizes error from orchestra output
func classifyError(combined string, exitCode int) ErrorCategory {
	if exitCode == 0 {
		return ErrorCategoryOK
	}

	lower := strings.ToLower(combined)

	// Infrastructure errors (network, connection, timeout)
	if strings.Contains(lower, "connection") ||
		strings.Contains(lower, "timeout") ||
		strings.Contains(lower, "failed to connect") ||
		strings.Contains(lower, "no such host") ||
		strings.Contains(lower, "network") ||
		strings.Contains(lower, "dns") {
		return ErrorCategoryInfrastructure
	}

	// System bugs (protocol errors, initialization, panics, framing)
	if strings.Contains(combined, string(protocol.NotInitialized)) ||
		strings.Contains(lower, "panic") ||
		strings.Contains(lower, "fatal") ||
		strings.Contains(lower, "content-length") ||
		strings.Contains(lower, "framing") ||
		strings.Contains(lower, "protocol mismatch") ||
		strings.Contains(lower, "failed to start core") ||
		strings.Contains(lower, "permission denied") ||
		strings.Contains(lower, "codec parse error") ||
		strings.Contains(lower, "invalid request") && !strings.Contains(lower, "unknown field") {
		return ErrorCategorySystemBug
	}

	// Model output errors (invalid ops, schema violations, tool denied, max_steps)
	if strings.Contains(lower, "max_steps exceeded") ||
		strings.Contains(lower, "unknown field") ||
		strings.Contains(lower, "json:") ||
		strings.Contains(lower, "invalid ops") ||
		strings.Contains(lower, "schema") ||
		strings.Contains(combined, string(protocol.ExecDenied)) ||
		strings.Contains(combined, string(protocol.InvalidLLMOutput)) {
		return ErrorCategoryModelOutput
	}

	// Unknown error - treat as system bug to be safe
	return ErrorCategorySystemBug
}

// parseApplyOutput parses orchestra apply output to extract ops/plan info
func parseApplyOutput(stdout, stderr string) (hasOps bool, hasDiff bool, errorCode string) {
	combined := stdout + "\n" + stderr

	// Check for ops/diff indicators
	hasOps = strings.Contains(combined, `"ops"`) || strings.Contains(combined, "ops:")
	hasDiff = strings.Contains(combined, "diff") || strings.Contains(combined, "---") || strings.Contains(combined, "+++")

	// Check for error codes
	if strings.Contains(combined, string(protocol.StaleContent)) {
		errorCode = string(protocol.StaleContent)
	} else if strings.Contains(combined, string(protocol.PathTraversal)) {
		errorCode = string(protocol.PathTraversal)
	} else if strings.Contains(combined, string(protocol.ExecDenied)) {
		errorCode = string(protocol.ExecDenied)
	}

	return hasOps, hasDiff, errorCode
}
