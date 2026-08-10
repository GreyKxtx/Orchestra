package web

import (
	"context"
	"strings"
	"testing"
	"time"
)

func defaultFetchCfg() Config {
	return Config{FetchTimeout: 5 * time.Second, MaxContentBytes: 512 * 1024}
}

func TestWebFetch_EmptyURL(t *testing.T) {
	_, err := WebFetch(context.Background(), defaultFetchCfg(), WebFetchRequest{URL: ""})
	if err == nil || !strings.Contains(err.Error(), "url is empty") {
		t.Fatalf("expected 'url is empty', got: %v", err)
	}
}

func TestWebFetch_BadScheme(t *testing.T) {
	_, err := WebFetch(context.Background(), defaultFetchCfg(), WebFetchRequest{URL: "ftp://example.com/file"})
	if err == nil || !strings.Contains(err.Error(), "unsupported scheme") {
		t.Fatalf("expected 'unsupported scheme', got: %v", err)
	}
}

func TestWebFetch_BlocksLoopback(t *testing.T) {
	_, err := WebFetch(context.Background(), defaultFetchCfg(), WebFetchRequest{URL: "http://127.0.0.1:9/"})
	if err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("expected SSRF block for loopback, got: %v", err)
	}
}

func TestWebFetch_BlocksPrivateClassA(t *testing.T) {
	_, err := WebFetch(context.Background(), defaultFetchCfg(), WebFetchRequest{URL: "http://10.0.0.1/"})
	if err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("expected SSRF block for private class-A, got: %v", err)
	}
}

func TestWebFetch_BlocksPrivateClassC(t *testing.T) {
	_, err := WebFetch(context.Background(), defaultFetchCfg(), WebFetchRequest{URL: "http://192.168.1.1/"})
	if err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("expected SSRF block for private class-C, got: %v", err)
	}
}

func TestWebFetch_BlocksIPv6Loopback(t *testing.T) {
	_, err := WebFetch(context.Background(), defaultFetchCfg(), WebFetchRequest{URL: "http://[::1]:9/"})
	if err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("expected SSRF block for IPv6 loopback, got: %v", err)
	}
}

func TestWebFetch_BlocksLinkLocal(t *testing.T) {
	_, err := WebFetch(context.Background(), defaultFetchCfg(), WebFetchRequest{URL: "http://169.254.0.1/"})
	if err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("expected SSRF block for link-local, got: %v", err)
	}
}
