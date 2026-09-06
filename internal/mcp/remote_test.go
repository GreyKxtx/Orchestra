package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/mcpauth"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type echoInput struct {
	Text string `json:"text"`
}

// startTestMCPServer runs a real Streamable HTTP MCP server in-process, so the
// remote client is exercised against the actual protocol rather than a mock of
// our own assumptions about it.
func startTestMCPServer(t *testing.T) (endpoint string, authHeader func() string) {
	t.Helper()
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "test-server", Version: "1.0.0"}, nil)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "echo", Description: "Echo the text back"},
		func(_ context.Context, _ *mcpsdk.CallToolRequest, in echoInput) (*mcpsdk.CallToolResult, any, error) {
			return &mcpsdk.CallToolResult{
				Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "echo:" + in.Text}},
			}, nil, nil
		})
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "secret", Description: "Should be filterable"},
		func(_ context.Context, _ *mcpsdk.CallToolRequest, _ echoInput) (*mcpsdk.CallToolResult, any, error) {
			return &mcpsdk.CallToolResult{
				Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "secret"}},
			}, nil, nil
		})

	handler := mcpsdk.NewStreamableHTTPHandler(
		func(*http.Request) *mcpsdk.Server { return srv }, nil)

	var mu sync.Mutex
	var seenAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		if h := r.Header.Get("Authorization"); h != "" {
			seenAuth = h
		}
		mu.Unlock()
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)

	return ts.URL, func() string {
		mu.Lock()
		defer mu.Unlock()
		return seenAuth
	}
}

// isolateHome mirrors internal/mcpauth's test helper of the same name —
// points os.UserHomeDir at a temp dir so mcpauth.SaveToken/TokenSourceFor
// never touch a developer's real ~/.orchestra.
func isolateHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

func TestStartRemote_OAuthWithNoStoredTokenFailsFastBeforeConnecting(t *testing.T) {
	isolateHome(t)
	_, err := StartRemote(context.Background(), RemoteConfig{
		Name: "test", URL: "https://unreachable.invalid.example/mcp", OAuth: true,
	}, StartOptions{})
	if err == nil || !strings.Contains(err.Error(), "orchestra mcp login test") {
		t.Fatalf("err = %v, want the actionable login message with no network attempt", err)
	}
}

