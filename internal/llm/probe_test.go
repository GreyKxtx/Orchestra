package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
)

func TestProbeModels_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v0/models":
			http.NotFound(w, r)
			return
		case "/v1/models":
			if got := r.Header.Get("Authorization"); got != "Bearer secret" {
				t.Fatalf("Authorization = %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"m1","max_model_len":8192}]}`))
			return
		default:
			t.Fatalf("path = %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)

	res := Probe(context.Background(), config.LLMConfig{
		APIBase: srv.URL + "/v1",
		APIKey:  "secret",
		Model:   "m1",
	}, ProbeModels)
	if !res.OK {
		t.Fatalf("want OK, got %#v", res)
	}
	if res.ContextTokens != 8192 {
		t.Fatalf("ContextTokens=%d want 8192", res.ContextTokens)
	}
}

func TestProbeModels_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"Unauthorized"}`))
	}))
	t.Cleanup(srv.Close)

	res := Probe(context.Background(), config.LLMConfig{
		APIBase: srv.URL + "/v1",
		APIKey:  "bad",
	}, ProbeModels)
	if res.OK {
		t.Fatal("expected failure")
	}
	if res.HTTPCode != 401 {
		t.Fatalf("HTTPCode = %d", res.HTTPCode)
	}
	if res.Hint == "" {
		t.Fatal("expected hint for 401")
	}
}

func TestProbeChat_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v0/models":
			http.NotFound(w, r)
		case r.URL.Path == "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"qwen","max_model_len":4096}]}`))
		case strings.HasSuffix(r.URL.Path, "/chat/completions"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"pong"}}]}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)

	res := Probe(context.Background(), config.LLMConfig{
		Provider:  "vllm",
		APIBase:   srv.URL,
		APIKey:    "k",
		Model:     "qwen",
		MaxTokens: 16,
		TimeoutS:  5,
	}, ProbeChat)
	if !res.OK {
		t.Fatalf("want OK, got %#v", res)
	}
	if res.ContextTokens != 4096 {
		t.Fatalf("ContextTokens=%d", res.ContextTokens)
	}
}
