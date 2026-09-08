package llmauth

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/authstore"
	"github.com/orchestra/orchestra/llm"
)

func TestAttach_NoAuthBlockLeavesTokenSourceNil(t *testing.T) {
	cfg := &llm.LLMConfig{APIKey: "static"}
	if err := Attach(context.Background(), "corp", cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.TokenSource != nil {
		t.Fatal("a provider without an auth block must keep TokenSource nil")
	}
}

func TestAttach_TokenCommandProducesAWorkingSource(t *testing.T) {
	argv, _ := counterScript(t, "cmd-token")
	cfg := &llm.LLMConfig{Auth: &llm.AuthConfig{TokenCommand: argv, TokenCommandTTL: time.Minute}}
	if err := Attach(context.Background(), "vertex", cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.TokenSource == nil {
		t.Fatal("TokenSource must be attached for a token_command provider")
	}
	got, err := cfg.TokenSource()
	if err != nil {
		t.Fatal(err)
	}
	if got != "cmd-token" {
		t.Fatalf("token = %q, want cmd-token", got)
	}
}

func TestAttach_OAuthProducesAWorkingSource(t *testing.T) {
	isolateHome(t)
	as := startFakeAS(t)
	if err := authstore.Save(Namespace, "corp", authstore.Token{
		TokenURL:    as.URL() + "/token",
		ClientID:    "orchestra-cli",
		AccessToken: "stored-token",
		Expiry:      time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	oc := oauthCfg(as)
	cfg := &llm.LLMConfig{Auth: &llm.AuthConfig{OAuth: &oc}}
	if err := Attach(context.Background(), "corp", cfg); err != nil {
		t.Fatal(err)
	}
	got, err := cfg.TokenSource()
	if err != nil {
		t.Fatal(err)
	}
	if got != "stored-token" {
		t.Fatalf("token = %q, want the stored one", got)
	}
}

// A provider configured for OAuth but never logged into must still LOAD: the
// error belongs at request time, naming the login command, not at startup
// where it would break every unrelated command -- including the very
// `orchestra auth login` that fixes it.
func TestAttach_OAuthWithoutAStoredTokenDefersTheError(t *testing.T) {
	isolateHome(t)
	as := startFakeAS(t)
	oc := oauthCfg(as)
	cfg := &llm.LLMConfig{Auth: &llm.AuthConfig{OAuth: &oc}}
	if err := Attach(context.Background(), "corp", cfg); err != nil {
		t.Fatalf("Attach must not fail when no token is stored yet: %v", err)
	}
	if cfg.TokenSource == nil {
		t.Fatal("TokenSource must still be attached")
	}
	_, err := cfg.TokenSource()
	if err == nil {
		t.Fatal("calling the source must fail until the user logs in")
	}
	if !strings.Contains(err.Error(), "orchestra auth login corp") {
		t.Fatalf("deferred err = %q, want it to name the login command", err.Error())
	}
}

func TestAttach_RejectsAnInvalidAuthBlock(t *testing.T) {
	cfg := &llm.LLMConfig{Auth: &llm.AuthConfig{}}
	if err := Attach(context.Background(), "corp", cfg); err == nil {
		t.Fatal("expected Attach to reject an auth block with neither mechanism")
	}
}
