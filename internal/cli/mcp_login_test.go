package cli

import (
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
)

// isolateHome mirrors internal/config/global_test.go's helper of the same
// name — points os.UserHomeDir at a temp dir so mcpauth's token file and
// ~/.orchestra/config.yml never touch a developer's real home directory.
func isolateHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

func TestFindServerInConfig_NotFound(t *testing.T) {
	_, err := findServerInConfig(config.MCPConfig{}, "missing")
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("err = %v, want it to name the missing server", err)
	}
}

func TestFindServerInConfig_Found(t *testing.T) {
	cfg := config.MCPConfig{Servers: []config.MCPServerConfig{{Name: "linear", URL: "https://mcp.linear.app"}}}
	srv, err := findServerInConfig(cfg, "linear")
	if err != nil {
		t.Fatal(err)
	}
	if srv.URL != "https://mcp.linear.app" {
		t.Errorf("got %+v", srv)
	}
}

func TestRunMCPLogin_RejectsServerWithoutOAuthBlock(t *testing.T) {
	isolateHome(t)
	dir := newMCPCLIFixture(t)
	appendToOrchestraYML(t, dir, "mcp:\n  servers:\n    - name: linear\n      url: https://mcp.linear.app\n")

	err := runMCPLogin(mcpLoginCmd, []string{"linear"})
	if err == nil || !strings.Contains(err.Error(), "oauth") {
		t.Fatalf("err = %v, want it to mention the missing oauth block", err)
	}
}

func TestRunMCPLogin_RejectsUnknownServer(t *testing.T) {
	isolateHome(t)
	newMCPCLIFixture(t)

	err := runMCPLogin(mcpLoginCmd, []string{"ghost"})
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("err = %v, want it to name the missing server", err)
	}
}

func TestRunMCPLogin_RejectsMissingClientSecretEnv(t *testing.T) {
	isolateHome(t)
	dir := newMCPCLIFixture(t)
	appendToOrchestraYML(t, dir, "mcp:\n  servers:\n    - name: linear\n      url: https://mcp.linear.app\n      oauth:\n        client_id: abc\n        client_secret_env: LINEAR_CLIENT_SECRET\n")
	t.Setenv("LINEAR_CLIENT_SECRET", "")

	err := runMCPLogin(mcpLoginCmd, []string{"linear"})
	if err == nil || !strings.Contains(err.Error(), "LINEAR_CLIENT_SECRET") {
		t.Fatalf("err = %v, want it to name the empty env var", err)
	}
}

func TestRunMCPLogout_IsIdempotentForAnUnknownServer(t *testing.T) {
	isolateHome(t)
	if err := runMCPLogout(mcpLogoutCmd, []string{"never-logged-in"}); err != nil {
		t.Fatalf("logout of a server with no stored token must not error, got %v", err)
	}
}

func TestMCPLoginLogoutCmds_RegisteredUnderMCPCommand(t *testing.T) {
	var names []string
	for _, sub := range mcpCmd.Commands() {
		names = append(names, sub.Name())
	}
	for _, want := range []string{"login", "logout"} {
		found := false
		for _, n := range names {
			if n == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%q not registered under the mcp command, got %v", want, names)
		}
	}
}
