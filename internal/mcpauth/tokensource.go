package mcpauth

import (
	"context"
	"errors"
	"fmt"

	"github.com/orchestra/orchestra/internal/authstore"
	"golang.org/x/oauth2"
)

// newPersistingTokenSource wraps base so a refreshed token is written back
// under this server's name.
func newPersistingTokenSource(serverName string, base oauth2.TokenSource, last Token) oauth2.TokenSource {
	return authstore.NewPersistingTokenSource(namespace, serverName, base, last)
}

// TokenSourceFor returns a token source for serverName that transparently
// refreshes an expired access token (no browser, no prompt) and persists the
// refreshed token back to disk. Returns an actionable error if nothing is
// stored yet.
func TokenSourceFor(ctx context.Context, serverName string) (oauth2.TokenSource, error) {
	tok, err := LoadToken(serverName)
	if err != nil {
		if errors.Is(err, ErrNoToken) {
			return nil, fmt.Errorf("mcp server %q: not authenticated, run: orchestra mcp login %s", serverName, serverName)
		}
		return nil, err
	}

	cfg := &oauth2.Config{
		ClientID:     tok.ClientID,
		ClientSecret: tok.ClientSecret,
		Endpoint:     oauth2.Endpoint{TokenURL: tok.TokenURL},
	}
	base := cfg.TokenSource(ctx, &oauth2.Token{
		AccessToken:  tok.AccessToken,
		TokenType:    tok.TokenType,
		RefreshToken: tok.RefreshToken,
		Expiry:       tok.Expiry,
	})
	return newPersistingTokenSource(serverName, base, tok), nil
}
