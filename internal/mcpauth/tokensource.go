package mcpauth

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"golang.org/x/oauth2"
)

// persistingTokenSource wraps an oauth2.TokenSource, persisting a refreshed
// token back to disk immediately so a later process start reuses it
// instead of finding a stale access token and needing a fresh discovery
// round trip.
type persistingTokenSource struct {
	serverName string
	base       oauth2.TokenSource

	mu   sync.Mutex
	last Token
}

func newPersistingTokenSource(serverName string, base oauth2.TokenSource, last Token) oauth2.TokenSource {
	return &persistingTokenSource{serverName: serverName, base: base, last: last}
}

func (p *persistingTokenSource) Token() (*oauth2.Token, error) {
	t, err := p.base.Token()
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if t.AccessToken == p.last.AccessToken {
		return t, nil // base didn't refresh; nothing new to persist
	}

	refreshToken := t.RefreshToken
	if refreshToken == "" {
		// Some authorization servers omit refresh_token on a refresh
		// response, meaning the original refresh token is still valid and
		// must keep being reused. Losing it here would silently turn every
		// later refresh into an authentication failure.
		refreshToken = p.last.RefreshToken
	}
	next := Token{
		TokenURL:     p.last.TokenURL,
		ClientID:     p.last.ClientID,
		ClientSecret: p.last.ClientSecret,
		AccessToken:  t.AccessToken,
		TokenType:    t.TokenType,
		RefreshToken: refreshToken,
		Expiry:       t.Expiry,
	}
	if err := SaveToken(p.serverName, next); err != nil {
		return nil, fmt.Errorf("mcpauth: persist refreshed token for %q: %w", p.serverName, err)
	}
	p.last = next
	return t, nil
}

// TokenSourceFor returns a token source for serverName that transparently
// refreshes an expired access token (no browser, no prompt) and persists
// the refreshed token back to disk. Returns an actionable error if nothing
// is stored yet.
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
