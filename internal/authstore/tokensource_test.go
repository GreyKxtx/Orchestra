package authstore

import (
	"testing"

	"golang.org/x/oauth2"
)

// stubTokenSource controls exactly what token comes back, without an HTTP
// round trip, so the refresh-token fallback can be observed directly.
type stubTokenSource struct{ tok *oauth2.Token }

func (s stubTokenSource) Token() (*oauth2.Token, error) { return s.tok, nil }

func TestPersistingTokenSource_KeepsOldRefreshTokenWhenResponseOmitsIt(t *testing.T) {
	isolateHome(t)
	last := Token{ClientID: "c", TokenURL: "https://x/token", AccessToken: "old", RefreshToken: "keep-me"}
	if err := Save("llm-oauth", "corp", last); err != nil {
		t.Fatal(err)
	}

	ts := NewPersistingTokenSource("llm-oauth", "corp", stubTokenSource{tok: &oauth2.Token{AccessToken: "new"}}, last)
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

	ts := NewPersistingTokenSource("llm-oauth", "corp", stubTokenSource{tok: &oauth2.Token{AccessToken: "same"}}, last)
	if _, err := ts.Token(); err != nil {
		t.Fatal(err)
	}
	if _, err := Load("llm-oauth", "corp"); err == nil {
		t.Fatal("file was rewritten even though the access token did not change")
	}
}
