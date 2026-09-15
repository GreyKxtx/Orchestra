package mcp

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/config"
)

func callText(t *testing.T, ctx context.Context, m *Manager, name string) string {
	t.Helper()
	raw, err := m.Call(ctx, name, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var out struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return out.Result
}

func sameDir(t *testing.T, a, b string) bool {
	t.Helper()
	sa, err := os.Stat(a)
	if err != nil {
		t.Fatalf("stat %s: %v", a, err)
	}
	sb, err := os.Stat(b)
	if err != nil {
		t.Fatalf("stat %s: %v", b, err)
	}
	return os.SameFile(sa, sb)
}

// A local server ran in whatever directory Orchestra itself was started from.
// .orchestra.yml is committed with the project, so a relative path in it —
// the filesystem server's `./notes`, a `node ./tools/server.js` — resolved
// against the project only when core happened to start there. Started from
// anywhere else (seen live: core launched with --workspace-root from outside
// the project), the server failed to start, and its tools silently vanished
// from every turn. The same server also has to come back in the same place
// after a crash, and with the hooks it was started with.
func TestMCPManager_RunsLocalServersInTheProjectAndRestartsThemThere(t *testing.T) {
	t.Setenv("ORCH_TEST_MCP_SERVER", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	project := t.TempDir()
	cfg := config.MCPConfig{Servers: []config.MCPServerConfig{{
		Name:          "testserver",
		Command:       testSelfBinary(),
		AllowSampling: true,
	}}}
	hooks := Hooks{
		Consent: func(context.Context, ConsentRequest) bool { return true },
		Sample: func(context.Context, SamplingRequest) (SamplingResult, error) {
			return SamplingResult{Model: "fake-model", Text: "4"}, nil
		},
	}
	mgr, errs := NewManager(ctx, cfg, project, hooks)
	if len(errs) > 0 {
		t.Fatalf("NewManager: %v", errs)
	}
	defer mgr.Close()

	if wd := callText(t, ctx, mgr, "mcp:testserver:cwd"); !sameDir(t, wd, project) {
		t.Errorf("the server runs in %s, not in the project %s", wd, project)
	}

	// Crash it. The call that finds it dead restarts it.
	_, _ = mgr.Call(ctx, "mcp:testserver:exit", json.RawMessage(`{}`))
	deadline := time.Now().Add(10 * time.Second)
	for !mgr.findClient("testserver").IsDead() {
		if time.Now().After(deadline) {
			t.Fatal("the server did not exit")
		}
		time.Sleep(20 * time.Millisecond)
	}

	if wd := callText(t, ctx, mgr, "mcp:testserver:cwd"); !sameDir(t, wd, project) {
		t.Errorf("after a restart the server runs in %s, not in the project %s", wd, project)
	}
	if out := callText(t, ctx, mgr, "mcp:testserver:ask_model"); !strings.Contains(out, `sampled="4"`) {
		t.Errorf("after a restart the server lost the sampling hooks it was started with: %s", out)
	}
}
