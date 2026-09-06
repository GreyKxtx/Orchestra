package mcpauth

import (
	"context"
	"fmt"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"golang.org/x/oauth2"
)

// oauthHandlerAdapter implements auth.OAuthHandler for non-interactive use:
// TokenSource replays a stored (and silently refreshed) token; Authorize
// never runs the interactive browser flow — it returns an actionable error
// instead. This is what stops an unattended `orchestra`/`orchestra core`
// run from ever popping a browser mid-turn: the only place the interactive
// flow runs is the explicit `orchestra mcp login` command (see login.go).
type oauthHandlerAdapter struct {
	serverName  string
	tokenSource oauth2.TokenSource
}

var _ auth.OAuthHandler = (*oauthHandlerAdapter)(nil)

func (h *oauthHandlerAdapter) TokenSource(context.Context) (oauth2.TokenSource, error) {
	return h.tokenSource, nil
}

func (h *oauthHandlerAdapter) Authorize(context.Context, *http.Request, *http.Response) error {
	return fmt.Errorf("mcp server %q: not authenticated, run: orchestra mcp login %s", h.serverName, h.serverName)
}

// NewOAuthHandler builds the auth.OAuthHandler internal/mcp wires into
// StreamableClientTransport.OAuthHandler for a server configured with
// oauth:. It loads (and, if expired, silently refreshes) the stored token
// eagerly, so a server with no stored token fails at connect time with an
// actionable error instead of surfacing as an opaque 401 later.
func NewOAuthHandler(ctx context.Context, serverName string) (auth.OAuthHandler, error) {
	ts, err := TokenSourceFor(ctx, serverName)
	if err != nil {
		return nil, err
	}
	return &oauthHandlerAdapter{serverName: serverName, tokenSource: ts}, nil
}
