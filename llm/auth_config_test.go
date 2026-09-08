package llm

import (
	"strings"
	"testing"
	"time"
)

func TestAuthConfig_RejectsBothMechanisms(t *testing.T) {
	a := &AuthConfig{
		OAuth:        &OAuthConfig{AuthURL: "https://a/authorize", TokenURL: "https://a/token", ClientID: "c"},
		TokenCommand: []string{"echo", "tok"},
	}
	err := a.Validate()
	if err == nil {
		t.Fatal("expected an error when both oauth and token_command are set")
	}
	if !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("err = %q, want it to say exactly one mechanism", err.Error())
	}
}

func TestAuthConfig_RejectsNeitherMechanism(t *testing.T) {
	if err := (&AuthConfig{}).Validate(); err == nil {
		t.Fatal("expected an error when neither mechanism is set")
	}
}

func TestAuthConfig_DeviceFlowRequiresDeviceAuthURL(t *testing.T) {
	a := &AuthConfig{OAuth: &OAuthConfig{
		AuthURL:  "https://a/authorize",
		TokenURL: "https://a/token",
		ClientID: "c",
		Flow:     "device",
	}}
	err := a.Validate()
	if err == nil {
		t.Fatal("expected an error: flow=device without device_auth_url")
	}
	if !strings.Contains(err.Error(), "device_auth_url") {
		t.Fatalf("err = %q, want it to name device_auth_url", err.Error())
	}
}

func TestAuthConfig_RejectsUnknownFlow(t *testing.T) {
	a := &AuthConfig{OAuth: &OAuthConfig{
		AuthURL: "https://a/authorize", TokenURL: "https://a/token", ClientID: "c", Flow: "magic",
	}}
	if err := a.Validate(); err == nil {
		t.Fatal("expected an error for an unknown flow")
	}
}

func TestAuthConfig_BrowserFlowIsTheDefault(t *testing.T) {
	a := &AuthConfig{OAuth: &OAuthConfig{
		AuthURL: "https://a/authorize", TokenURL: "https://a/token", ClientID: "c",
	}}
	if err := a.Validate(); err != nil {
		t.Fatalf("empty flow must default to browser and validate, got %v", err)
	}
	if got := a.OAuth.EffectiveFlow(); got != "browser" {
		t.Fatalf("EffectiveFlow = %q, want browser", got)
	}
}

func TestAuthConfig_OAuthRequiresEndpointsAndClientID(t *testing.T) {
	for name, oc := range map[string]*OAuthConfig{
		"no auth_url":  {TokenURL: "https://a/token", ClientID: "c"},
		"no token_url": {AuthURL: "https://a/authorize", ClientID: "c"},
		"no client_id": {AuthURL: "https://a/authorize", TokenURL: "https://a/token"},
	} {
		if err := (&AuthConfig{OAuth: oc}).Validate(); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestAuthConfig_EffectiveTokenCommandTTLDefaultsToFiveMinutes(t *testing.T) {
	a := &AuthConfig{TokenCommand: []string{"echo", "tok"}}
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := a.EffectiveTokenCommandTTL(); got != 5*time.Minute {
		t.Fatalf("EffectiveTokenCommandTTL = %v, want 5m", got)
	}
}

func TestAuthConfig_RejectsEmptyTokenCommandProgram(t *testing.T) {
	if err := (&AuthConfig{TokenCommand: []string{"  "}}).Validate(); err == nil {
		t.Fatal("expected an error when token_command's program is blank")
	}
}
