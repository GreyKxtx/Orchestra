package mcpauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewOAuthHandler_NoStoredTokenIsActionable(t *testing.T) {
	isolateHome(t)
	_, err := NewOAuthHandler(context.Background(), "linear")
	if err == nil || !strings.Contains(err.Error(), "orchestra mcp login linear") {
		t.Fatalf("err = %v, want an actionable 'orchestra mcp login' message", err)
	}
}

func TestOAuthHandlerAdapter_TokenSourceReplaysStoredToken(t *testing.T) {
	isolateHome(t)
	if err := SaveToken("linear", Token{
		TokenURL: "https://auth.example.com/token", AccessToken: "at-1", Expiry: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	h, err := NewOAuthHandler(context.Background(), "linear")
	if err != nil {
		t.Fatal(err)
	}
	ts, err := h.TokenSource(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	tok, err := ts.Token()
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "at-1" {
		t.Fatalf("AccessToken = %q, want at-1", tok.AccessToken)
	}
}

func TestOAuthHandlerAdapter_AuthorizeNeverRunsAnInteractiveFlow(t *testing.T) {
	isolateHome(t)
	if err := SaveToken("linear", Token{
		TokenURL: "https://auth.example.com/token", AccessToken: "at-1", Expiry: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	h, err := NewOAuthHandler(context.Background(), "linear")
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "https://mcp.example.com/", nil)
	resp := &http.Response{StatusCode: http.StatusUnauthorized, Body: http.NoBody, Header: make(http.Header)}
	// This must return an actionable error, not attempt to authorize
	// interactively (no browser, no server it could even reach here) —
	// that is the entire point of the non-interactive adapter.
	err = h.Authorize(context.Background(), req, resp)
	if err == nil || !strings.Contains(err.Error(), "orchestra mcp login linear") {
		t.Fatalf("Authorize err = %v, want the actionable login message", err)
	}
}
