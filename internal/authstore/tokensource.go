package authstore

import (
	"fmt"
	"sync"
	"time"

	"github.com/orchestra/orchestra/patch/fsutil"
	"golang.org/x/oauth2"
)

// expiryDelta is the margin oauth2 itself uses: a token this close to its
// expiry counts as expired.
const expiryDelta = 10 * time.Second

// persistingTokenSource wraps an oauth2.TokenSource, persisting a refreshed
// token back to disk immediately so a later process start reuses it instead
// of finding a stale access token and needing a fresh discovery round trip.
//
// A refresh is coordinated across processes (DATA-10): a refresh token
// rotates, so when the TUI's core and the extension's core both refreshed
// the same grant, the second one presented a spent token, got invalid_grant
// and logged the user out. Before refreshing, the source takes the token
// file's lock and adopts the token on disk when another process already
// refreshed it.
type persistingTokenSource struct {
	namespace string
	name      string
	newBase   func(Token) oauth2.TokenSource

	mu   sync.Mutex
	base oauth2.TokenSource
	last Token
}

// NewPersistingTokenSource returns a token source that writes every
// refreshed grant back to disk under (namespace, name). newBase builds the
// provider's own source from a stored token; it is called again when a
// token refreshed by another process is adopted, so the new refresh token
// is the one used from then on.
func NewPersistingTokenSource(namespace, name string, newBase func(Token) oauth2.TokenSource, last Token) oauth2.TokenSource {
	return &persistingTokenSource{namespace: namespace, name: name, newBase: newBase, base: newBase(last), last: last}
}

func (p *persistingTokenSource) Token() (*oauth2.Token, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// A token with a known expiry that has not come needs no refresh, no
	// disk and no lock.
	if !p.last.Expiry.IsZero() && time.Until(p.last.Expiry) > expiryDelta && p.last.AccessToken != "" {
		return p.last.OAuth2Token(), nil
	}

	path, err := Path(p.namespace, p.name)
	if err != nil {
		return nil, err
	}
	unlock, err := fsutil.LockFile(path + ".lock")
	if err != nil {
		return nil, fmt.Errorf("authstore: lock token for %q: %w", p.name, err)
	}
	defer unlock()

	// Another process may have refreshed while this one waited for the
	// lock, or long before: its token is the live one, and the refresh
	// token this process holds is spent.
	if disk, err := Load(p.namespace, p.name); err == nil && disk.AccessToken != "" &&
		disk.AccessToken != p.last.AccessToken && (disk.Expiry.IsZero() || time.Until(disk.Expiry) > expiryDelta) {
		p.last = disk
		p.base = p.newBase(disk)
		return disk.OAuth2Token(), nil
	}

	t, err := p.base.Token()
	if err != nil {
		return nil, err
	}
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

// OAuth2Token is the stored token in the library's form.
func (t Token) OAuth2Token() *oauth2.Token {
	return &oauth2.Token{
		AccessToken:  t.AccessToken,
		TokenType:    t.TokenType,
		RefreshToken: t.RefreshToken,
		Expiry:       t.Expiry,
	}
}
