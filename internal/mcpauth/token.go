// Package mcpauth implements OAuth 2.1 client support for Orchestra's MCP
// client (internal/mcp): the interactive login flow that only
// `orchestra mcp login` runs, a non-interactive auth.OAuthHandler for normal
// runs, and thin adapters over internal/authstore for token persistence. See
// docs/superpowers/specs/2026-09-06-mcp-oauth-design.md.
package mcpauth

import (
	"github.com/orchestra/orchestra/internal/authstore"
)

// namespace is the ~/.orchestra subdirectory holding MCP server tokens.
const namespace = "mcp-oauth"

// Token is the on-disk shape of one server's OAuth grant.
type Token = authstore.Token

// ErrNoToken indicates no OAuth token is stored for a server.
var ErrNoToken = authstore.ErrNoToken

// tokenPath returns the on-disk path for a server's stored token. The name
// guard lives in authstore: a server name comes from a hand-editable
// .orchestra.yml, so it is validated there for every caller rather than
// here for one.
func tokenPath(serverName string) (string, error) {
	return authstore.Path(namespace, serverName)
}

// SaveToken persists tok for serverName.
func SaveToken(serverName string, tok Token) error {
	return authstore.Save(namespace, serverName, tok)
}

// LoadToken reads the stored token for serverName. Returns ErrNoToken
// (wrapped, checkable via errors.Is) when nothing is stored.
func LoadToken(serverName string) (Token, error) {
	return authstore.Load(namespace, serverName)
}

// DeleteToken removes the stored token for serverName. Idempotent: deleting
// a server that was never logged in is not an error.
func DeleteToken(serverName string) error {
	return authstore.Delete(namespace, serverName)
}
