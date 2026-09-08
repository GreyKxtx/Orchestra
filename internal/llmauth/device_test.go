package llmauth

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/authstore"
	"github.com/orchestra/orchestra/internal/oauthtest"
	"github.com/orchestra/orchestra/llm"
)

func startDeviceAS(t *testing.T) *oauthtest.FakeAuthorizationServer {
	t.Helper()
	as := oauthtest.NewFakeAuthorizationServer(oauthtest.Config{
		IssueRefreshToken: true,
		DeviceFlowEnabled: true,
		RegistrationConfig: &oauthtest.RegistrationConfig{
			PreregisteredClients: map[string]oauthtest.ClientInfo{
				"orchestra-cli": {Secret: ""},
			},
		},
	})
	as.Start(t)
	return as
}

func deviceCfg(as *oauthtest.FakeAuthorizationServer) llm.OAuthConfig {
	return llm.OAuthConfig{
		AuthURL:       as.URL() + "/authorize",
		TokenURL:      as.URL() + "/token",
		DeviceAuthURL: as.URL() + "/device_authorization",
		ClientID:      "orchestra-cli",
		Flow:          "device",
	}
}

func TestLogin_DeviceFlowPrintsTheCodeAndStoresAToken(t *testing.T) {
	isolateHome(t)
	as := startDeviceAS(t)
	var out bytes.Buffer

	if err := Login(context.Background(), LoginConfig{
		Name:  "corp",
		OAuth: deviceCfg(as),
		Out:   &out,
	}); err != nil {
		t.Fatalf("Login: %v", err)
	}

	// The user cannot complete the flow without seeing both of these.
	if !strings.Contains(out.String(), "TEST-CODE") {
		t.Fatalf("output %q must show the user code", out.String())
	}
	if !strings.Contains(out.String(), as.URL()+"/device") {
		t.Fatalf("output %q must show the verification URL", out.String())
	}

	tok, err := authstore.Load(Namespace, "corp")
	if err != nil {
		t.Fatalf("token must be stored after device Login: %v", err)
	}
	if tok.AccessToken != "test_access_token" {
		t.Fatalf("AccessToken = %q, want the fake server's token", tok.AccessToken)
	}
	if tok.TokenURL != as.URL()+"/token" {
		t.Fatalf("TokenURL = %q, want it stored for later refresh", tok.TokenURL)
	}
}

// The fake server answers authorization_pending on the first poll, so a
// Login that gave up on the first non-success answer would fail here.
func TestLogin_DeviceFlowSurvivesAuthorizationPending(t *testing.T) {
	isolateHome(t)
	as := startDeviceAS(t)
	if err := Login(context.Background(), LoginConfig{
		Name:  "corp",
		OAuth: deviceCfg(as),
		Out:   &bytes.Buffer{},
	}); err != nil {
		t.Fatalf("Login must poll through authorization_pending, got %v", err)
	}
	if n := as.DeviceTokenPolls(); n < 2 {
		t.Fatalf("device token endpoint was polled %d times, want at least 2", n)
	}
}

// A device login never opens a browser: the whole point is that the machine
// running Orchestra has none.
func TestLogin_DeviceFlowNeverOpensABrowser(t *testing.T) {
	isolateHome(t)
	as := startDeviceAS(t)
	opened := false
	if err := Login(context.Background(), LoginConfig{
		Name:    "corp",
		OAuth:   deviceCfg(as),
		OpenURL: func(string) error { opened = true; return nil },
		Out:     &bytes.Buffer{},
	}); err != nil {
		t.Fatal(err)
	}
	if opened {
		t.Fatal("device flow must not open a browser")
	}
}
