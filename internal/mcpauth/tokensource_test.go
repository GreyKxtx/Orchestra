package mcpauth

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/mcpauth/oauthtest"
	"golang.org/x/oauth2"
)

func TestTokenSourceFor_NoStoredTokenIsActionable(t *testing.T) {
	isolateHome(t)
	_, err := TokenSourceFor(context.Background(), "linear")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), `run: orchestra mcp login linear`) {
		t.Fatalf("err = %q, want it to name the login command", err.Error())
	}
}

func TestTokenSourceFor_ReturnsStoredTokenUnchangedWhenNotExpired(t *testing.T) {
	isolateHome(t)
	if err := SaveToken("linear", Token{
		TokenURL:    "https://auth.example.com/token",
		AccessToken: "still-good",
		Expiry:      time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	ts, err := TokenSourceFor(context.Background(), "linear")
	if err != nil {
		t.Fatal(err)
	}
	tok, err := ts.Token()
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "still-good" {
		t.Fatalf("AccessToken = %q, want the stored token unchanged", tok.AccessToken)
	}
}

func TestTokenSourceFor_RefreshesExpiredTokenAndPersists(t *testing.T) {
	isolateHome(t)
	authServer := oauthtest.NewFakeAuthorizationServer(oauthtest.Config{
		AccessTokenTTL:    1,
		IssueRefreshToken: true,
		RegistrationConfig: &oauthtest.RegistrationConfig{
			PreregisteredClients: map[string]oauthtest.ClientInfo{
				"test-client": {Secret: "test-secret", RedirectURIs: []string{"http://127.0.0.1/callback"}},
			},
		},
	})
	authServer.Start(t)

	// The refresh token value must match the fake server's fixed accepted
	// value (oauthtest.testRefreshToken, unexported but stably
	// "test_refresh_token" — see fake_authorization_server.go's
	// handleRefreshTokenGrant), since that's what a real authorization
	// server would have originally issued alongside this access token.
	if err := SaveToken("linear", Token{
		TokenURL:     authServer.URL() + "/token",
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		AccessToken:  "expired-access-token",
		RefreshToken: "test_refresh_token",
		Expiry:       time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	ts, err := TokenSourceFor(context.Background(), "linear")
	if err != nil {
		t.Fatal(err)
	}
	tok, err := ts.Token()
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if tok.AccessToken != "test_access_token_refreshed" {
		t.Fatalf("AccessToken = %q, want the fake server's refreshed token", tok.AccessToken)
	}

	persisted, err := LoadToken("linear")
	if err != nil {
		t.Fatal(err)
	}
	if persisted.AccessToken != "test_access_token_refreshed" {
		t.Fatalf("refreshed token must be persisted to disk, got %+v", persisted)
	}
	if persisted.RefreshToken != "test_refresh_token" {
		t.Fatalf("refresh token must survive the refresh, got %q", persisted.RefreshToken)
	}
}

// stubTokenSource is a hand-written test double for the one case that needs
// to control exactly what oauth2 token comes back without a real HTTP
// round trip: proving persistingTokenSource falls back to the previous
// refresh token when a refresh response omits one (some authorization
// servers do this, meaning the original refresh token is still valid).
type stubTokenSource struct{ tok *oauth2.Token }

func (s stubTokenSource) Token() (*oauth2.Token, error) { return s.tok, nil }

func TestPersistingTokenSource_KeepsOldRefreshTokenWhenResponseOmitsIt(t *testing.T) {
	isolateHome(t)
	last := Token{ClientID: "c", TokenURL: "https://x/token", AccessToken: "old", RefreshToken: "keep-me"}
	if err := SaveToken("linear", last); err != nil {
		t.Fatal(err)
	}

	ts := newPersistingTokenSource("linear", stubTokenSource{tok: &oauth2.Token{AccessToken: "new"}}, last)
	if _, err := ts.Token(); err != nil {
		t.Fatal(err)
	}

	got, err := LoadToken("linear")
	if err != nil {
		t.Fatal(err)
	}
	if got.RefreshToken != "keep-me" {
		t.Fatalf("RefreshToken = %q, want preserved %q", got.RefreshToken, "keep-me")
	}
	if got.AccessToken != "new" {
		t.Fatalf("AccessToken = %q, want new", got.AccessToken)
	}
}
