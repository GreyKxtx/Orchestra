package embed

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
)

func fakeEmbedServer(t *testing.T, dim int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			http.Error(w, "wrong path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "wrong method", http.StatusMethodNotAllowed)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req embedReq
		_ = json.Unmarshal(body, &req)
		if req.Model == "" {
			http.Error(w, "missing model", http.StatusBadRequest)
			return
		}
		// Deterministic vector: index i → [float32(i), 1, 1, ..., 1]
		// allows tests to verify ordering and per-input shape.
		data := make([]struct {
			Embedding []float32 `json:"embedding"`
		}, len(req.Input))
		for i := range req.Input {
			vec := make([]float32, dim)
			for j := range vec {
				vec[j] = 1
			}
			vec[0] = float32(i)
			data[i].Embedding = vec
		}
		resp := embedResp{Data: data}
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestHTTPClient_Embed_HappyPath(t *testing.T) {
	srv := fakeEmbedServer(t, 4)
	defer srv.Close()

	c := New(config.EmbedConfig{
		APIBase: srv.URL + "/v1",
		Model:   "test-model",
	})
	vecs, err := c.Embed(context.Background(), []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vecs) != 3 {
		t.Fatalf("len: %d", len(vecs))
	}
	if len(vecs[0]) != 4 {
		t.Errorf("dim: %d", len(vecs[0]))
	}
	if vecs[2][0] != 2 {
		t.Errorf("ordering broken: vecs[2][0]=%v", vecs[2][0])
	}
	if c.Dim() != 4 {
		t.Errorf("learned dim: %d", c.Dim())
	}
}

func TestHTTPClient_Embed_BatchSplitting(t *testing.T) {
	srv := fakeEmbedServer(t, 2)
	defer srv.Close()
	c := New(config.EmbedConfig{
		APIBase:   srv.URL + "/v1",
		Model:     "m",
		BatchSize: 2,
	})
	inputs := []string{"a", "b", "c", "d", "e"}
	vecs, err := c.Embed(context.Background(), inputs)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vecs) != 5 {
		t.Errorf("len: %d", len(vecs))
	}
}

func TestHTTPClient_Embed_EmptyInput(t *testing.T) {
	c := New(config.EmbedConfig{Model: "m"})
	vecs, err := c.Embed(context.Background(), nil)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if vecs != nil {
		t.Errorf("expected nil, got %v", vecs)
	}
}

func TestHTTPClient_Embed_NoModel(t *testing.T) {
	c := New(config.EmbedConfig{}) // model is empty
	_, err := c.Embed(context.Background(), []string{"x"})
	if err == nil || !strings.Contains(err.Error(), "model is empty") {
		t.Fatalf("expected model-empty error, got %v", err)
	}
}

func TestHTTPClient_Embed_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := New(config.EmbedConfig{APIBase: srv.URL + "/v1", Model: "m"})
	_, err := c.Embed(context.Background(), []string{"x"})
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("expected HTTP-500 error, got %v", err)
	}
}

func TestHTTPClient_Embed_SendsAuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"data":[{"embedding":[1.0]}]}`))
	}))
	defer srv.Close()
	c := New(config.EmbedConfig{APIBase: srv.URL + "/v1", Model: "m", APIKey: "sk-test"})
	_, err := c.Embed(context.Background(), []string{"x"})
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer sk-test" {
		t.Errorf("auth header: %q", gotAuth)
	}
}

func TestCosineSimilarity(t *testing.T) {
	cases := []struct {
		name string
		a, b []float32
		want float32
		tol  float32
	}{
		{"identical", []float32{1, 0, 0}, []float32{1, 0, 0}, 1, 1e-6},
		{"orthogonal", []float32{1, 0}, []float32{0, 1}, 0, 1e-6},
		{"opposite", []float32{1, 0}, []float32{-1, 0}, -1, 1e-6},
		{"scaled identical", []float32{2, 0}, []float32{4, 0}, 1, 1e-6},
		{"len mismatch", []float32{1, 0}, []float32{1, 0, 0}, 0, 0},
		{"zero vector", []float32{0, 0}, []float32{1, 0}, 0, 0},
		{"empty", nil, nil, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CosineSimilarity(tc.a, tc.b)
			if math.Abs(float64(got-tc.want)) > float64(tc.tol) {
				t.Errorf("got %v want %v", got, tc.want)
			}
		})
	}
}