func TestStartRemote_SendsOAuthBearerFromStoredToken(t *testing.T) {
	isolateHome(t)
	endpoint, authHeader := startTestMCPServer(t)
	if err := mcpauth.SaveToken("test", mcpauth.Token{
		TokenURL:    "https://auth.example.com/token", // unused: this token is not expired
		AccessToken: "oauth-token-xyz",
		Expiry:      time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	c, err := StartRemote(context.Background(), RemoteConfig{Name: "test", URL: endpoint, OAuth: true}, StartOptions{})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	if got := authHeader(); got != "Bearer oauth-token-xyz" {
		t.Fatalf("Authorization = %q, want %q", got, "Bearer oauth-token-xyz")
	}
}

func TestStartRemote_ListsAndCallsTools(t *testing.T) {
	endpoint, _ := startTestMCPServer(t)

	c, err := StartRemote(context.Background(), RemoteConfig{Name: "test", URL: endpoint}, StartOptions{})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	names := c.AllToolNames()
	if len(names) != 2 {
		t.Fatalf("expected both tools, got %v", names)
	}
	out, err := c.Call(context.Background(), "echo", json.RawMessage(`{"text":"hi"}`))
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !strings.Contains(out, "echo:hi") {
		t.Fatalf("tool output = %q", out)
	}
	if c.ServerName() != "test" {
		t.Errorf("ServerName = %q", c.ServerName())
	}
}

func TestStartRemote_SendsBearerToken(t *testing.T) {
	endpoint, authHeader := startTestMCPServer(t)
	t.Setenv("ORCH_TEST_MCP_TOKEN", "s3cret")

	c, err := StartRemote(context.Background(), RemoteConfig{
		Name: "test", URL: endpoint, BearerTokenEnv: "ORCH_TEST_MCP_TOKEN",
	}, StartOptions{})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	// Remote servers are the ones that need auth, and a token that never
	// reaches the wire fails as a 401 with no clue why.
	if got := authHeader(); got != "Bearer s3cret" {
		t.Fatalf("Authorization = %q, want %q", got, "Bearer s3cret")
	}
}

func TestStartRemote_AllowlistFiltersTools(t *testing.T) {
	endpoint, _ := startTestMCPServer(t)

	c, err := StartRemote(context.Background(), RemoteConfig{Name: "test", URL: endpoint}, StartOptions{})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	c.SetAllowedTools([]string{"ech*"})

	tools := c.Tools()
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("allowlist must apply to a remote server too, got %+v", tools)
	}
	// Defence in depth: the allowlist also refuses the call, not just the listing.
	if _, err := c.Call(context.Background(), "secret", json.RawMessage(`{}`)); err == nil {
		t.Fatal("a tool outside allowed_tools must be refused")
	}
	// AllToolNames ignores the allowlist so settings UI can re-enable tools.
	if len(c.AllToolNames()) != 2 {
		t.Fatalf("AllToolNames must ignore the allowlist, got %v", c.AllToolNames())
	}
}

func TestStartRemote_ReportsToolErrors(t *testing.T) {
	endpoint, _ := startTestMCPServer(t)

	c, err := StartRemote(context.Background(), RemoteConfig{Name: "test", URL: endpoint}, StartOptions{})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	if _, err := c.Call(context.Background(), "no-such-tool", json.RawMessage(`{}`)); err == nil {
		t.Fatal("calling a tool the server does not have must return an error")
	}
}

func TestNewManager_StartsARemoteServer(t *testing.T) {
	endpoint, _ := startTestMCPServer(t)

	m, errs := NewManager(context.Background(), config.MCPConfig{
		Servers: []config.MCPServerConfig{{Name: "remote", URL: endpoint}},
	})
	t.Cleanup(m.Close)

	if len(errs) != 0 {
		t.Fatalf("unexpected startup errors: %v", errs)
	}
	var names []string
	for _, d := range m.ListToolDefs() {
		names = append(names, d.Function.Name)
	}
	if !slices.Contains(names, "mcp:remote:echo") {
		t.Fatalf("remote tools must reach the agent, got %v", names)
	}

	out, err := m.Call(context.Background(), "mcp:remote:echo", json.RawMessage(`{"text":"hi"}`))
	if err != nil {
		t.Fatalf("routed call: %v", err)
	}
	if !strings.Contains(string(out), "echo:hi") {
		t.Fatalf("routed result = %s", out)
	}
}

func TestNewManager_ReportsServerWithNoTransport(t *testing.T) {
	m, errs := NewManager(context.Background(), config.MCPConfig{
		Servers: []config.MCPServerConfig{{Name: "broken"}},
	})
	t.Cleanup(m.Close)

	// The old filter skipped anything without a command, so a server with a
	// typo'd key vanished without a word and the operator was left wondering
	// where their tools went.
	if len(errs) == 0 {
		t.Fatal("a server with no transport must be reported, not skipped")
	}
	if !strings.Contains(errs[0].Error(), "broken") {
		t.Errorf("error must name the server, got: %v", errs[0])
	}
}

func TestStartRemote_UnreachableEndpointFails(t *testing.T) {
	_, err := StartRemote(context.Background(), RemoteConfig{
		Name: "dead", URL: "http://127.0.0.1:9/mcp",
	}, StartOptions{})

	// Port 9 is the discard port: nothing listens. A remote server that cannot
	// be reached must be reported at startup, not surface later as a missing tool.
	if err == nil {
		t.Fatal("connecting to a dead endpoint must fail")
	}
}
