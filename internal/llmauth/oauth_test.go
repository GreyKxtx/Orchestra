package llmauth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/authstore"
	"github.com/orchestra/orchestra/internal/oauthtest"
	"github.com/orchestra/orchestra/llm"
)

func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

// startFakeAS starts a fake authorization server with one public client
// (empty secret), which is what a PKCE CLI client looks like.
func startFakeAS(t *testing.T) *oauthtest.FakeAuthorizationServer {
	t.Helper()
	as := oauthtest.NewFakeAuthorizationServer(oauthtest.Config{
		IssueRefreshToken: true,
		RegistrationConfig: &oauthtest.RegistrationConfig{
			PreregisteredClients: map[string]oauthtest.ClientInfo{
				"orchestra-cli": {Secret: "", RedirectURIs: []string{"http://127.0.0.1/callback"}},
			},
		},
	})
	as.Start(t)
	return as
}

func oauthCfg(as *oauthtest.FakeAuthorizationServer) llm.OAuthConfig {
	return llm.OAuthConfig{
		AuthURL:  as.URL() + "/authorize",
		TokenURL: as.URL() + "/token",
		ClientID: "orchestra-cli",
		Scopes:   []string{"llm.invoke"},
	}
}

// visitingOpener follows the authorization URL the way a browser would, so
// the loopback receiver gets a real redirect. It must not block the caller:
// Login has to reach its select before the handler can hand off.
func visitingOpener() func(string) error {
	return func(u string) error {
		go func() {
			resp, err := http.Get(u)
			if err == nil {
				resp.Body.Close()
			}
		}()
		return nil
	}
}

func TestLogin_BrowserFlowStoresAToken(t *testing.T) {
	isolateHome(t)
	as := startFakeAS(t)

	err := Login(context.Background(), LoginConfig{
		Name:    "corp",
		OAuth:   oauthCfg(as),
		OpenURL: visitingOpener(),
		Out:     io.Discard,
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	tok, err := authstore.Load(Namespace, "corp")
	if err != nil {
		t.Fatalf("token must be stored after Login: %v", err)
	}
	if tok.AccessToken != "test_access_token" {
		t.Fatalf("AccessToken = %q, want the fake server's token", tok.AccessToken)
	}
	if tok.RefreshToken == "" {
		t.Fatal("a refresh token must be requested and stored")
	}
	if tok.TokenURL != as.URL()+"/token" {
		t.Fatalf("TokenURL = %q, want it stored for later refresh", tok.TokenURL)
	}
	if tok.ClientID != "orchestra-cli" {
		t.Fatalf("ClientID = %q, want it stored for later refresh", tok.ClientID)
	}
}

func TestLogin_RejectsMismatchedState(t *testing.T) {
	isolateHome(t)
	as := startFakeAS(t)

	// Hit the loopback receiver directly with a wrong state, bypassing
	// /authorize entirely -- this is the shape of a CSRF attempt.
	opener := func(u string) error {
		parsed, err := url.Parse(u)
		if err != nil {
			return err
		}
		redirect := parsed.Query().Get("redirect_uri")
		go func() {
			resp, err := http.Get(redirect + "?code=abc&state=wrong")
			if err == nil {
				resp.Body.Close()
			}
		}()
		return nil
	}

	err := Login(context.Background(), LoginConfig{
		Name: "corp", OAuth: oauthCfg(as), OpenURL: opener, Out: io.Discard,
	})
	if err == nil {
		t.Fatal("expected Login to reject a mismatched state")
	}
	if !strings.Contains(err.Error(), "state mismatch") {
		t.Fatalf("err = %q, want it to name the state mismatch", err.Error())
	}
}

func TestLogin_RejectsEmptyProviderName(t *testing.T) {
	isolateHome(t)
	as := startFakeAS(t)
	if err := Login(context.Background(), LoginConfig{OAuth: oauthCfg(as)}); err == nil {
		t.Fatal("expected an error for an empty provider name")
	}
}

func TestTokenSourceFor_NoStoredTokenIsActionable(t *testing.T) {
	isolateHome(t)
	as := startFakeAS(t)
	_, err := TokenSourceFor(context.Background(), "corp", oauthCfg(as))
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "orchestra auth login corp") {
		t.Fatalf("err = %q, want it to name the login command", err.Error())
	}
}

func TestTokenSourceFor_ReturnsStoredTokenUnchangedWhenNotExpired(t *testing.T) {
	isolateHome(t)
	as := startFakeAS(t)
	if err := authstore.Save(Namespace, "corp", authstore.Token{
		TokenURL:    as.URL() + "/token",
		ClientID:    "orchestra-cli",
		AccessToken: "still-good",
		Expiry:      time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	src, err := TokenSourceFor(context.Background(), "corp", oauthCfg(as))
	if err != nil {
		t.Fatal(err)
	}
	got, err := src()
	if err != nil {
		t.Fatal(err)
	}
	if got != "still-good" {
		t.Fatalf("token = %q, want the stored one unchanged", got)
	}
}

func TestTokenSourceFor_RefreshesExpiredTokenAndPersists(t *testing.T) {
	isolateHome(t)
	as := startFakeAS(t)
	if err := authstore.Save(Namespace, "corp", authstore.Token{
		TokenURL:     as.URL() + "/token",
		ClientID:     "orchestra-cli",
		AccessToken:  "expired-access-token",
		RefreshToken: "test_refresh_token",
		Expiry:       time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	src, err := TokenSourceFor(context.Background(), "corp", oauthCfg(as))
	if err != nil {
		t.Fatal(err)
	}
	got, err := src()
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if got != "test_access_token_refreshed" {
		t.Fatalf("token = %q, want the refreshed one", got)
	}

	persisted, err := authstore.Load(Namespace, "corp")
	if err != nil {
		t.Fatal(err)
	}
	if persisted.AccessToken != "test_access_token_refreshed" {
		t.Fatalf("refreshed token must be persisted, got %+v", persisted)
	}
}

// A refresh that fails must name the login command: that error reaches the
// user through a chat request, where "401" alone tells them nothing.
func TestTokenSourceFor_RefreshFailureNamesTheLoginCommand(t *testing.T) {
	isolateHome(t)
	as := startFakeAS(t)
	if err := authstore.Save(Namespace, "corp", authstore.Token{
		TokenURL:     as.URL() + "/token",
		ClientID:     "orchestra-cli",
		AccessToken:  "expired-access-token",
		RefreshToken: "no-longer-valid",
		Expiry:       time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	src, err := TokenSourceFor(context.Background(), "corp", oauthCfg(as))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := src(); err == nil {
		t.Fatal("expected the refresh to fail")
	} else if !strings.Contains(err.Error(), "orchestra auth login corp") {
		t.Fatalf("err = %q, want it to name the login command", err.Error())
	}
}

func TestLogout_RemovesTheStoredTokenAndIsIdempotent(t *testing.T) {
	isolateHome(t)
	if err := authstore.Save(Namespace, "corp", authstore.Token{AccessToken: "at"}); err != nil {
		t.Fatal(err)
	}
	if err := Logout("corp"); err != nil {
		t.Fatalf("first logout: %v", err)
	}
	if _, err := authstore.Load(Namespace, "corp"); !errors.Is(err, authstore.ErrNoToken) {
		t.Fatalf("token must be gone, got err = %v", err)
	}
	if err := Logout("corp"); err != nil {
		t.Fatalf("second logout must be a no-op, got %v", err)
	}
}
