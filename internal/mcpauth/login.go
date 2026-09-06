package mcpauth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/auth"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/oauth2"
)

// OpenURLFunc opens a URL in the user's default browser. Real logins use
// openBrowser (OS-specific, fire-and-forget — see its doc comment for why
// it must not block); tests inject a fake that never shells out.
type OpenURLFunc func(url string) error

// LoginConfig configures one interactive OAuth login.
type LoginConfig struct {
	// ServerName identifies the MCP server. The resulting token is stored
	// under this name (SaveToken) and later read back by TokenSourceFor.
	ServerName string
	// ServerURL is the MCP server's remote (Streamable HTTP) endpoint.
	ServerURL string
	// ClientID, if set, selects a preregistered client instead of Dynamic
	// Client Registration.
	ClientID string
	// ClientSecret is the preregistered client's secret, if it has one.
	// Only meaningful together with ClientID; empty means a public client.
	ClientSecret string
	// Scopes are requested explicitly, for Dynamic Client Registration
	// only (see MCPServerOAuthConfig.Scopes in internal/config).
	Scopes []string
	// OpenURL opens the authorization URL in a browser. nil uses
	// openBrowser, the real OS-specific opener.
	OpenURL OpenURLFunc
}

// codeReceiver runs a temporary loopback HTTP server to receive the OAuth
// redirect, mirroring the SDK's own examples/auth/client/main.go reference
// pattern: the SDK's auth.AuthorizationCodeHandler implements the whole
// authorization-code-with-PKCE flow; this is just the redirect catcher a
// caller is expected to supply.
type codeReceiver struct {
	openURL  OpenURLFunc
	authChan chan *auth.AuthorizationResult
	errChan  chan error
	server   *http.Server
}

func (r *codeReceiver) serve(ln net.Listener) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		r.authChan <- &auth.AuthorizationResult{
			Code:  req.URL.Query().Get("code"),
			State: req.URL.Query().Get("state"),
			Iss:   req.URL.Query().Get("iss"),
		}
		fmt.Fprint(w, "Authentication successful. You can close this window.")
	})
	r.server = &http.Server{Handler: mux}
	if err := r.server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		r.errChan <- err
	}
}

// getAuthorizationCode is the auth.AuthorizationCodeFetcher passed to the
// SDK's handler. openURL must return as soon as the browser process starts
// — it must NOT wait for the browser to finish loading the page: this
// goroutine needs to reach the select below before the loopback handler
// above can hand off on authChan (an unbuffered channel), so a synchronous
// "visit and wait for the whole redirect chain" openURL would deadlock
// against its own server.
func (r *codeReceiver) getAuthorizationCode(ctx context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
	if err := r.openURL(args.URL); err != nil {
		return nil, fmt.Errorf("open browser: %w", err)
	}
	select {
	case res := <-r.authChan:
		return res, nil
	case err := <-r.errChan:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (r *codeReceiver) close() {
	if r.server != nil {
		_ = r.server.Close()
	}
}

// Login runs the interactive OAuth 2.1 authorization-code-with-PKCE flow
// for one MCP server and, on success, persists the resulting token via
// SaveToken. It is the ONLY place in Orchestra that ever opens a browser —
// every other path (TokenSourceFor, NewOAuthHandler, StartRemote) fails
// with an actionable error instead.
func Login(ctx context.Context, cfg LoginConfig) error {
	if strings.TrimSpace(cfg.ServerName) == "" {
		return errors.New("mcpauth: server name is required")
	}
	if strings.TrimSpace(cfg.ServerURL) == "" {
		return errors.New("mcpauth: server url is required")
	}
	openURL := cfg.OpenURL
	if openURL == nil {
		openURL = openBrowser
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("mcpauth: open a local port for the OAuth redirect: %w", err)
	}
	redirectURL := fmt.Sprintf("http://%s/", ln.Addr().String())

	receiver := &codeReceiver{
		openURL:  openURL,
		authChan: make(chan *auth.AuthorizationResult),
		errChan:  make(chan error, 1),
	}
	go receiver.serve(ln)
	defer receiver.close()

	handlerCfg := &auth.AuthorizationCodeHandlerConfig{
		RedirectURL:              redirectURL,
		AuthorizationCodeFetcher: receiver.getAuthorizationCode,
		RequestRefreshToken:      true,
		NewTokenSource: func(tsCtx context.Context, oc *oauth2.Config, tok *oauth2.Token) (oauth2.TokenSource, error) {
			stored := Token{
				TokenURL:     oc.Endpoint.TokenURL,
				ClientID:     oc.ClientID,
				ClientSecret: oc.ClientSecret,
				AccessToken:  tok.AccessToken,
				TokenType:    tok.TokenType,
				RefreshToken: tok.RefreshToken,
				Expiry:       tok.Expiry,
			}
			if err := SaveToken(cfg.ServerName, stored); err != nil {
				return nil, fmt.Errorf("save token: %w", err)
			}
			return newPersistingTokenSource(cfg.ServerName, oc.TokenSource(tsCtx, tok), stored), nil
		},
	}
	if cfg.ClientID != "" {
		var secretAuth *oauthex.ClientSecretAuth
		if cfg.ClientSecret != "" {
			secretAuth = &oauthex.ClientSecretAuth{ClientSecret: cfg.ClientSecret}
		}
		handlerCfg.PreregisteredClient = &oauthex.ClientCredentials{
			ClientID:         cfg.ClientID,
			ClientSecretAuth: secretAuth,
		}
	} else {
		handlerCfg.DynamicClientRegistrationConfig = &auth.DynamicClientRegistrationConfig{
			Metadata: &oauthex.ClientRegistrationMetadata{
				ClientName:   "Orchestra",
				RedirectURIs: []string{redirectURL},
				GrantTypes:   []string{"authorization_code", "refresh_token"},
				Scope:        strings.Join(cfg.Scopes, " "),
			},
		}
	}

	authHandler, err := auth.NewAuthorizationCodeHandler(handlerCfg)
	if err != nil {
		return fmt.Errorf("mcpauth: configure oauth handler: %w", err)
	}

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "orchestra", Version: "vnext"}, nil)
	session, err := client.Connect(ctx, &mcpsdk.StreamableClientTransport{
		Endpoint:     cfg.ServerURL,
		OAuthHandler: authHandler,
	}, nil)
	if err != nil {
		return fmt.Errorf("mcp server %q: authorize: %w", cfg.ServerName, err)
	}
	_ = session.Close()
	return nil
}

// Logout deletes the stored token for a server. Idempotent — logging out
// of a server that was never logged in is not an error.
func Logout(serverName string) error {
	return DeleteToken(serverName)
}

// openBrowser launches the OS default browser at url without waiting for
// it to exit — see the comment on codeReceiver.getAuthorizationCode for
// why that matters. Not covered by tests: actually opening a browser
// cannot run in CI, which is exactly why OpenURLFunc exists as a seam.
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
