package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/llm"
)

// Every TUI start probed the model and wrote what it found into .orchestra.yml
// — num_ctx under extra_body and under the model's preset, plus every default
// the config struct carries. A workspace that was clean in git was dirty after
// one start. Worse, the written window then reads as the user's choice: load
// the model with a larger context later, and "user num_ctx below the server's"
// keeps the stale value. What the server reports is used for this session and
// not written down; core discovers it again on its own start.
func TestDiscoveredLimits_AreUsedButNotWrittenToTheConfig(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".orchestra.yml")
	original := "project_root: .\nllm:\n  model: qwen/qwen3.8-27b\n  timeout_s: 600\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	a := testChromeApp(t)
	a.cfgStore = newConfigStore(path, root)

	cmd := a.discoveredLimitsCmd(llm.ProbeResult{OK: true, ContextTokens: 25088})
	if cmd == nil {
		t.Fatal("a probe that found the window must still report it")
	}
	msg, ok := cmd().(limitsAppliedMsg)
	if !ok || msg.err != nil || msg.contextTokens != 25088 {
		t.Fatalf("limits message = %+v (ok=%v), want the 25088 window", msg, ok)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Errorf("the probe rewrote .orchestra.yml:\n%s", got)
	}
}
