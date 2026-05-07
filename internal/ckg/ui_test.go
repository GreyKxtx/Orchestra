package ckg

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
