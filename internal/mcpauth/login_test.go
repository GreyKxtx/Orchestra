package mcpauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"

	"github.com/orchestra/orchestra/internal/mcpauth/oauthtest"
)

// newFakeOAuthProtectedMCPServer starts a real (in-process) MCP Streamable
// HTTP server that requires a bearer token minted by authServer, wired
// exactly the way a real hosted MCP server advertises OAuth: a 401 with a
// WWW-Authenticate header pointing at RFC 9728 protected-resource metadata.
func newFakeOAuthProtectedMCPServer(t *testing.T, authServer *oauthtest.FakeAuthorizationServer) (serverURL string) {
	t.Helper()
	mcpSrv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "fake-remote", Version: "1.0.0"}, nil)
	mcpHandler := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return mcpSrv }, nil)

	verifier := func(_ context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		if token != "test_access_token" {
			return nil, auth.ErrInvalidToken
		}
		return &auth.TokenInfo{Expiration: time.Now().Add(time.Hour)}, nil
	}

	mux := http.NewServeMux()
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	resourceURL := ts.URL + "/mcp"
	metadataURL := ts.URL + "/.well-known/oauth-protected-resource/mcp"
	mux.Handle("/.well-known/oauth-protected-resource/mcp", auth.ProtectedResourceMetadataHandler(&oauthex.ProtectedResourceMetadata{
		Resource:             resourceURL,
		AuthorizationServers: []string{authServer.URL()},
	}))
	mux.Handle("/mcp", auth.RequireBearerToken(verifier, &auth.RequireBearerTokenOptions{
		ResourceMetadataURL: metadataURL,
	})(mcpHandler))

	return resourceURL
}

func TestLogin_CompletesDCRFlowAndPersistsToken(t *testing.T) {
	isolateHome(t)
	authServer := oauthtest.NewFakeAuthorizationServer(oauthtest.Config{
		RegistrationConfig: &oauthtest.RegistrationConfig{DynamicClientRegistrationEnabled: true},
	})
	authServer.Start(t)
	serverURL := newFakeOAuthProtectedMCPServer(t, authServer)

	// openURL simulates the user's browser: a real browser launch is
	// fire-and-forget (it must not block Login while a tab loads), so this
	// visits the URL in its own goroutine. http.Get follows the fake
	// server's redirect straight back to Login's own loopback callback
	// server, completing the round trip synchronously from the caller's
	// point of view.
	openURL := func(u string) error {
		go func() {
			resp, err := http.Get(u)
			if err == nil {
				resp.Body.Close()
			}
		}()
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := Login(ctx, LoginConfig{ServerName: "fake", ServerURL: serverURL, OpenURL: openURL}); err != nil {
		t.Fatalf("Login: %v", err)
	}

	tok, err := LoadToken("fake")
	if err != nil {
		t.Fatalf("LoadToken: %v", err)
	}
	if tok.AccessToken != "test_access_token" {
		t.Fatalf("AccessToken = %q, want test_access_token", tok.AccessToken)
	}
	if tok.TokenURL != authServer.URL()+"/token" {
		t.Fatalf("TokenURL = %q, want %q", tok.TokenURL, authServer.URL()+"/token")
	}
}

func TestLogin_RejectsEmptyServerNameOrURL(t *testing.T) {
	isolateHome(t)
	if err := Login(context.Background(), LoginConfig{ServerURL: "https://x"}); err == nil {
		t.Fatal("empty ServerName must be rejected")
	}
	if err := Login(context.Background(), LoginConfig{ServerName: "x"}); err == nil {
		t.Fatal("empty ServerURL must be rejected")
	}
}

func TestLogout_IsIdempotentForAServerThatWasNeverLoggedIn(t *testing.T) {
	isolateHome(t)
	if err := Logout("never-logged-in"); err != nil {
		t.Fatalf("Logout of an unknown server must not error, got %v", err)
	}
}

func TestLogout_RemovesAStoredToken(t *testing.T) {
	isolateHome(t)
	if err := SaveToken("linear", Token{AccessToken: "at"}); err != nil {
		t.Fatal(err)
	}
	if err := Logout("linear"); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadToken("linear"); err != ErrNoToken {
		t.Fatalf("err = %v, want ErrNoToken after logout", err)
	}
}
