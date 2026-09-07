package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// modelsServer answers GET …/models with the given model ids, and 404s
// everything else — the shape an OpenAI-compatible local server presents.
func modelsServer(t *testing.T, ids ...string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" && r.URL.Path != "/models" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		body := `{"data":[`
		for i, id := range ids {
			if i > 0 {
				body += ","
			}
			body += `{"id":"` + id + `"}`
		}
		body += `]}`
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDetectLocalServer_ReportsTheServerAndItsModels(t *testing.T) {
	srv := modelsServer(t, "qwen2.5-coder-7b", "nemotron-4b")

	got, ok := detectLocalServerAt(context.Background(), []string{srv.URL})
	if !ok {
		t.Fatal("detect returned false for a server that answers /v1/models")
	}
	if got.APIBase != srv.URL+"/v1" {
		t.Errorf("APIBase = %q, want %q — init writes this into .orchestra.yml verbatim",
			got.APIBase, srv.URL+"/v1")
	}
	if len(got.Models) != 2 || got.Models[0].ID != "qwen2.5-coder-7b" {
		t.Errorf("Models = %+v, want the two the server listed, first one first", got.Models)
	}
}

// A port with nothing behind it must not be reported, and must not stall init:
// the whole point of probing is that it costs nothing when no server is up.
func TestDetectLocalServer_IgnoresDeadPorts(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := dead.URL
	dead.Close() // nothing listens on deadURL now

	start := time.Now()
	if _, ok := detectLocalServerAt(context.Background(), []string{deadURL}); ok {
		t.Fatal("detect returned true for a port nothing listens on")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("probing one dead port took %s — init would appear to hang", elapsed)
	}
}

// A server that answers but has no models is not usable: init would write an
// api_base with an empty model, which is worse than the default it replaces.
func TestDetectLocalServer_SkipsAServerWithNoModels(t *testing.T) {
	srv := modelsServer(t)
	if _, ok := detectLocalServerAt(context.Background(), []string{srv.URL}); ok {
		t.Fatal("detect returned true for a server advertising no models")
	}
}

// Order is the caller's priority, not whoever answers first: two servers up
// must resolve the same way on every run, or `orchestra init` writes a
// different config depending on the weather.
func TestDetectLocalServer_PrefersTheEarlierCandidate(t *testing.T) {
	first := modelsServer(t, "first-model")
	second := modelsServer(t, "second-model")

	got, ok := detectLocalServerAt(context.Background(), []string{first.URL, second.URL})
	if !ok {
		t.Fatal("detect returned false with two live servers")
	}
	if got.APIBase != first.URL+"/v1" {
		t.Errorf("APIBase = %q, want the first candidate %q", got.APIBase, first.URL+"/v1")
	}

	// Same set, reversed priority — the answer must follow the list.
	got, ok = detectLocalServerAt(context.Background(), []string{second.URL, first.URL})
	if !ok {
		t.Fatal("detect returned false with two live servers (reversed)")
	}
	if got.APIBase != second.URL+"/v1" {
		t.Errorf("APIBase = %q, want the first candidate %q", got.APIBase, second.URL+"/v1")
	}
}

// The exported entry point must probe the ports the plan names, in that order:
// LM Studio, Ollama, then vLLM / llama.cpp.
func TestLocalServerCandidates_AreTheDocumentedPorts(t *testing.T) {
	want := []string{
		"http://localhost:1234",
		"http://localhost:11434",
		"http://localhost:8000",
	}
	got := localServerCandidates()
	if len(got) != len(want) {
		t.Fatalf("candidates = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidates = %v, want %v", got, want)
		}
	}
}
