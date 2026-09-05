package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMCPServeCmd_RegisteredUnderMCPCommand(t *testing.T) {
	found := false
	for _, sub := range mcpCmd.Commands() {
		if sub.Name() == "serve" {
			found = true
		}
	}
	if !found {
		t.Fatal(`"orchestra mcp serve" is not registered under the "mcp" command`)
	}
}

func TestMCPServeCmd_Flags(t *testing.T) {
	for _, name := range []string{"workspace-root", "http", "http-addr", "mcp-token"} {
		if mcpServeCmd.Flags().Lookup(name) == nil {
			t.Errorf("missing --%s flag", name)
		}
	}
}

func TestMCPServeCmd_HTTPAddrDefaultsToLoopback(t *testing.T) {
	flag := mcpServeCmd.Flags().Lookup("http-addr")
	if flag == nil {
		t.Fatal("--http-addr flag not registered")
	}
	if !strings.HasPrefix(flag.DefValue, "127.0.0.1:") {
		t.Errorf("--http-addr default = %q, want a 127.0.0.1 default", flag.DefValue)
	}
}

func TestResolveMCPToken_MissingReturnsError(t *testing.T) {
	t.Setenv("ORCH_MCP_TOKEN", "")
	if _, err := resolveMCPToken(""); err == nil {
		t.Fatal("expected an error when no token is provided by flag or env")
	}
}

func TestResolveMCPToken_FallsBackToEnv(t *testing.T) {
	t.Setenv("ORCH_MCP_TOKEN", "from-env")
	got, err := resolveMCPToken("")
	if err != nil {
		t.Fatal(err)
	}
	if got != "from-env" {
		t.Errorf("token = %q, want from-env", got)
	}
}

func TestResolveMCPToken_FlagWinsOverEnv(t *testing.T) {
	t.Setenv("ORCH_MCP_TOKEN", "from-env")
	got, err := resolveMCPToken("from-flag")
	if err != nil {
		t.Fatal(err)
	}
	if got != "from-flag" {
		t.Errorf("token = %q, want from-flag", got)
	}
}

func TestRequireBearerToken_RejectsWrongOrMissingToken(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	handler := requireBearerToken("secret", next)

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no Authorization header: status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	req.Header.Set("Authorization", "Bearer wrong")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong token: status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("correct token: status = %d, want %d", rec.Code, http.StatusOK)
	}
}
