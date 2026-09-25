package ckg

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func makeSourceReq(file, start, end string) *http.Request {
	req := httptest.NewRequest("GET", "/api/source", nil)
	q := req.URL.Query()
	if file != "" {
		q.Set("file", file)
	}
	if start != "" {
		q.Set("start", start)
	}
	if end != "" {
		q.Set("end", end)
	}
	req.URL.RawQuery = q.Encode()
	return req
}

func TestSourceHandler_MissingParams(t *testing.T) {
	root := t.TempDir()
	h := sourceHandlerFunc(root)

	cases := []struct {
		name  string
		file  string
		start string
		end   string
	}{
		{"no file", "", "1", "2"},
		{"no start", "f.go", "", "2"},
		{"no end", "f.go", "1", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			h(w, makeSourceReq(tc.file, tc.start, tc.end))
			if w.Code != http.StatusBadRequest {
				t.Errorf("got %d, want 400", w.Code)
			}
		})
	}
}

func TestSourceHandler_PathTraversal_Rejected(t *testing.T) {
	root := t.TempDir()
	// Write a file outside root to ensure we're not accidentally reading it.
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	_ = os.WriteFile(secret, []byte("forbidden"), 0644)

	h := sourceHandlerFunc(root)
	w := httptest.NewRecorder()
	// Attempt traversal: ../secret.txt relative to root
	h(w, makeSourceReq("../secret.txt", "1", "1"))
	if w.Code != http.StatusForbidden {
		t.Errorf("got %d, want 403", w.Code)
	}
}

func TestSourceHandler_FileNotFound(t *testing.T) {
	root := t.TempDir()
	h := sourceHandlerFunc(root)

	w := httptest.NewRecorder()
	h(w, makeSourceReq("nonexistent.go", "1", "3"))
	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", w.Code)
	}
}

func TestSourceHandler_NormalSnippet(t *testing.T) {
	root := t.TempDir()
	content := "line1\nline2\nline3\nline4\nline5\n"
	if err := os.WriteFile(filepath.Join(root, "f.go"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	h := sourceHandlerFunc(root)
	w := httptest.NewRecorder()
	h(w, makeSourceReq("f.go", "2", "4"))
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}
	got := w.Body.String()
	if !strings.Contains(got, "line2") || !strings.Contains(got, "line4") {
		t.Errorf("snippet mismatch: %q", got)
	}
	if strings.Contains(got, "line1") || strings.Contains(got, "line5") {
		t.Errorf("snippet contains lines outside range: %q", got)
	}
}

func TestSourceHandler_OutOfBoundsClamped(t *testing.T) {
	root := t.TempDir()
	content := "a\nb\nc\n"
	if err := os.WriteFile(filepath.Join(root, "g.go"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	h := sourceHandlerFunc(root)

	// start=0 should clamp to 1; end=999 should clamp to len(lines).
	w := httptest.NewRecorder()
	h(w, makeSourceReq("g.go", "0", "999"))
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}
	// Should return all lines without panic.
	got := w.Body.String()
	if !strings.Contains(got, "a") || !strings.Contains(got, "c") {
		t.Errorf("clamped response missing content: %q", got)
	}
}

// ---- StartUIServer ----

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

func TestStartUIServer_ServesAPI(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	port := freePort(t)

	const token = "t0ken"
	go func() {
		_ = StartUIServer(store, root, port, token)
	}()

	addr := fmt.Sprintf("http://127.0.0.1:%d", port)

	// Poll until the server is up (max 2 s).
	var lastErr error
	for i := 0; i < 40; i++ {
		time.Sleep(50 * time.Millisecond)
		resp, err := http.Get(addr + "/api/graph?token=" + token)
		if err == nil {
			resp.Body.Close()
			lastErr = nil
			break
		}
		lastErr = err
	}
	if lastErr != nil {
		t.Fatalf("server did not start: %v", lastErr)
	}

	// Without the token nothing is served.
	if resp, err := http.Get(addr + "/api/graph"); err != nil {
		t.Fatalf("GET /api/graph: %v", err)
	} else {
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("/api/graph without a token = %d, want 401", resp.StatusCode)
		}
	}

	// /api/graph should return valid JSON.
	resp, err := http.Get(addr + "/api/graph?token=" + token)
	if err != nil {
		t.Fatalf("GET /api/graph: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/api/graph status = %d, want 200", resp.StatusCode)
	}
	var gd GraphData
	if err := json.NewDecoder(resp.Body).Decode(&gd); err != nil {
		t.Errorf("decode graph data: %v", err)
	}

	// / should return the HTML UI.
	resp2, err := http.Get(addr + "/?token=" + token)
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp2.Body.Close()
	if ct := resp2.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("/ Content-Type = %q, want text/html", ct)
	}
}

func TestSourceHandler_SubdirFile(t *testing.T) {
	root := t.TempDir()
	subDir := filepath.Join(root, "pkg")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "sub.go"), []byte("package pkg\n"), 0644); err != nil {
		t.Fatal(err)
	}

	h := sourceHandlerFunc(root)
	w := httptest.NewRecorder()
	h(w, makeSourceReq("pkg/sub.go", "1", "1"))
	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200", w.Code)
	}
}

// /api/source serves project files only: not the credential files, not
// through "..".
func TestSourceHandler_RefusesCredentialsAndTraversal(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".orchestra.env"), []byte("KEY=sk-live\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := sourceHandlerFunc(root)
	for _, f := range []string{".orchestra.env", "../../etc/passwd", "/etc/passwd"} {
		w := httptest.NewRecorder()
		h(w, makeSourceReq(f, "1", "5"))
		if w.Code != http.StatusForbidden || strings.Contains(w.Body.String(), "sk-live") {
			t.Fatalf("%s: got %d %q, want 403", f, w.Code, w.Body.String())
		}
	}
}

// A page on another name (DNS rebinding) is refused even with the cookie.
func TestRequireUIToken_ChecksHostAndToken(t *testing.T) {
	h := requireUIToken("tok", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	cases := []struct {
		host, target string
		want         int
	}{
		{"127.0.0.1:6061", "/api/graph", http.StatusUnauthorized},
		{"127.0.0.1:6061", "/api/graph?token=bad", http.StatusUnauthorized},
		{"127.0.0.1:6061", "/api/graph?token=tok", http.StatusOK},
		{"localhost:6061", "/?token=tok", http.StatusOK},
		{"evil.example:6061", "/?token=tok", http.StatusForbidden},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodGet, c.target, nil)
		r.Host = c.host
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != c.want {
			t.Fatalf("%s %s = %d, want %d", c.host, c.target, w.Code, c.want)
		}
	}
	// The first load leaves a cookie that admits later requests.
	r := httptest.NewRequest(http.MethodGet, "/?token=tok", nil)
	r.Host = "127.0.0.1:6061"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly {
		t.Fatalf("want one HttpOnly cookie, got %+v", cookies)
	}
	r2 := httptest.NewRequest(http.MethodGet, "/api/graph", nil)
	r2.Host = "127.0.0.1:6061"
	r2.AddCookie(cookies[0])
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, r2)
	if w2.Code != http.StatusOK {
		t.Fatalf("cookie request = %d, want 200", w2.Code)
	}
}
