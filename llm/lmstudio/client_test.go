package lmstudio_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/orchestra/orchestra/llm/lmstudio"
)

func TestListModels_V0API(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v0/models" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "qwen3-27b", "object": "model", "type": "llm", "state": "loaded", "max_context_length": 20480},
				{"id": "llama-3b", "object": "model", "type": "llm", "state": "", "max_context_length": 8192},
			},
		})
	}))
	defer srv.Close()

	client := lmstudio.NewClient(srv.URL, "")
	models, err := client.ListModels()
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 {
		t.Fatalf("want 2 models, got %d", len(models))
	}
	if models[0].ID != "qwen3-27b" {
		t.Errorf("want first model qwen3-27b, got %s", models[0].ID)
	}
	if !models[0].IsLoaded {
		t.Error("want first model loaded")
	}
	if models[0].MaxContextLength != 20480 {
		t.Errorf("want MaxContextLength 20480, got %d", models[0].MaxContextLength)
	}
}

func TestListModels_V1Fallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v0/models" {
			http.Error(w, "not found", 404)
			return
		}
		if r.URL.Path == "/v1/models" {
			json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "mistral-7b"},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := lmstudio.NewClient(srv.URL, "")
	models, err := client.ListModels()
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID != "mistral-7b" {
		t.Errorf("fallback failed, got %+v", models)
	}
}

func TestListModels_Unreachable(t *testing.T) {
	client := lmstudio.NewClient("http://127.0.0.1:1", "")
	_, err := client.ListModels()
	if err == nil {
		t.Error("expected error for unreachable server")
	}
}

func TestListModels_V1MaxModelLen(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v0/models" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"Qwen/Qwen3.6-27B-FP8","object":"model","max_model_len":51200}]}`))
	}))
	t.Cleanup(srv.Close)

	client := lmstudio.NewClient(srv.URL, "")
	models, err := client.ListModels()
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].MaxContextLength != 51200 {
		t.Fatalf("got %+v", models)
	}
	n, err := client.FindModelContext("qwen/qwen3.6-27b-fp8")
	if err != nil {
		t.Fatal(err)
	}
	if n != 51200 {
		t.Fatalf("FindModelContext = %d", n)
	}
}

// llama.cpp's server reports the window it was started with under meta.n_ctx,
// next to n_ctx_train (what the model was trained for). Neither was read, so
// discovery found nothing and the name catalog answered instead: a Qwen3.5-9B
// served with -c 32768 was taken for 131072 tokens. Compaction then waited for
// a prompt four times larger than the server accepts, and the server refused
// first ("request exceeds the available context size"). The running window is
// n_ctx, not n_ctx_train.
func TestListModels_V1LlamaCppMetaNCtx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v0/models" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"models":[{"name":"/models/Qwen3.5-9B-Q4_K_M.gguf"}],"object":"list","data":[{"id":"/models/Qwen3.5-9B-Q4_K_M.gguf","object":"model","owned_by":"llamacpp","meta":{"vocab_type":1,"n_vocab":248320,"n_ctx":32768,"n_ctx_train":262144,"n_embd":4096}}]}`))
	}))
	t.Cleanup(srv.Close)

	n, err := lmstudio.NewClient(srv.URL, "").FindModelContext("/models/Qwen3.5-9B-Q4_K_M.gguf")
	if err != nil {
		t.Fatal(err)
	}
	if n != 32768 {
		t.Fatalf("FindModelContext = %d, want the served window 32768 (n_ctx), not n_ctx_train", n)
	}
}
