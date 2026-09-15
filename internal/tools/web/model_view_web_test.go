package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/orchestra/orchestra/internal/config"
)

// webfetch had tests for every way it refuses and one for its HTML helper, and
// none for the path the model relies on: fetch a real page over HTTP and get
// readable text back. That was not an oversight so much as a wall — every local
// test server listens on loopback, and the SSRF guard refuses loopback. The
// blockIP seam lets these tests allow exactly 127.0.0.1 and nothing else, so
// the guard stays live for every other address, including where a redirect
// points.

// loopbackOnly allows the local test server and applies the real guard to
// everything else.
func loopbackOnly(ip net.IP) bool {
	if ip.Equal(net.IPv4(127, 0, 0, 1)) {
		return false
	}
	return isBlockedIP(ip)
}

func fetchCfg() Config {
	return Config{FetchTimeout: 5 * time.Second, MaxContentBytes: 512 * 1024, blockIP: loopbackOnly}
}

func serve(t *testing.T, h http.HandlerFunc) string {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv.URL
}

// The job itself: a page comes back as its title and its words. Script and
// style bodies are code the model did not ask for and would read as content.
func TestWebFetch_APageComesBackAsItsTitleAndReadableText(t *testing.T) {
	url := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html><head><title>Retry policy</title>
<style>.x{color:red}</style><script>var trackingSecret = 1;</script></head>
<body><h1>Retries</h1><p>Back off exponentially, cap at 30 seconds.</p></body></html>`)
	})

	resp, err := WebFetch(context.Background(), fetchCfg(), WebFetchRequest{URL: url})
	if err != nil {
		t.Fatalf("webfetch could not fetch a plain page: %v", err)
	}
	if resp.Title != "Retry policy" {
		t.Errorf("title = %q", resp.Title)
	}
	if !strings.Contains(resp.Content, "Back off exponentially") {
		t.Errorf("the page's text is missing:\n%s", resp.Content)
	}
	if strings.Contains(resp.Content, "trackingSecret") || strings.Contains(resp.Content, "color:red") {
		t.Errorf("script or style bodies reached the model as page text:\n%s", resp.Content)
	}
}

// The guard has to hold on every hop, not just the URL the model typed: a
// public page redirecting to an internal address is the classic way around a
// check that only looks at the first URL.
func TestWebFetch_ARedirectToAPrivateAddressIsRefused(t *testing.T) {
	url := serve(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://10.20.30.40/admin", http.StatusFound)
	})

	_, err := webFetchErr(t, WebFetchRequest{URL: url})
	if err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("a redirect into a private range was followed: %v", err)
	}
}

// The description promises "the page as text". A PDF, an image or an archive
// is not text: handing its bytes to the model spends up to MaxContentBytes of
// context on noise, and the model cannot tell it got garbage rather than a
// page. It should be told what it fetched.
func TestWebFetch_BinaryContentIsNamedInsteadOfPassedOnAsText(t *testing.T) {
	for _, tc := range []struct {
		name, contentType string
		body              []byte
	}{
		{"pdf", "application/pdf", append([]byte("%PDF-1.7\n"), 0x00, 0x01, 0xFF, 0xFE, 0x10)},
		{"png", "image/png", []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00}},
		{"zip, no content type", "", []byte{'P', 'K', 0x03, 0x04, 0x14, 0x00, 0x00, 0x00}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			url := serve(t, func(w http.ResponseWriter, r *http.Request) {
				if tc.contentType != "" {
					w.Header().Set("Content-Type", tc.contentType)
				} else {
					// Suppress Go's own sniffing so the response really has none.
					w.Header()["Content-Type"] = nil
				}
				_, _ = w.Write(tc.body)
			})
			resp, err := webFetchErr(t, WebFetchRequest{URL: url})
			if err == nil {
				t.Fatalf("binary content came back as page text (%d bytes):\n%q", len(resp.Content), resp.Content)
			}
			if !strings.Contains(err.Error(), "not text") {
				t.Errorf("the refusal should say the content is not text, so the model does not "+
					"retry the same URL: %v", err)
			}
		})
	}
}

// Text that is not HTML is still text: JSON APIs, plain files, source code.
func TestWebFetch_NonHTMLTextIsStillReturned(t *testing.T) {
	for _, ct := range []string{"text/plain; charset=utf-8", "application/json", "application/xml", "application/vnd.api+json"} {
		t.Run(ct, func(t *testing.T) {
			url := serve(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", ct)
				fmt.Fprint(w, `{"ok": true, "note": "readable"}`)
			})
			resp, err := webFetchErr(t, WebFetchRequest{URL: url})
			if err != nil {
				t.Fatalf("%s was refused though it is text: %v", ct, err)
			}
			if !strings.Contains(resp.Content, "readable") {
				t.Errorf("content lost: %q", resp.Content)
			}
		})
	}
}

// A cut page must say it was cut, or the model reasons about half a document
// as if it were whole.
func TestWebFetch_ACutPageSaysItWasCut(t *testing.T) {
	url := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, strings.Repeat("abcdefghij", 100))
	})
	resp, err := webFetchErr(t, WebFetchRequest{URL: url, MaxBytes: 100})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Truncated {
		t.Error("a 1000-byte page fetched with max_bytes=100 was not marked truncated")
	}
	if len(resp.Content) > 100 {
		t.Errorf("max_bytes was not honoured: %d bytes", len(resp.Content))
	}
}

func TestWebFetch_AnHTTPErrorNamesTheStatus(t *testing.T) {
	url := serve(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	})
	_, err := webFetchErr(t, WebFetchRequest{URL: url})
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("a 404 must say 404, or the model cannot tell a dead link from a broken tool: %v", err)
	}
}

func webFetchErr(t *testing.T, req WebFetchRequest) (*WebFetchResponse, error) {
	t.Helper()
	resp, err := WebFetch(context.Background(), fetchCfg(), req)
	if resp == nil {
		resp = &WebFetchResponse{}
	}
	return resp, err
}

// websearch read the provider's answer through io.LimitReader(64 KB) and THEN
// parsed it. A cap applied before parsing is not a cap on what reaches the
// model; it is a guarantee that any answer over the cap is invalid JSON,
// reported as "brave: parse response: unexpected end of JSON input" — which
// reads like a broken provider rather than a length limit. Providers return
// more per result than the three fields read here (Brave: profile, meta_url,
// thumbnail, extra_snippets …), so a full page of twenty is where the cap is
// plausibly reached. That was NOT observed against the live API — no key and
// no network here — the response below is constructed to exceed it.
func TestWebSearch_ALargeProviderAnswerIsNotAParseError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		results := make([]map[string]any, 0, 20)
		for i := 0; i < 20; i++ {
			results = append(results, map[string]any{
				"title":          fmt.Sprintf("Result %d", i),
				"url":            fmt.Sprintf("https://example.invalid/%d", i),
				"description":    "A short description.",
				"extra_snippets": []string{strings.Repeat("Context that Brave returns alongside. ", 120)},
				"profile":        map[string]any{"name": "Example", "long_name": "example.invalid", "img": "https://example.invalid/favicon.png"},
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"web": map[string]any{"results": results}})
	}))
	t.Cleanup(srv.Close)

	cfg := Config{
		Search:        config.WebSearchConfig{Provider: "brave", APIKey: "k", MaxResults: 20},
		BraveEndpoint: srv.URL,
	}
	resp, err := WebSearch(context.Background(), cfg, WebSearchRequest{Query: "retries", MaxResults: 20})
	if err != nil {
		t.Fatalf("a full page of results failed instead of arriving: %v", err)
	}
	if len(resp.Results) != 20 {
		t.Errorf("got %d of 20 results", len(resp.Results))
	}
}

// Raising the read limit must not unbound what the model receives: that is
// what the old 64 KB was standing in for. Twenty results with a snippet each
// is the whole answer, and a snippet is clipped on a UTF-8 boundary.
func TestWebSearch_WhatReachesTheModelStaysBounded(t *testing.T) {
	long := strings.Repeat("многобайтный текст ", 2000) // ~70 KB, 2-byte runes
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{{"title": "t", "url": "https://example.invalid", "content": long}},
		})
	}))
	t.Cleanup(srv.Close)

	cfg := Config{Search: config.WebSearchConfig{Provider: "tavily", APIKey: "k"}, TavilyEndpoint: srv.URL}
	resp, err := WebSearch(context.Background(), cfg, WebSearchRequest{Query: "q"})
	if err != nil {
		t.Fatal(err)
	}
	snippet := resp.Results[0].Snippet
	if len(snippet) > webSearchMaxSnippetBytes+len("…") {
		t.Errorf("a %d-byte snippet reached the model whole", len(snippet))
	}
	if !utf8.ValidString(snippet) {
		t.Error("the snippet was clipped inside a multi-byte character")
	}
}
