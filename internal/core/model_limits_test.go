package core

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/llm"
)

// Opening a project must not wait on the LLM server. It used to: New asked
// the server for the model's context window with an eight-second timeout,
// and a project whose endpoint was down — or on a VPN that was not up —
// took the full eight seconds to open, every time, showing nothing.
func TestNew_DoesNotWaitForModelLimitDiscovery(t *testing.T) {
	answered := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Slow, the way a server behind a dead route is slow — but it does
		// answer, so the test can also check the answer lands.
		select {
		case <-time.After(1500 * time.Millisecond):
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"slow-model","object":"model","type":"llm","max_context_length":65536,"state":"loaded"}]}`))
		select {
		case answered <- struct{}{}:
		default:
		}
	}))
	t.Cleanup(srv.Close)

	root := t.TempDir()
	cfg := config.DefaultConfig(root)
	cfg.ProjectRoot = root
	cfg.LLM.APIBase = srv.URL + "/v1"
	cfg.LLM.Model = "slow-model"
	if err := config.Save(filepath.Join(root, ".orchestra.yml"), cfg); err != nil {
		t.Fatal(err)
	}

	started := time.Now()
	c, err := New(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if took := time.Since(started); took > time.Second {
		t.Fatalf("New waited on the LLM server: %v", took)
	}

	// The answer arrives later and is applied where every RPC passes through.
	select {
	case <-answered:
	case <-time.After(5 * time.Second):
		t.Fatal("the server was never asked")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		c.applyDiscoveredModelLimits()
		c.cfgMu.RLock()
		got, _ := c.cfg.LLM.ExtraBody["num_ctx"].(int)
		c.cfgMu.RUnlock()
		if got == 65536 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("discovered window never applied: num_ctx=%v", got)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A window measured for one model must not be applied to another: the model
// can be switched between the ask going out and the answer coming back.
func TestApplyDiscoveredModelLimits_IgnoresAnotherModel(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultConfig(root)
	cfg.ProjectRoot = root
	cfg.LLM.APIBase = "http://127.0.0.1:1/v1"
	cfg.LLM.Model = "current-model"
	if err := config.Save(filepath.Join(root, ".orchestra.yml"), cfg); err != nil {
		t.Fatal(err)
	}
	c, err := New(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	c.limitsMu.Lock()
	c.discoveredLimits = &llm.ModelLimits{Model: "other-model", ContextTokens: 4096}
	c.limitsMu.Unlock()
	c.applyDiscoveredModelLimits()

	c.cfgMu.RLock()
	_, set := c.cfg.LLM.ExtraBody["num_ctx"]
	c.cfgMu.RUnlock()
	if set {
		t.Fatal("a window for another model was applied")
	}
}
