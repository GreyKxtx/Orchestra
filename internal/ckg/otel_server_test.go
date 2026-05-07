package ckg

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestOTLPServer(t *testing.T) *OTLPServer {
	t.Helper()
	s := newTestStore(t)
	return NewOTLPServer(s, t.TempDir())
}

func doOTLP(srv *OTLPServer, method, contentType, encoding string, body []byte) *http.Response {
	req := httptest.NewRequest(method, "/v1/traces", bytes.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if encoding != "" {
		req.Header.Set("Content-Encoding", encoding)
	}
	rr := httptest.NewRecorder()
	srv.httpSrv.Handler.ServeHTTP(rr, req)
	return rr.Result()
}

func gzipBytes(data []byte) []byte {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	_, _ = w.Write(data)
	_ = w.Close()
	return buf.Bytes()
}

func TestOTLPServer_HappyPath(t *testing.T) {
	srv := newTestOTLPServer(t)
	payload := minimalOTLPPayload("trace-aabb", "span-0011", "internal/agent/agent.go", 10)

	resp := doOTLP(srv, http.MethodPost, "application/json", "", payload)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200; body: %s", resp.StatusCode, body)
	}

	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, ok := out["partialSuccess"]; !ok {
		t.Errorf("response missing partialSuccess key: %v", out)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func TestOTLPServer_Gzip(t *testing.T) {
	srv := newTestOTLPServer(t)
	payload := minimalOTLPPayload("trace-gzip", "span-gz01", "main.go", 1)
	compressed := gzipBytes(payload)

	resp := doOTLP(srv, http.MethodPost, "application/json", "gzip", compressed)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200; body: %s", resp.StatusCode, body)
	}
}

func TestOTLPServer_ProtobufRejected(t *testing.T) {
	srv := newTestOTLPServer(t)
	resp := doOTLP(srv, http.MethodPost, "application/x-protobuf", "", []byte("fake"))
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(body, []byte("http/json")) {
		t.Errorf("want hint about http/json in body, got: %s", body)
	}
}

func TestOTLPServer_MethodNotAllowed(t *testing.T) {
	srv := newTestOTLPServer(t)
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		resp := doOTLP(srv, method, "application/json", "", nil)
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s: status = %d, want 405", method, resp.StatusCode)
		}
	}
}

func TestOTLPServer_InvalidJSON(t *testing.T) {
	srv := newTestOTLPServer(t)
	resp := doOTLP(srv, http.MethodPost, "application/json", "", []byte("not-json"))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestOTLPServer_InvalidGzip(t *testing.T) {
	srv := newTestOTLPServer(t)
	resp := doOTLP(srv, http.MethodPost, "application/json", "gzip", []byte("not-gzip"))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}
