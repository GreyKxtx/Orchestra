// Package authstore persists OAuth grants under ~/.orchestra/<namespace>/,
// one JSON file per named credential, and refreshes them through a token
// source that writes the refreshed grant straight back to disk. It is shared
// by internal/mcpauth (MCP servers) and internal/llmauth (LLM providers);
// neither owns it.
package authstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/orchestra/orchestra/patch/fsutil"
)

// ErrNoToken indicates nothing is stored under this namespace and name.
var ErrNoToken = errors.New("authstore: no token stored")

// Token is the on-disk shape of one OAuth grant. It carries enough to
// rebuild an oauth2.Config for a silent refresh (TokenURL, ClientID,
// ClientSecret) without re-running discovery on every process start.
type Token struct {
	TokenURL     string    `json:"token_url"`
	ClientID     string    `json:"client_id"`
	ClientSecret string    `json:"client_secret,omitempty"`
	AccessToken  string    `json:"access_token"`
	TokenType    string    `json:"token_type,omitempty"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	Expiry       time.Time `json:"expiry"`
}

// safeComponent rejects anything that is not a single plain path component.
// Both separators are rejected explicitly, not left to filepath.Base: Base
// only knows the HOST's separator, so a name containing a backslash is one
// ordinary filename on Linux and passes there, while the same name is
// rejected on Windows. A path guard that means different things depending on
// where Orchestra runs is not much of a guard. CI is what caught this — the
// test runs on Linux, where the original check did not hold.
func safeComponent(kind, s string) error {
	if strings.ContainsAny(s, `/\`) {
		return fmt.Errorf("authstore: invalid %s %q", kind, s)
	}
	if s == "" || s != filepath.Base(s) || s == "." || s == ".." {
		return fmt.Errorf("authstore: invalid %s %q", kind, s)
	}
	return nil
}

// Path returns the on-disk path for one stored credential.
func Path(namespace, name string) (string, error) {
	if err := safeComponent("namespace", namespace); err != nil {
		return "", err
	}
	if err := safeComponent("name", name); err != nil {
		return "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("authstore: resolve home directory: %w", err)
	}
	return filepath.Join(home, ".orchestra", namespace, name+".json"), nil
}

// Save persists tok, creating ~/.orchestra/<namespace>/ if needed. The write
// is atomic (temp file + rename) so a crash mid-write never leaves a
// half-written token file for the next run to choke on.
func Save(namespace, name string, tok Token) error {
	path, err := Path(namespace, name)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(tok, "", "  ")
	if err != nil {
		return fmt.Errorf("authstore: encode token for %q: %w", name, err)
	}
	if err := fsutil.AtomicWriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("authstore: write token for %q: %w", name, err)
	}
	return nil
}

// Load reads the stored token. Returns ErrNoToken (wrapped, checkable via
// errors.Is) when nothing is stored.
func Load(namespace, name string) (Token, error) {
	path, err := Path(namespace, name)
	if err != nil {
		return Token{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Token{}, ErrNoToken
		}
		return Token{}, fmt.Errorf("authstore: read token for %q: %w", name, err)
	}
	var tok Token
	if err := json.Unmarshal(data, &tok); err != nil {
		return Token{}, fmt.Errorf("authstore: decode token for %q: %w", name, err)
	}
	return tok, nil
}

// Delete removes the stored token. Idempotent: deleting something that was
// never stored is not an error.
func Delete(namespace, name string) error {
	path, err := Path(namespace, name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("authstore: delete token for %q: %w", name, err)
	}
	return nil
}
