package authstore

import (
	"testing"
	"time"

	"github.com/orchestra/orchestra/patch/fsutil"
	"golang.org/x/oauth2"
)

// stubTokenSource controls exactly what token comes back, without an HTTP
// round trip, so the refresh-token fallback can be observed directly.
type stubTokenSource struct{ tok *oauth2.Token }

func (s stubTokenSource) Token() (*oauth2.Token, error) { return s.tok, nil }

// stubBase is a newBase that always hands back ts.
func stubBase(ts oauth2.TokenSource) func(Token) oauth2.TokenSource {
	return func(Token) oauth2.TokenSource { return ts }
}

func TestPersistingTokenSource_KeepsOldRefreshTokenWhenResponseOmitsIt(t *testing.T) {
	isolateHome(t)
	last := Token{ClientID: "c", TokenURL: "https://x/token", AccessToken: "old", RefreshToken: "keep-me"}
	if err := Save("llm-oauth", "corp", last); err != nil {
		t.Fatal(err)
	}

	ts := NewPersistingTokenSource("llm-oauth", "corp", stubBase(stubTokenSource{tok: &oauth2.Token{AccessToken: "new"}}), last)
	if _, err := ts.Token(); err != nil {
		t.Fatal(err)
	}

	got, err := Load("llm-oauth", "corp")
	if err != nil {
		t.Fatal(err)
	}
	if got.RefreshToken != "keep-me" {
		t.Fatalf("RefreshToken = %q, want preserved %q", got.RefreshToken, "keep-me")
	}
	if got.AccessToken != "new" {
		t.Fatalf("AccessToken = %q, want new", got.AccessToken)
	}
}

func TestPersistingTokenSource_DoesNotRewriteWhenNothingRefreshed(t *testing.T) {
	isolateHome(t)
	last := Token{ClientID: "c", AccessToken: "same", RefreshToken: "rt"}
	if err := Save("llm-oauth", "corp", last); err != nil {
		t.Fatal(err)
	}
	path, err := Path("llm-oauth", "corp")
	if err != nil {
		t.Fatal(err)
	}
	// Corrupt the file: if the source rewrites when nothing changed, the
	// corruption is replaced with valid JSON and the read below succeeds.
	if err := writeRaw(path, "not json"); err != nil {
		t.Fatal(err)
	}

	ts := NewPersistingTokenSource("llm-oauth", "corp", stubBase(stubTokenSource{tok: &oauth2.Token{AccessToken: "same"}}), last)
	if _, err := ts.Token(); err != nil {
		t.Fatal(err)
	}
	if _, err := Load("llm-oauth", "corp"); err == nil {
		t.Fatal("file was rewritten even though the access token did not change")
	}
}

// countingSource fails the test's expectations if the provider is asked to
// refresh when another process already did.
type countingSource struct {
	calls *int
	tok   *oauth2.Token
}

func (c countingSource) Token() (*oauth2.Token, error) {
	*c.calls++
	return c.tok, nil
}

// Two Orchestra processes share one grant. When the other one refreshed
// first, this one must use its token — refreshing again would present a
// spent refresh token and fail with invalid_grant (DATA-10).
func TestPersistingTokenSource_AdoptsATokenAnotherProcessRefreshed(t *testing.T) {
	isolateHome(t)
	expired := Token{ClientID: "c", AccessToken: "old", RefreshToken: "rt1", Expiry: time.Now().Add(-time.Hour)}
	if err := Save("llm-oauth", "corp", expired); err != nil {
		t.Fatal(err)
	}
	calls := 0
	var rebasedWith []Token
	newBase := func(tok Token) oauth2.TokenSource {
		rebasedWith = append(rebasedWith, tok)
		return countingSource{calls: &calls, tok: &oauth2.Token{AccessToken: "from-provider", RefreshToken: "rt3"}}
	}
	ts := NewPersistingTokenSource("llm-oauth", "corp", newBase, expired)

	// The other process refreshes: a live token with a rotated refresh token.
	fresh := Token{ClientID: "c", AccessToken: "fresh", RefreshToken: "rt2", Expiry: time.Now().Add(time.Hour)}
	if err := Save("llm-oauth", "corp", fresh); err != nil {
		t.Fatal(err)
	}

	got, err := ts.Token()
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "fresh" {
		t.Fatalf("AccessToken = %q, want the token the other process refreshed", got.AccessToken)
	}
	if calls != 0 {
		t.Fatalf("the provider was asked to refresh %d time(s) with a spent refresh token", calls)
	}
	if n := len(rebasedWith); n != 2 || rebasedWith[1].RefreshToken != "rt2" {
		t.Fatalf("the provider source was not rebuilt from the adopted token: %+v", rebasedWith)
	}
	// Still live: no disk, no provider.
	if got, err := ts.Token(); err != nil || got.AccessToken != "fresh" {
		t.Fatalf("second call = %v, %v", got, err)
	}
	if calls != 0 {
		t.Fatalf("a live token was refreshed")
	}
}

// A refresh waits for the token file's lock: the other process may be
// writing the token this one is about to adopt.
func TestPersistingTokenSource_RefreshesUnderTheTokenFileLock(t *testing.T) {
	isolateHome(t)
	expired := Token{ClientID: "c", AccessToken: "old", RefreshToken: "rt1", Expiry: time.Now().Add(-time.Hour)}
	if err := Save("llm-oauth", "corp", expired); err != nil {
		t.Fatal(err)
	}
	path, err := Path("llm-oauth", "corp")
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := fsutil.LockFile(path + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	ts := NewPersistingTokenSource("llm-oauth", "corp", stubBase(stubTokenSource{tok: &oauth2.Token{AccessToken: "refreshed", RefreshToken: "rt2"}}), expired)
	done := make(chan *oauth2.Token, 1)
	go func() {
		tok, _ := ts.Token()
		done <- tok
	}()
	select {
	case tok := <-done:
		t.Fatalf("refreshed to %v while another process held the token file's lock", tok)
	case <-time.After(150 * time.Millisecond):
	}
	unlock()
	select {
	case tok := <-done:
		if tok == nil || tok.AccessToken != "refreshed" {
			t.Fatalf("token after the lock was released = %v", tok)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the refresh never proceeded after the lock was released")
	}
	stored, err := Load("llm-oauth", "corp")
	if err != nil || stored.AccessToken != "refreshed" || stored.RefreshToken != "rt2" {
		t.Fatalf("stored = %+v, %v", stored, err)
	}
}
