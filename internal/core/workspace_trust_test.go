package core_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/core"
)

// Opening a repository used to start the MCP servers its committed config
// named, before anyone was asked. An untrusted workspace starts none; trusting
// it starts them without restarting the core.
func TestUntrustedWorkspaceStartsNoMCPServerUntilTrusted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ORCHESTRA_TRUST_STORE", filepath.Join(t.TempDir(), "trusted.json"))
	root := t.TempDir()
	marker := filepath.Join(t.TempDir(), "started")
	cfg := "project_root: .\nllm:\n  api_base: http://127.0.0.1:9/v1\n  model: m\n" +
		"mcp:\n  servers:\n    - name: probe\n      command: [\"sh\", \"-c\", \"touch " + marker + "\"]\n"
	if err := os.WriteFile(filepath.Join(root, ".orchestra.yml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := core.New(root, core.Options{LLMClient: stubLLM{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	time.Sleep(200 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("an untrusted workspace started its MCP server")
	}
	st, err := c.WorkspaceTrustStatus()
	if err != nil || st.Trusted || len(st.Ignored) == 0 {
		t.Fatalf("status must report the ignored servers: %+v %v", st, err)
	}

	res, err := c.WorkspaceTrust(context.Background(), core.WorkspaceTrustParams{})
	if err != nil || !res.Trusted {
		t.Fatalf("trust: %+v %v", res, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("trusting the workspace must start its MCP server")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
