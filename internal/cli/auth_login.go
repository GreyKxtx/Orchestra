package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/authstore"
	"github.com/orchestra/orchestra/internal/llmauth"
	"github.com/orchestra/orchestra/llm"
	"github.com/spf13/cobra"
)

var authLoginCmd = &cobra.Command{
	Use:   "login [provider]",
	Short: "Authorize a provider that uses OAuth instead of a static api_key",
	Args:  cobra.ExactArgs(1),
	RunE:  runAuthLogin,
}

var authLogoutCmd = &cobra.Command{
	Use:   "logout [provider]",
	Short: "Forget a provider's stored OAuth token",
	Args:  cobra.ExactArgs(1),
	RunE:  runAuthLogout,
}

func init() {
	authCmd.AddCommand(authLoginCmd)
	authCmd.AddCommand(authLogoutCmd)
}

// providerAuth returns the named provider's config, resolving "llm" and
// "default" to the main block the way `auth set-key` already does.
func providerAuth(name string) (llm.LLMConfig, error) {
	cfg, err := loadProjectConfig()
	if err != nil {
		return llm.LLMConfig{}, err
	}
	if name == "default" || name == "llm" {
		return cfg.LLM, nil
	}
	p, ok := cfg.Providers[name]
	if !ok {
		return llm.LLMConfig{}, fmt.Errorf("provider %q not found; add it under providers: in .orchestra.yml first", name)
	}
	return p, nil
}

func runAuthLogin(cmd *cobra.Command, args []string) error {
	name := strings.TrimSpace(args[0])
	p, err := providerAuth(name)
	if err != nil {
		return err
	}
	if p.Auth != nil && len(p.Auth.TokenCommand) > 0 {
		return fmt.Errorf("provider %q authenticates with token_command; there is nothing to log into", name)
	}
	if p.Auth == nil || p.Auth.OAuth == nil {
		return fmt.Errorf("provider %q has no oauth block; add auth: with oauth: (auth_url, token_url, client_id) under it in .orchestra.yml", name)
	}

	// Orchestra ships no provider presets: these endpoints and this client id
	// are the user's own. Whether using them is permitted is between the user
	// and their provider, and saying so once is proportionate.
	fmt.Fprintf(os.Stderr, "Authorizing %q against %s.\n", name, p.Auth.OAuth.AuthURL)
	fmt.Fprintln(os.Stderr, "Complying with the provider's terms of service is your responsibility.")

	if err := llmauth.Login(context.Background(), llmauth.LoginConfig{
		Name:  name,
		OAuth: *p.Auth.OAuth,
		Out:   os.Stderr,
	}); err != nil {
		return err
	}
	fmt.Printf("Authorized %q.\n", name)
	return nil
}

func runAuthLogout(cmd *cobra.Command, args []string) error {
	name := strings.TrimSpace(args[0])
	if err := llmauth.Logout(name); err != nil {
		return err
	}
	fmt.Printf("Forgot the stored token for %q.\n", name)
	return nil
}

// authStatusLine describes how one provider authenticates, for `auth list`.
// Before this, list showed only a redacted api_key, which reads as "no
// credential" for a provider that authenticates by OAuth.
func authStatusLine(name string, cfg llm.LLMConfig) string {
	if cfg.Auth != nil && len(cfg.Auth.TokenCommand) > 0 {
		return fmt.Sprintf("token_command %v (cached %s)", cfg.Auth.TokenCommand, cfg.Auth.EffectiveTokenCommandTTL())
	}
	if cfg.Auth != nil && cfg.Auth.OAuth != nil {
		return oauthStatus(name)
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return "no credential configured"
	}
	return "key=" + redactKey(cfg.APIKey)
}

func oauthStatus(name string) string {
	tok, err := authstore.Load(llmauth.Namespace, name)
	if errors.Is(err, authstore.ErrNoToken) {
		return fmt.Sprintf("oauth (not authorized, run: orchestra auth login %s)", name)
	}
	if err != nil {
		return fmt.Sprintf("oauth (token unreadable: %v)", err)
	}
	if tok.Expiry.IsZero() {
		return "oauth (no expiry recorded)"
	}
	if time.Now().After(tok.Expiry) {
		// An expired access token is only the user's problem when there is no
		// refresh token to trade in; otherwise the next request fixes it.
		if tok.RefreshToken != "" {
			return "oauth (access token expired, will refresh on next use)"
		}
		return fmt.Sprintf("oauth (expired, run: orchestra auth login %s)", name)
	}
	return fmt.Sprintf("oauth (expires in %s)", time.Until(tok.Expiry).Round(time.Minute))
}
