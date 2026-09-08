package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/authstore"
	"github.com/orchestra/orchestra/internal/llmauth"
	"github.com/orchestra/orchestra/llm"
)

// chdirWithConfig writes a project config into a temp dir and makes it the
// working directory, the way loadProjectConfig (internal/cli/model.go:106)
// expects to find it. Mirrors TestSessionExportImportCLI's setup.
func chdirWithConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".orchestra.yml"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(wd) })
	return dir
}

const plainConfig = "project_root: .\nllm:\n  api_base: http://x/v1\n  model: m\n"

func TestAuthLogin_RejectsUnknownProvider(t *testing.T) {
	isolateHome(t)
	chdirWithConfig(t, plainConfig)
	err := runAuthLogin(nil, []string{"nope"})
	if err == nil {
		t.Fatal("expected an error for a provider absent from config")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Fatalf("err = %q, want it to name the provider", err.Error())
	}
}

func TestAuthLogin_RejectsProviderWithoutAuthBlock(t *testing.T) {
	isolateHome(t)
	chdirWithConfig(t, `
project_root: .
llm:
  api_base: http://x/v1
  model: m
providers:
  plain:
    api_base: http://y/v1
    model: m
`)
	err := runAuthLogin(nil, []string{"plain"})
	if err == nil {
		t.Fatal("expected an error for a provider with no auth block")
	}
	if !strings.Contains(err.Error(), "auth:") {
		t.Fatalf("err = %q, want it to say what to add", err.Error())
	}
}

func TestAuthLogin_RejectsTokenCommandProvider(t *testing.T) {
	isolateHome(t)
	chdirWithConfig(t, `
project_root: .
llm:
  api_base: http://x/v1
  model: m
providers:
  vertex:
    api_base: http://y/v1
    model: m
    auth:
      token_command: ["echo", "tok"]
`)
	err := runAuthLogin(nil, []string{"vertex"})
	if err == nil {
		t.Fatal("token_command providers have nothing to log into; expected an error")
	}
	if !strings.Contains(err.Error(), "token_command") {
		t.Fatalf("err = %q, want it to explain that token_command needs no login", err.Error())
	}
}

func TestAuthLogout_RemovesTheToken(t *testing.T) {
	isolateHome(t)
	chdirWithConfig(t, plainConfig)
	if err := authstore.Save(llmauth.Namespace, "corp", authstore.Token{AccessToken: "at"}); err != nil {
		t.Fatal(err)
	}
	if err := runAuthLogout(nil, []string{"corp"}); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, err := authstore.Load(llmauth.Namespace, "corp"); err == nil {
		t.Fatal("token must be gone after logout")
	}
}

func TestAuthStatusLine_ReportsEachMechanism(t *testing.T) {
	isolateHome(t)

	if got := authStatusLine("plain", llm.LLMConfig{APIKey: "sk-abcdef"}); !strings.Contains(got, "key=") {
		t.Fatalf("static key line = %q, want it to show a redacted key", got)
	}
	if got := authStatusLine("none", llm.LLMConfig{}); !strings.Contains(got, "no credential") {
		t.Fatalf("empty line = %q, want it to say there is no credential", got)
	}

	cmd := llm.LLMConfig{Auth: &llm.AuthConfig{TokenCommand: []string{"echo", "x"}}}
	if got := authStatusLine("vertex", cmd); !strings.Contains(got, "token_command") {
		t.Fatalf("command line = %q, want it to name token_command", got)
	}

	oauthed := llm.LLMConfig{Auth: &llm.AuthConfig{OAuth: &llm.OAuthConfig{
		AuthURL: "https://a/authorize", TokenURL: "https://a/token", ClientID: "c",
	}}}
	if got := authStatusLine("corp", oauthed); !strings.Contains(got, "orchestra auth login corp") {
		t.Fatalf("logged-out line = %q, want it to name the login command", got)
	}

	if err := authstore.Save(llmauth.Namespace, "corp", authstore.Token{
		AccessToken: "at", Expiry: time.Now().Add(42 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	got := authStatusLine("corp", oauthed)
	if !strings.Contains(got, "oauth") || !strings.Contains(got, "expires in") {
		t.Fatalf("logged-in line = %q, want it to show oauth and the expiry", got)
	}
}

// An expired access token with a refresh token is not a problem the user has
// to act on -- telling them to log in again would be wrong.
func TestAuthStatusLine_ExpiredButRefreshableIsNotAnAlarm(t *testing.T) {
	isolateHome(t)
	oauthed := llm.LLMConfig{Auth: &llm.AuthConfig{OAuth: &llm.OAuthConfig{
		AuthURL: "https://a/authorize", TokenURL: "https://a/token", ClientID: "c",
	}}}
	if err := authstore.Save(llmauth.Namespace, "corp", authstore.Token{
		AccessToken:  "at",
		RefreshToken: "rt",
		Expiry:       time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	got := authStatusLine("corp", oauthed)
	if strings.Contains(got, "orchestra auth login") {
		t.Fatalf("line = %q, want no call to action while a refresh token is held", got)
	}
	if !strings.Contains(got, "refresh") {
		t.Fatalf("line = %q, want it to say the token will refresh", got)
	}
}
