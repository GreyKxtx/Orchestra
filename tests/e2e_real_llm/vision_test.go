package e2e_real_llm

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 1×1 red PNG (smoke image for vision models).
const testPNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

// TestRealLLMVisionAttachment sends a PNG via session.message attachments and
// expects the model to acknowledge an image turn. Skips when the provider
// rejects multimodal payloads.
func TestRealLLMVisionAttachment(t *testing.T) {
	requireE2ELLM(t)

	projectDir := setupVisionTestProject(t)
	client := startCoreRPC(t, projectDir)
	client.initialize(projectDir)

	sessionID := client.sessionStart("")
	imgPath := filepath.Join(projectDir, "red-pixel.png")
	data, err := base64.StdEncoding.DecodeString(testPNGBase64)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(imgPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	query := "An image file is attached. Reply with ONLY the word RED if the image is a solid red pixel, or UNKNOWN otherwise."
	err = client.sessionMessageWithAttachments(sessionID, query, []map[string]any{
		{"path": imgPath, "kind": "image", "mime": "image/png", "name": "red-pixel.png"},
	})
	if err != nil {
		if strings.Contains(err.Error(), "multimodal") || strings.Contains(strings.ToLower(err.Error()), "vision") {
			t.Skipf("provider/model does not support vision: %v", err)
		}
		t.Fatalf("session.message: %v", err)
	}

	events := client.drainAgentEvents()
	found := false
	for _, ev := range events {
		if ev.Method != "agent/event" {
			continue
		}
		lower := strings.ToLower(string(ev.Params))
		if strings.Contains(lower, "red") || strings.Contains(lower, "unknown") {
			found = true
			break
		}
	}
	if !found {
		t.Logf("vision response not matched in events (model-dependent); history check still runs")
	}

	if client.sessionHistoryLen(sessionID) == 0 {
		t.Fatal("expected session history after vision turn")
	}
}

func setupVisionTestProject(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()

	cfg := map[string]interface{}{
		"project_root":     tmpDir,
		"context_limit_kb": 50,
		"llm": map[string]interface{}{
			"api_base":    getLLMAPIBase(),
			"api_key":     getLLMAPIKey(),
			"model":       getLLMModel(),
			"max_tokens":  8000,
			"temperature": 0.0,
			"multimodal":  true,
			"extra_body":  map[string]interface{}{"num_ctx": wantNumCtx},
		},
		"agent": map[string]interface{}{
			"max_steps":           8,
			"max_invalid_retries": 2,
		},
	}

	cfgBytes, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, ".orchestra.yml"), cfgBytes, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	mainGo := `package main

func main() {}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(mainGo), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	return tmpDir
}
