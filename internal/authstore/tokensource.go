package authstore

import (
	"fmt"
	"sync"

	"golang.org/x/oauth2"
)

// persistingTokenSource wraps an oauth2.TokenSource, persisting a refreshed
// token back to disk immediately so a later process start reuses it instead
// of finding a stale access token and needing a fresh discovery round trip.
type persistingTokenSource struct {
	namespace string
	name      string
	base      oauth2.TokenSource

	mu   sync.Mutex
	last Token
}

// NewPersistingTokenSource returns a token source that writes every
// refreshed grant back to disk under (namespace, name).
func NewPersistingTokenSource(namespace, name string, base oauth2.TokenSource, last Token) oauth2.TokenSource {
	return &persistingTokenSource{namespace: namespace, name: name, base: base, last: last}
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
	if err := Save(p.namespace, p.name, next); err != nil {
		return nil, fmt.Errorf("authstore: persist refreshed token for %q: %w", p.name, err)
	}
	p.last = next
	return t, nil
}
