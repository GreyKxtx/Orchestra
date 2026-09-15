package view

import (
	"strings"
	"testing"
	"time"
)

// llama.cpp names a model by the file it loaded, so the id is a path:
// "/models/models--unsloth--Qwen3.5-9B-MTP-GGUF/snapshots/9716a6…/Qwen3.5-9B-Q4_K_M.gguf".
// The TUI printed it whole in the turn footer and the input row, where it
// wrapped over three lines. A path is shown as its file name; an id that is
// only namespaced ("qwen/qwen3.8-27b") is shown as it is.
func TestDisplayModelName(t *testing.T) {
	for in, want := range map[string]string{
		"/models/models--unsloth--Qwen3.5-9B-MTP-GGUF/snapshots/9716a636ee4bddc3fed678220b7a33dd2a4160ae/Qwen3.5-9B-Q4_K_M.gguf": "Qwen3.5-9B-Q4_K_M",
		`C:\models\qwen\Qwen3-8B-Q8_0.gguf`: "Qwen3-8B-Q8_0",
		"qwen/qwen3.8-27b":                  "qwen/qwen3.8-27b",
		"Qwen/Qwen3.6-27B-FP8":              "Qwen/Qwen3.6-27B-FP8",
		"gpt-5":                             "gpt-5",
		"":                                  "",
	} {
		if got := DisplayModelName(in); got != want {
			t.Errorf("DisplayModelName(%q) = %q, want %q", in, got, want)
		}
	}

	footer := assistantFooter("build", "/models/snapshots/abc/Qwen3.5-9B-Q4_K_M.gguf", 15*time.Second, 0, 0, false, 0)
	if strings.Contains(footer, "snapshots") || !strings.Contains(footer, "Qwen3.5-9B-Q4_K_M") {
		t.Errorf("turn footer shows the model path: %q", footer)
	}
}
