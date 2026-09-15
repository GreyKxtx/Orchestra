package core

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
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
	// The server answers only when the test says so. Timing New with a clock
	// failed on loaded CI runners — New alone took 3 to 25 seconds there while
	// the server under test was idle — so the proof is an ordering instead: New
	// returns while the server's request is still unanswered and not given up.
	release := make(chan struct{})
	answered := make(chan struct{}, 1)
	abandoned := make(chan struct{})
	var abandonOnce sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
			abandonOnce.Do(func() { close(abandoned) })
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
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })

	root := t.TempDir()
	cfg := config.DefaultConfig(root)
	cfg.ProjectRoot = root
	cfg.LLM.APIBase = srv.URL + "/v1"
	cfg.LLM.Model = "slow-model"
	if err := config.Save(filepath.Join(root, ".orchestra.yml"), cfg); err != nil {
		t.Fatal(err)
	}

	type made struct {
		c   *Core
		err error
	}
	done := make(chan made, 1)
	go func() {
		c, err := New(root, Options{})
		done <- made{c, err}
	}()
	var c *Core
	select {
	case m := <-done:
		if m.err != nil {
			t.Fatal(m.err)
		}
		c = m.c
	case <-time.After(2 * time.Minute):
		t.Fatal("New waited on an LLM server that had not answered")
	}
	t.Cleanup(func() { _ = c.Close() })
	// A New that asked with a timeout has cancelled the request by now; the
	// handler sees that a moment later.
	select {
	case <-abandoned:
		t.Fatal("New waited on the LLM server until its request gave up")
	case <-time.After(300 * time.Millisecond):
	}
	releaseOnce.Do(func() { close(release) })

	// The answer arrives later and is applied where every RPC passes through.
	select {
	case <-answered:
	case <-time.After(30 * time.Second):
		t.Fatal("the server was never asked")
	}
	deadline := time.Now().Add(30 * time.Second)
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
