package llm

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestRemoteModel_ContextTokens(t *testing.T) {
	tests := []struct {
		name string
		m    RemoteModel
		want int
	}{
		{"max_model_len", RemoteModel{MaxModelLen: 8192}, 8192},
		{"max_context_length", RemoteModel{MaxContextLength: 32768}, 32768},
		{"context_length", RemoteModel{ContextLength: 4096}, 4096},
		{"priority", RemoteModel{MaxModelLen: 100, MaxContextLength: 200, ContextLength: 300}, 100},
		{"none", RemoteModel{ID: "x"}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.ContextTokens(); got != tt.want {
				t.Fatalf("ContextTokens()=%d want %d", got, tt.want)
			}
		})
	}
}

// modelsRoundTrip stands in for the network under a models probe.
type modelsRoundTrip func(*http.Request) (*http.Response, error)

func (f modelsRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// The two candidate paths are two paths on one host. A host that cannot be
// reached at all fails them both the same way, so the second attempt only
// doubled the wait: the settings panel's model probe spent thirty seconds on
// a provider behind a VPN that was not up — the client's whole timeout, twice.
func TestListRemoteModels_StopsAfterTransportFailure(t *testing.T) {
	saved := modelsTransport
	t.Cleanup(func() { modelsTransport = saved })
	var attempts int32
	modelsTransport = modelsRoundTrip(func(r *http.Request) (*http.Response, error) {
		atomic.AddInt32(&attempts, 1)
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("i/o timeout")}
	})

	_, err := ListRemoteModels(context.Background(), LLMConfig{APIBase: "http://10.255.255.1:1234"})
	if err == nil {
		t.Fatal("expected an error from a host that cannot be reached")
	}
	if n := atomic.LoadInt32(&attempts); n != 1 {
		t.Fatalf("attempts=%d want 1: the second path is on the same dead host", n)
	}
}

// An HTTP answer is a different matter: a 404 on /v1/models says the server
// is there and the path is wrong, and the plain /models path is still to be
// tried.
func TestListRemoteModels_FallsBackToPlainModelsPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"m-1"}]}`))
	}))
	t.Cleanup(srv.Close)

	models, err := ListRemoteModels(context.Background(), LLMConfig{APIBase: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID != "m-1" {
		t.Fatalf("models=%+v want [m-1]", models)
	}
}
