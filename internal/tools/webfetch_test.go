package tools

import (
	"context"
	"strings"
	"testing"
)

func newWebRunner(t *testing.T) *Runner {
	t.Helper()
	root := t.TempDir()
	r, err := NewRunner(root, RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

func TestWebFetch_EmptyURL(t *testing.T) {
	r := newWebRunner(t)
	_, err := r.WebFetch(context.Background(), WebFetchRequest{URL: ""})
	if err == nil || !strings.Contains(err.Error(), "url is empty") {
		t.Fatalf("expected 'url is empty', got: %v", err)
	}
}

func TestWebFetch_BadScheme(t *testing.T) {
	r := newWebRunner(t)
	_, err := r.WebFetch(context.Background(), WebFetchRequest{URL: "ftp://example.com/file"})
	if err == nil || !strings.Contains(err.Error(), "unsupported scheme") {
		t.Fatalf("expected 'unsupported scheme', got: %v", err)
	}
}

func TestWebFetch_BlocksLoopback(t *testing.T) {
	r := newWebRunner(t)
	_, err := r.WebFetch(context.Background(), WebFetchRequest{URL: "http://127.0.0.1:9/"})
	if err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("expected SSRF block for loopback, got: %v", err)
	}
}

func TestWebFetch_BlocksPrivateClassA(t *testing.T) {
	r := newWebRunner(t)
	_, err := r.WebFetch(context.Background(), WebFetchRequest{URL: "http://10.0.0.1/"})
	if err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("expected SSRF block for private class-A, got: %v", err)
	}
}

func TestWebFetch_BlocksPrivateClassC(t *testing.T) {
	r := newWebRunner(t)
	_, err := r.WebFetch(context.Background(), WebFetchRequest{URL: "http://192.168.1.1/"})
	if err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("expected SSRF block for private class-C, got: %v", err)
	}
}

func TestWebFetch_BlocksIPv6Loopback(t *testing.T) {
	r := newWebRunner(t)
	_, err := r.WebFetch(context.Background(), WebFetchRequest{URL: "http://[::1]:9/"})
	if err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("expected SSRF block for IPv6 loopback, got: %v", err)
	}
}

func TestWebFetch_BlocksLinkLocal(t *testing.T) {
	r := newWebRunner(t)
	_, err := r.WebFetch(context.Background(), WebFetchRequest{URL: "http://169.254.0.1/"})
	if err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("expected SSRF block for link-local, got: %v", err)
	}
}

// TestWebFetch_HTMLExtraction verifies HTML → text extraction without network.
func TestWebFetch_HTMLExtraction(t *testing.T) {
	html := `<html><head><title>My Page</title></head><body>
<script>alert('skip me')</script>
<p>Hello world</p>
<p>Second paragraph</p>
</body></html>`

	title, content := extractTextFromHTML(html)
	if title != "My Page" {
		t.Fatalf("expected title 'My Page', got %q", title)
	}
	if !strings.Contains(content, "Hello world") {
		t.Fatalf("content should contain 'Hello world', got: %q", content)
	}
	if strings.Contains(content, "alert") {
		t.Fatalf("script content should be stripped, got: %q", content)
	}
}

func TestWebFetch_IsLikelyHTML(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{`<html><body>hi</body></html>`, true},
		{`<!DOCTYPE html><html>`, true},
		{`<head><title>t</title></head>`, true},
		{`<body>content</body>`, true},
		{`{"json": true}`, false},
		{`plain text content`, false},
	}
	for _, tc := range cases {
		got := isLikelyHTML([]byte(tc.input))
		if got != tc.want {
			t.Errorf("isLikelyHTML(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}
