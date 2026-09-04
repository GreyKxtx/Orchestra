package config

import (
	"strings"
	"testing"
)

func mcpCfg(servers ...MCPServerConfig) *ProjectConfig {
	return &ProjectConfig{MCP: MCPConfig{Servers: servers}}
}

func TestValidateMCP_AcceptsAStdioServer(t *testing.T) {
	err := mcpCfg(MCPServerConfig{Name: "local", Command: []string{"npx", "server"}}).ValidateMCPOnly()
	if err != nil {
		t.Fatalf("a plain stdio server must stay valid: %v", err)
	}
}

func TestValidateMCP_AcceptsARemoteServer(t *testing.T) {
	err := mcpCfg(MCPServerConfig{Name: "github", URL: "https://example.com/mcp"}).ValidateMCPOnly()
	if err != nil {
		t.Fatalf("a url-only server must be valid: %v", err)
	}
}

func TestValidateMCP_RejectsBothCommandAndURL(t *testing.T) {
	err := mcpCfg(MCPServerConfig{
		Name:    "confused",
		Command: []string{"npx", "server"},
		URL:     "https://example.com/mcp",
	}).ValidateMCPOnly()

	// Two transports for one server has no sensible reading, and guessing
	// which one wins is how a config silently connects to the wrong thing.
	if err == nil {
		t.Fatal("a server with both command and url must be rejected")
	}
	if !strings.Contains(err.Error(), "confused") {
		t.Errorf("error must name the server, got: %v", err)
	}
}

func TestValidateMCP_RejectsNeitherCommandNorURL(t *testing.T) {
	err := mcpCfg(MCPServerConfig{Name: "empty"}).ValidateMCPOnly()

	// This is the shape that used to be dropped in silence: the manager
	// skipped anything without a command, so a typo'd key meant the server
	// simply never appeared and nothing said why.
	if err == nil {
		t.Fatal("a server with no transport at all must be rejected")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error must name the server, got: %v", err)
	}
}

func TestValidateMCP_RejectsPlaintextRemoteByDefault(t *testing.T) {
	err := mcpCfg(MCPServerConfig{Name: "insecure", URL: "http://example.com/mcp"}).ValidateMCPOnly()

	// A bearer token over plaintext to a non-loopback host leaks it to the
	// network. Opt in explicitly if you really mean it.
	if err == nil {
		t.Fatal("plaintext http to a remote host must be rejected without allow_insecure_http")
	}
	if !strings.Contains(err.Error(), "allow_insecure_http") {
		t.Errorf("error must name the escape hatch, got: %v", err)
	}
}

func TestValidateMCP_AllowsPlaintextToLoopback(t *testing.T) {
	for _, url := range []string{"http://localhost:3000/mcp", "http://127.0.0.1:3000/mcp"} {
		if err := mcpCfg(MCPServerConfig{Name: "dev", URL: url}).ValidateMCPOnly(); err != nil {
			t.Errorf("a local dev server on %s must be allowed: %v", url, err)
		}
	}
}

func TestValidateMCP_AllowsPlaintextWhenOptedIn(t *testing.T) {
	err := mcpCfg(MCPServerConfig{
		Name:              "internal",
		URL:               "http://mcp.internal/mcp",
		AllowInsecureHTTP: true,
	}).ValidateMCPOnly()
	if err != nil {
		t.Fatalf("allow_insecure_http must permit plaintext: %v", err)
	}
}

func TestValidateMCP_RejectsUnknownURLScheme(t *testing.T) {
	err := mcpCfg(MCPServerConfig{Name: "weird", URL: "ftp://example.com/mcp"}).ValidateMCPOnly()
	if err == nil {
		t.Fatal("only http and https are transports we can speak")
	}
}
