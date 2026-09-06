// Package mcpauth implements OAuth 2.1 client support for Orchestra's MCP
// client (internal/mcp): on-disk token storage, silent refresh, a
// non-interactive auth.OAuthHandler for normal runs, and the interactive
// login flow that only `orchestra mcp login` runs. See
// docs/superpowers/specs/2026-09-06-mcp-oauth-design.md.
package mcpauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/orchestra/orchestra/patch/fsutil"
)

// ErrNoToken indicates no OAuth token is stored for a server.
var ErrNoToken = errors.New("mcpauth: no token stored for this server")

// Token is the on-disk shape of one server's OAuth grant. It carries enough
// to rebuild an oauth2.Config for a silent refresh (TokenURL, ClientID,
// ClientSecret) without re-running RFC 8414/9728 discovery on every process
// start — see TokenSourceFor in tokensource.go.
type Token struct {
	TokenURL     string    `json:"token_url"`
	ClientID     string    `json:"client_id"`
	ClientSecret string    `json:"client_secret,omitempty"`
	AccessToken  string    `json:"access_token"`
	TokenType    string    `json:"token_type,omitempty"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	Expiry       time.Time `json:"expiry"`
}

// tokenPath returns the on-disk path for a server's stored token, rejecting
// any name that isn't a single plain path component. serverName comes from
// a hand-editable .orchestra.yml, so this is defense in depth independent
// of internal/config's own validation (which only forbids ':').
func tokenPath(serverName string) (string, error) {
	if serverName == "" || serverName != filepath.Base(serverName) || serverName == "." || serverName == ".." {
		return "", fmt.Errorf("mcpauth: invalid server name %q", serverName)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("mcpauth: resolve home directory: %w", err)
	}
	return filepath.Join(home, ".orchestra", "mcp-oauth", serverName+".json"), nil
}

// SaveToken persists tok for serverName, creating ~/.orchestra/mcp-oauth/ if
// needed. The write is atomic (temp file + rename) so a crash mid-write
// never leaves a half-written token file for the next run to choke on.
func SaveToken(serverName string, tok Token) error {
	path, err := tokenPath(serverName)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(tok, "", "  ")
	if err != nil {
		return fmt.Errorf("mcpauth: encode token for %q: %w", serverName, err)
	}
	if err := fsutil.AtomicWriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("mcpauth: write token for %q: %w", serverName, err)
	}
	return nil
}

// LoadToken reads the stored token for serverName. Returns ErrNoToken
// (wrapped, checkable via errors.Is) when nothing is stored.
func LoadToken(serverName string) (Token, error) {
	path, err := tokenPath(serverName)
	if err != nil {
		return Token{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Token{}, ErrNoToken
		}
		return Token{}, fmt.Errorf("mcpauth: read token for %q: %w", serverName, err)
	}
	var tok Token
	if err := json.Unmarshal(data, &tok); err != nil {
		return Token{}, fmt.Errorf("mcpauth: decode token for %q: %w", serverName, err)
	}
	return tok, nil
}

// DeleteToken removes the stored token for serverName. Idempotent: deleting
// a server that was never logged in is not an error.
func DeleteToken(serverName string) error {
	path, err := tokenPath(serverName)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("mcpauth: delete token for %q: %w", serverName, err)
	}
	return nil
}
