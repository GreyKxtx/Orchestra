package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestServer_HealthRefreshContext(t *testing.T) {
	root := t.TempDir()
	// Create a couple files
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("Hello World\n"), 0644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "internal"), 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	srv, err := NewServer(ServerConfig{
		ProjectRoot:       root,
		Address:           "127.0.0.1",
		Port:              0,
		ExcludeDirs:       []string{".git", ".orchestra"},
		SkipBackups:       true,
		ScanInterval:      50 * time.Millisecond,
		MaxCacheFileBytes: 64 * 1024,
		CacheEnabled:      false,
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// warm scan
	if _, err := srv.state.ScanOnce(context.Background()); err != nil {
		t.Fatalf("ScanOnce failed: %v", err)
	}

	ts := httptest.NewServer(srv.httpServer.Handler)
	defer ts.Close()
	token := srv.Token()

	// health
	healthReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/health", nil)
	healthReq.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(healthReq)
	if err != nil {
		t.Fatalf("GET health failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}
	var h HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if h.ProtocolVersion != ProtocolVersion {
		t.Fatalf("unexpected protocol_version: %d", h.ProtocolVersion)
	}
	if h.ProjectRoot == "" || h.ProjectID == "" {
		t.Fatalf("expected project_root and project_id")
	}

	// refresh
	refreshReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/refresh", nil)
	refreshReq.Header.Set("Authorization", "Bearer "+token)
	refreshResp, err := http.DefaultClient.Do(refreshReq)
	if err != nil {
		t.Fatalf("POST refresh failed: %v", err)
	}
	defer refreshResp.Body.Close()
	if refreshResp.StatusCode != 200 {
		t.Fatalf("unexpected refresh status: %d", refreshResp.StatusCode)
	}
	var rr RefreshResponse
	if err := json.NewDecoder(refreshResp.Body).Decode(&rr); err != nil {
		t.Fatalf("decode refresh failed: %v", err)
	}
	if rr.Status != "ok" {
		t.Fatalf("unexpected refresh status: %q", rr.Status)
	}
	if rr.ScannedFiles == 0 {
		t.Fatalf("expected scanned_files > 0")
	}

	// context
	ctxReqBody, _ := json.Marshal(ContextRequest{Query: "hello", LimitKB: 50, Limits: &ContextLimits{MaxFiles: 10, MaxTotalBytes: 50 * 1024, MaxBytesPerFile: 1024}})
	ctxReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/context", bytes.NewReader(ctxReqBody))
	ctxReq.Header.Set("Content-Type", "application/json")
	ctxReq.Header.Set("Authorization", "Bearer "+token)
	ctxResp, err := http.DefaultClient.Do(ctxReq)
	if err != nil {
		t.Fatalf("POST context failed: %v", err)
	}
	defer ctxResp.Body.Close()
	if ctxResp.StatusCode != 200 {
		t.Fatalf("unexpected context status: %d", ctxResp.StatusCode)
	}
	var cr ContextResponse
	if err := json.NewDecoder(ctxResp.Body).Decode(&cr); err != nil {
		t.Fatalf("decode context failed: %v", err)
	}
	if len(cr.Files) == 0 {
		t.Fatalf("expected some context files")
	}
}
