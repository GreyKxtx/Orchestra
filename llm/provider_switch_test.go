package llm

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogProviderSwitch_LandsInTheLog(t *testing.T) {
	dir := t.TempDir()
	logger := NewLogger(dir)
	logger.LogProviderSwitch("vllm", "openrouter", "endpoint unreachable")

	data, err := os.ReadFile(filepath.Join(dir, ".orchestra", "llm_log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	line := string(data)
	for _, want := range []string{`"event":"provider.switch"`, `"vllm"`, "openrouter", "unreachable"} {
		if !strings.Contains(line, want) {
			t.Errorf("log line missing %q:\n%s", want, line)
		}
	}
}

func TestMaybeWrapFallback_RecordsTheSwitch(t *testing.T) {
	// A failover that leaves no trace is indistinguishable from a slow day.
	dir := t.TempDir()
	logger := NewLogger(dir)
	primary := &scriptedClient{label: "primary", err: unreachable()}

	wrapped := MaybeWrapFallback(primary, fallbackRegistry(),
		LLMConfig{Provider: "vllm", APIBase: "http://localhost:8000/v1", FallbackProvider: "backup"}, logger)
	fb, ok := wrapped.(*FallbackClient)
	if !ok {
		t.Fatalf("got %T", wrapped)
	}
	// The standby is a real client pointed at a host we will not reach; the
	// failover itself is what this asserts, not the second call's outcome.
	_, _ = fb.Complete(context.Background(), CompleteRequest{})

	data, _ := os.ReadFile(filepath.Join(dir, ".orchestra", "llm_log.jsonl"))
	if !strings.Contains(string(data), `"event":"provider.switch"`) {
		t.Errorf("no provider.switch entry after a failover:\n%s", data)
	}
}
