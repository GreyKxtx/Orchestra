package webtransport

import (
	"context"
	"net/http"
	"testing"
	"testing/fstest"

	"github.com/orchestra/orchestra/protocol/jsonrpc"
)

func startAssetServer(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	base, stop, err := Serve(ctx, Options{
		Token:  "secret",
		Health: map[string]any{"status": "ok"},
		Assets: fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<h1>hi</h1>")}},
		NewHandler: func() (jsonrpc.Handler, func(*jsonrpc.Server)) { return nil, nil },
	})
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	t.Cleanup(func() { _ = stop() })
	return base
}

// Loading the page with a token must hand the browser a cookie and drop the
// token from the URL, so it never lands in history or the address bar.
func TestCookie_PageLoadSetsCookieAndRedirects(t *testing.T) {
	base := startAssetServer(t)

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Get(base + "/?token=secret")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/" {
		t.Fatalf("Location = %q, want \"/\" — the token must leave the URL", loc)
	}

	var got *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "orchestra" {
			got = c
		}
	}
	if got == nil {
		t.Fatal("no orchestra cookie was set")
	}
	if got.Value != "secret" {
		t.Fatalf("cookie value = %q, want the token", got.Value)
	}
	if !got.HttpOnly {
		t.Fatal("cookie is not HttpOnly — page scripts could read the credential")
	}
	if got.SameSite != http.SameSiteStrictMode {
		t.Fatalf("SameSite = %v, want Strict — this is the CSRF guard for /api/*", got.SameSite)
	}
	if got.Path != "/" {
		t.Fatalf("cookie Path = %q, want \"/\"", got.Path)
	}
}

// After that redirect the browser sends only the cookie. If it does not
// authenticate, the whole flow is broken.
func TestCookie_AloneAuthenticates(t *testing.T) {
	base := startAssetServer(t)

	req, _ := http.NewRequest(http.MethodGet, base+"/health", nil)
	req.AddCookie(&http.Cookie{Name: "orchestra", Value: "secret"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cookie-only request = %d, want 200", resp.StatusCode)
	}
}

func TestCookie_WrongCookieIsRejected(t *testing.T) {
	base := startAssetServer(t)

	req, _ := http.NewRequest(http.MethodGet, base+"/health", nil)
	req.AddCookie(&http.Cookie{Name: "orchestra", Value: "not-the-token"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong cookie = %d, want 401 — any page in the browser can reach 127.0.0.1", resp.StatusCode)
	}
}

func TestCookie_NoCredentialIsRejected(t *testing.T) {
	base := startAssetServer(t)
	resp, err := http.Get(base + "/health")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no credential = %d, want 401", resp.StatusCode)
	}
}
